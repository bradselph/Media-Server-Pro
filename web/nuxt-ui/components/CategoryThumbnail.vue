<script setup lang="ts">
/**
 * CategoryThumbnail — adaptive mosaic cover for a curated category.
 *
 * Fills its parent's 16:9 shell (the caller supplies
 * `aspect-video relative rounded-lg overflow-hidden bg-muted`); this component
 * paints into it via `absolute inset-0`.
 *
 * Priority: an admin-pinned `coverMediaId` wins; otherwise up to four
 * `previewMediaIds` (the category's first members, in display order) are laid
 * out as a 1/2/3/4-tile mosaic. Tiles whose thumbnail fails to load (e.g. audio
 * items with no real thumbnail, or thumbnails still being generated) are dropped
 * and the mosaic re-balances to the working ones; when nothing is left a library
 * icon placeholder is shown — matching the previous single-cover fallback.
 */
const props = defineProps<{
  /** Admin-pinned single cover; takes priority over previewMediaIds. */
  coverMediaId?: string
  /** Up to 4 preview member IDs in display order. */
  previewMediaIds?: string[]
  /** Alt text forwarded to each tile (use the category name). */
  alt?: string
}>()

const mediaApi = useMediaApi()

// Per-tile error tracking. Keyed by media ID, plus the literal 'cover' for the
// admin override. Replaced (not mutated) so the computeds below re-evaluate.
const failedIds = ref<Set<string>>(new Set())

function markFailed(id: string) {
  if (failedIds.value.has(id)) return
  const next = new Set(failedIds.value)
  next.add(id)
  failedIds.value = next
}

// Reset error state if the category this instance renders changes (defensive:
// instances are normally keyed per category, but previews can be reordered).
watch(
  () => [props.coverMediaId, ...(props.previewMediaIds ?? [])].join('|'),
  () => {
    failedIds.value = new Set()
  },
)

// Admin cover URL; null when unset or after it errored.
const coverSrc = computed(() =>
  props.coverMediaId && !failedIds.value.has('cover')
    ? mediaApi.getThumbnailUrl(props.coverMediaId)
    : null,
)

// Preview IDs minus failed ones, capped at 4.
const visibleIds = computed(() =>
  (props.previewMediaIds ?? []).filter(id => !failedIds.value.has(id)).slice(0, 4),
)

type Layout = 'cover' | 'one' | 'two' | 'three' | 'four' | 'empty'
const layout = computed<Layout>(() => {
  if (coverSrc.value) return 'cover'
  const n = visibleIds.value.length
  if (n === 0) return 'empty'
  if (n === 1) return 'one'
  if (n === 2) return 'two'
  if (n === 3) return 'three'
  return 'four'
})

// Shared tile <img> class — mirrors the existing media-card thumbnail treatment.
const tileImg = 'w-full h-full object-cover group-hover:scale-105 transition-transform duration-200'
</script>

<template>
  <div class="absolute inset-0">
    <!-- Admin single-cover override -->
    <img
        v-if="layout === 'cover'"
        :src="coverSrc!"
        :alt="alt"
        :class="tileImg"
        loading="lazy"
        @error="markFailed('cover')"
    />

    <!-- No thumbnails (none provided, or every tile errored) -->
    <div
        v-else-if="layout === 'empty'"
        class="w-full h-full flex items-center justify-center"
    >
      <UIcon name="i-lucide-library" class="size-8 text-muted"/>
    </div>

    <!-- 1 or 2 tiles: single column / two columns -->
    <div
        v-else-if="layout === 'one' || layout === 'two'"
        class="grid h-full gap-px bg-[var(--hairline)]"
        :class="layout === 'two' ? 'grid-cols-2' : 'grid-cols-1'"
    >
      <div
          v-for="id in visibleIds"
          :key="id"
          class="relative overflow-hidden bg-[var(--surface-card)]"
      >
        <img :src="mediaApi.getThumbnailUrl(id)" :alt="alt" :class="tileImg" loading="lazy" @error="markFailed(id)"/>
      </div>
    </div>

    <!-- 3 tiles: hero-left (full height) + two stacked right. Every tile (hero
         included) is rendered through a v-for so its id is closure-captured and
         keyed — a stale @error from a since-replaced image then targets its own
         id, never whichever id currently sits at visibleIds[0]. -->
    <div
        v-else-if="layout === 'three'"
        class="grid grid-cols-2 grid-rows-2 h-full gap-px bg-[var(--hairline)]"
    >
      <div
          v-for="id in visibleIds.slice(0, 1)"
          :key="id"
          class="relative row-span-2 overflow-hidden bg-[var(--surface-card)]"
      >
        <img :src="mediaApi.getThumbnailUrl(id)" :alt="alt" :class="tileImg" loading="lazy" @error="markFailed(id)"/>
      </div>
      <div
          v-for="id in visibleIds.slice(1)"
          :key="id"
          class="relative overflow-hidden bg-[var(--surface-card)]"
      >
        <img :src="mediaApi.getThumbnailUrl(id)" :alt="alt" :class="tileImg" loading="lazy" @error="markFailed(id)"/>
      </div>
    </div>

    <!-- 4 tiles: 2×2 grid -->
    <div
        v-else
        class="grid grid-cols-2 grid-rows-2 h-full gap-px bg-[var(--hairline)]"
    >
      <div
          v-for="id in visibleIds"
          :key="id"
          class="relative overflow-hidden bg-[var(--surface-card)]"
      >
        <img :src="mediaApi.getThumbnailUrl(id)" :alt="alt" :class="tileImg" loading="lazy" @error="markFailed(id)"/>
      </div>
    </div>
  </div>
</template>
