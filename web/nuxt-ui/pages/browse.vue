<script setup lang="ts">
/**
 * /browse — Tag cloud discovery page (retention plan B.1).
 *
 * Renders every tag in the library as a chip sized proportional to the log
 * of its usage frequency. Users can multi-select tags with an AND/OR mode
 * toggle; a debounced count query keeps a live result count. Apply navigates
 * to /?tags=a,b,c&tag_mode=and so the home grid renders the filtered list.
 *
 * The backend `/api/tags` endpoint already strips mature tags for callers
 * without the mature-view permission, so this page renders the response
 * as-is without any client-side gating.
 */
import { useMediaApi } from '~/composables/useApiEndpoints'
import type { TagCount } from '~/composables/useApiEndpoints'
import { useTagsApi } from '~/composables/useApiEndpoints'

definePageMeta({ title: 'Browse tags' })

const router = useRouter()
const route = useRoute()
const tagsApi = useTagsApi()
const mediaApi = useMediaApi()
const toast = useToast()

const tags = ref<TagCount[]>([])
const loading = ref(true)
const loadError = ref('')

// Selection state — initialised from any tags=... query string so the page
// is deep-linkable from a shared URL.
const initialTags = ((route.query.tags as string | undefined) ?? '')
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
const selected = ref<Set<string>>(new Set(initialTags))
const mode = ref<'and' | 'or'>(
    route.query.tag_mode === 'or' ? 'or' : 'and',
)

const searchQuery = ref('')

async function load() {
    loading.value = true
    loadError.value = ''
    try {
        tags.value = (await tagsApi.list()) ?? []
    } catch (e: unknown) {
        loadError.value = e instanceof Error ? e.message : 'Failed to load tags'
    } finally {
        loading.value = false
    }
}

onMounted(load)

onBeforeUnmount(() => {
    if (previewTimer) {
        clearTimeout(previewTimer)
        previewTimer = null
    }
})

const filteredTags = computed(() => {
    const q = searchQuery.value.trim().toLowerCase()
    if (!q) return tags.value
    return tags.value.filter(t => t.tag.toLowerCase().includes(q))
})

// Font-size scaling. Log-scaled so a tag with 1000 items doesn't visually
// dwarf one with 5. min/max font sizes are tuned for legibility at the
// 12–28px range called out in the prototype.
const sizeBounds = computed(() => {
    const counts = tags.value.map(t => t.count)
    if (counts.length === 0) return { min: 1, max: 1 }
    return { min: Math.min(...counts), max: Math.max(...counts) }
})

function sizeFor(count: number): number {
    const { min, max } = sizeBounds.value
    if (max <= min) return 16
    const minPx = 12
    const maxPx = 28
    const t = Math.log(count - min + 1) / Math.log(max - min + 1)
    return Math.round(minPx + (maxPx - minPx) * t)
}

function toggleTag(tag: string) {
    const next = new Set(selected.value)
    if (next.has(tag)) next.delete(tag)
    else next.add(tag)
    selected.value = next
}

function clearSelection() { selected.value = new Set() }

// Live preview count — debounced 200ms per plan. Sends a `count_only=1`
// hint via limit=1 then reads total_items from the standard list response.
const previewCount = ref<number | null>(null)
const previewLoading = ref(false)
let previewTimer: ReturnType<typeof setTimeout> | null = null

watch([selected, mode], () => {
    if (previewTimer) clearTimeout(previewTimer)
    if (selected.value.size === 0) {
        previewCount.value = null
        return
    }
    previewTimer = setTimeout(() => { void refreshPreview() }, 200)
}, { deep: true })

async function refreshPreview() {
    if (selected.value.size === 0) {
        previewCount.value = null
        return
    }
    previewLoading.value = true
    try {
        const res = await mediaApi.list({
            tags: [...selected.value],
            tag_mode: mode.value,
            limit: 1,
            page: 1,
        })
        previewCount.value = res.total_items ?? 0
    } catch {
        previewCount.value = null
    } finally {
        previewLoading.value = false
    }
}

function apply() {
    if (selected.value.size === 0) {
        toast.add({ title: 'Select at least one tag first', color: 'warning', icon: 'i-lucide-info' })
        return
    }
    router.push({
        path: '/',
        query: {
            tags: [...selected.value].join(','),
            tag_mode: mode.value,
            view: 'browse',
        },
    })
}
</script>

<template>
  <UContainer class="py-6 max-w-5xl">
    <header class="space-y-2 mb-5">
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-tags" class="size-5 text-primary" />
        <h1 class="text-xl font-semibold">Browse by tag</h1>
      </div>
      <p class="text-sm text-muted">Pick a few tags to narrow the library — pair the AND mode to find items that match every tag, or OR mode for anything matching one.</p>
    </header>

    <!-- Controls row -->
    <div class="flex flex-wrap items-center gap-3 mb-4">
      <UInput
        v-model="searchQuery"
        icon="i-lucide-search"
        placeholder="Filter tags…"
        size="sm"
        type="search"
        class="min-w-[200px]"
      />
      <div class="inline-flex rounded-md border border-default overflow-hidden text-xs font-semibold">
        <button
          type="button"
          class="px-3 py-1.5 transition-colors"
          :class="mode === 'and' ? 'bg-[var(--accent)] text-white' : 'text-muted hover:text-default'"
          :aria-pressed="mode === 'and'"
          @click="mode = 'and'"
        >AND (all)</button>
        <button
          type="button"
          class="px-3 py-1.5 transition-colors border-l border-default"
          :class="mode === 'or' ? 'bg-[var(--accent)] text-white' : 'text-muted hover:text-default'"
          :aria-pressed="mode === 'or'"
          @click="mode = 'or'"
        >OR (any)</button>
      </div>
      <UButton
        v-if="selected.size > 0"
        icon="i-lucide-x"
        label="Clear"
        size="sm"
        color="neutral"
        variant="ghost"
        @click="clearSelection"
      />
    </div>

    <!-- Tag cloud -->
    <div v-if="loading" class="py-16 text-center text-muted">
      <UIcon name="i-lucide-loader-2" class="size-6 animate-spin" />
      <p class="mt-2 text-sm">Loading tags…</p>
    </div>
    <UAlert v-else-if="loadError" :title="loadError" color="error" icon="i-lucide-alert-circle" />
    <div v-else-if="tags.length === 0" class="py-16 text-center text-muted">
      <UIcon name="i-lucide-tags" class="size-10 mb-3 mx-auto opacity-40" />
      <p>No tags yet.</p>
    </div>
    <div v-else>
      <div class="flex flex-wrap gap-2.5 leading-none">
        <button
          v-for="t in filteredTags"
          :key="t.tag"
          type="button"
          class="inline-flex items-baseline gap-1 px-3 py-1.5 rounded-full border transition-colors"
          :class="selected.has(t.tag)
            ? 'bg-[var(--accent)] border-[var(--accent)] text-white'
            : 'bg-[var(--surface-card)] border-[var(--hairline)] text-[var(--text-med)] hover:text-[var(--text-strong)] hover:border-[var(--hairline-strong)]'"
          :style="{ fontSize: `${sizeFor(t.count)}px` }"
          :aria-pressed="selected.has(t.tag)"
          @click="toggleTag(t.tag)"
        >
          <span class="font-semibold">{{ t.tag }}</span>
          <span class="font-mono text-[10px] opacity-70">{{ t.count }}</span>
        </button>
      </div>
      <p v-if="filteredTags.length === 0" class="mt-4 text-sm text-muted">No tags match "{{ searchQuery }}".</p>
    </div>

    <!-- Apply bar -->
    <div
      v-if="selected.size > 0"
      class="sticky bottom-4 mt-8 mx-auto max-w-3xl bg-[var(--surface-card)] border border-[var(--hairline-strong)] rounded-xl px-4 py-3 shadow-2xl flex items-center gap-3 flex-wrap"
    >
      <div class="flex-1 min-w-[200px]">
        <p class="text-sm font-semibold">{{ selected.size }} tag<span v-if="selected.size !== 1">s</span> selected · {{ mode === 'and' ? 'AND' : 'OR' }} mode</p>
        <p class="text-xs text-muted">
          <UIcon v-if="previewLoading" name="i-lucide-loader-2" class="size-3 animate-spin inline" />
          <span v-else-if="previewCount !== null">{{ previewCount.toLocaleString() }} item<span v-if="previewCount !== 1">s</span> match</span>
          <span v-else>Updating count…</span>
        </p>
      </div>
      <UButton
        icon="i-lucide-arrow-right"
        label="Apply"
        size="md"
        color="primary"
        @click="apply"
      />
    </div>
  </UContainer>
</template>
