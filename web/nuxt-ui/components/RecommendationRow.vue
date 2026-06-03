<script setup lang="ts">
import type { Suggestion } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration } from '~/utils/format'

const props = defineProps<{
  title: string
  icon: string
  items: Suggestion[]
  failedIds?: Set<string>
  to?: string
  // When true (and items is empty), the row renders skeleton placeholder
  // cards instead of being hidden — gives the home page a stable layout
  // during the initial /api/suggestions/* fan-out.
  loading?: boolean
  // Optional map of mediaId → progress in [0..1]. When provided, cards
  // surface the same progress bar / Watched pill pair as the main grid.
  // Passing a partial map is fine; missing IDs just don't render the
  // indicator.
  progress?: Record<string, number>
  // Favorited media IDs (parent-owned) so the heart renders its filled state.
  favoriteIds?: Set<string>
  // Parent-provided builder for the "add to playlist" dropdown menu items.
  // When omitted, the playlist action is not shown.
  playlistMenuItems?: (mediaId: string) => { label: string; icon?: string; click?: () => void; to?: string }[][]
}>()

const emit = defineEmits<{
  'thumbnail-error': [id: string]
  'toggle-favorite': [mediaId: string]
}>()

const mediaApi = useMediaApi()
const authStore = useAuthStore()
const scrollContainer = ref<HTMLElement | null>(null)

function scrollBy(delta: number) {
  scrollContainer.value?.scrollBy({ left: delta, behavior: 'smooth' })
}

const PALETTES: [string, string][] = [
  ['#1a0835','#9333ea'],['#081530','#2563eb'],['#1a0808','#dc2626'],
  ['#081508','#16a34a'],['#1a1208','#d97706'],['#081515','#0891b2'],
  ['#150815','#db2777'],['#0a0815','#6366f1'],['#150a0a','#ea580c'],
  ['#0a1515','#059669'],['#0f0a20','#a855f7'],['#1a1000','#ca8a04'],
]

function getGradientStyle(id: string): string {
  let hash = 0
  for (let i = 0; i < id.length; i++) hash = (hash * 31 + id.charCodeAt(i)) & 0xffff
  const [c1, c2] = PALETTES[hash % PALETTES.length]
  return `linear-gradient(135deg, ${c1}, ${c2})`
}
</script>

<template>
  <div v-if="loading && items.length === 0" class="space-y-3" aria-busy="true">
    <div class="flex items-center justify-between">
      <h2 class="text-lg font-bold text-[var(--text-strong)] flex items-center gap-2">
        <UIcon :name="icon" class="size-4 text-[var(--accent)]" />
        {{ title }}
      </h2>
    </div>
    <div class="flex gap-3 overflow-hidden pb-3" aria-hidden="true">
      <div v-for="n in 6" :key="n" class="shrink-0 w-52 space-y-2">
        <div class="aspect-video rounded-lg bg-elevated/60 animate-pulse" />
        <div class="h-3 w-3/4 rounded bg-elevated/60 animate-pulse" />
        <div class="h-2.5 w-1/3 rounded bg-elevated/40 animate-pulse" />
      </div>
    </div>
  </div>
  <div v-else-if="items.length > 0" class="space-y-3">
    <div class="flex items-center justify-between">
      <h2 class="text-lg font-bold text-[var(--text-strong)] flex items-center gap-2">
        <UIcon :name="icon" class="size-4 text-[var(--accent)]" />
        {{ title }}
      </h2>
      <div class="flex items-center gap-2">
        <NuxtLink v-if="to" :to="to" class="text-xs font-medium text-[var(--accent-soft)] hover:underline flex items-center gap-1">See all <UIcon name="i-lucide-arrow-right" class="size-3" /></NuxtLink>
        <div class="flex gap-1">
          <UButton icon="i-lucide-chevron-left" size="xs" variant="ghost" color="neutral" aria-label="Scroll left" @click="scrollBy(-320)" />
          <UButton icon="i-lucide-chevron-right" size="xs" variant="ghost" color="neutral" aria-label="Scroll right" @click="scrollBy(320)" />
        </div>
      </div>
    </div>
    <div ref="scrollContainer" class="flex gap-3 overflow-x-auto pb-3 scroll-smooth scrollbar-hide">
      <NuxtLink
        v-for="s in items"
        :key="s.media_id"
        :to="`/player?id=${encodeURIComponent(s.media_id)}`"
        class="group shrink-0 w-52"
      >
        <div class="relative aspect-video rounded-lg overflow-hidden mb-2 media-card-lift scanline-thumb">
          <!-- Audio items: gradient bg + animated bars -->
          <div
            v-if="s.media_type === 'audio'"
            class="w-full h-full flex flex-col items-center justify-center gap-2"
            :style="{ background: getGradientStyle(s.media_id) }"
          >
            <AudioBars size="sm" :bars="5" class="opacity-70 group-hover:opacity-100 transition-opacity" />
            <span class="text-[9px] font-medium text-white/60 uppercase tracking-wider">Audio</span>
          </div>
          <!-- Video: thumbnail with gradient fallback -->
          <template v-else>
            <div
              class="absolute inset-0"
              :style="{ background: getGradientStyle(s.media_id) }"
            />
            <HoverPreviewImg
              v-if="!failedIds?.has(s.media_id)"
              :media-id="s.media_id"
              :src="mediaApi.getThumbnailUrl(s.media_id)"
              :alt="getDisplayTitle(s)"
              :width="320"
              :height="180"
              img-class="absolute inset-0 w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
              @error="emit('thumbnail-error', s.media_id)"
            />
          </template>
          <!-- Duration badge -->
          <div v-if="s.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1.5 py-0.5 rounded">
            {{ formatDuration(s.duration) }}
          </div>
          <!-- Progress / Watched indicator (parity with home grid) -->
          <div
            v-if="progress?.[s.media_id] && (progress?.[s.media_id] ?? 0) < 0.9"
            class="absolute bottom-0 left-0 right-0 h-1 bg-white/20"
          >
            <div
              class="h-full bg-primary"
              :style="{ width: `${Math.min(100, Math.round((progress?.[s.media_id] ?? 0) * 100))}%` }"
            />
          </div>
          <div
            v-if="(progress?.[s.media_id] ?? 0) >= 0.9"
            class="absolute bottom-1 left-1 flex items-center gap-1 bg-emerald-600/85 text-white text-[10px] font-semibold px-1.5 py-0.5 rounded shadow-sm"
            :title="`Watched (${Math.round((progress?.[s.media_id] ?? 0) * 100)}%)`"
          >
            <UIcon name="i-lucide-check" class="size-3" />
            <span>Watched</span>
          </div>
          <!-- Hover quick actions (logged-in only) — favorite + add to playlist,
               matching the home grid's card overlay. -->
          <template v-if="authStore.isLoggedIn">
            <button
              class="absolute bottom-6 right-1 p-0.5 rounded-full bg-black/50 opacity-0 group-hover:opacity-100 transition-opacity"
              :aria-label="favoriteIds?.has(s.media_id) ? 'Remove from favorites' : 'Add to favorites'"
              @click.prevent.stop="emit('toggle-favorite', s.media_id)"
            >
              <UIcon
                name="i-lucide-heart"
                :class="favoriteIds?.has(s.media_id) ? 'size-4 text-red-400 [&>svg]:fill-current' : 'size-4 text-white'"
              />
            </button>
            <div
              v-if="playlistMenuItems"
              class="absolute bottom-6 right-8 opacity-0 group-hover:opacity-100 transition-opacity"
              @click.prevent.stop
            >
              <UDropdownMenu :items="playlistMenuItems(s.media_id)">
                <button class="p-0.5 rounded-full bg-black/50" aria-label="Add to playlist">
                  <UIcon name="i-lucide-list-plus" class="size-4 text-white" />
                </button>
              </UDropdownMenu>
            </div>
          </template>
        </div>
        <p class="text-xs font-semibold truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(s)">{{ getDisplayTitle(s) }}</p>
        <p v-if="s.category" class="text-[10px] text-muted truncate mt-0.5">{{ s.category }}</p>
      </NuxtLink>
    </div>
  </div>
</template>
