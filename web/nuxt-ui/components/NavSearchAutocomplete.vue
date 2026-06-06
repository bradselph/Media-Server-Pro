<script setup lang="ts">
import {useMediaApi} from '~/composables/useApiEndpoints'
import {useRecentSearches} from '~/composables/useRecentSearches'
import {getDisplayTitle} from '~/utils/mediaTitle'
import type {MediaItem} from '~/types/api'

// Lightweight autocomplete dropped under the nav search input. Two sources:
//  - Recent searches (localStorage) — shown when the input is empty/short.
//  - Top media titles matching the prefix — fetched debounced (180ms) via
//    /api/media?search=…&limit=5 once the user has typed ≥ 2 chars.
//
// Selecting a recent query reruns the search; selecting a media item jumps
// straight to the player. Submitting (Enter) always falls back to /search?q=
// so the parent form behavior stays intact.

const props = defineProps<{
  query: string
  open: boolean
}>()

const emit = defineEmits<{
  select: [string]              // user picked a recent search → set input + navigate
  navigate: [string]            // user picked a media item → jump to /player?id=
  close: []                      // request the parent to hide the dropdown
}>()

const mediaApi = useMediaApi()
const {recent, remove: removeRecent} = useRecentSearches()

const suggestions = ref<MediaItem[]>([])
const loading = ref(false)
let token = 0
let timer: ReturnType<typeof setTimeout> | null = null

const trimmed = computed(() => props.query.trim())

const matchingRecent = computed(() => {
  const q = trimmed.value.toLowerCase()
  if (!q) return recent.value
  return recent.value.filter(r => r.toLowerCase().includes(q) && r.toLowerCase() !== q).slice(0, 5)
})

watch(() => props.query, (q) => {
  const t = q.trim()
  if (timer) {
    clearTimeout(timer);
    timer = null
  }
  if (t.length < 2) {
    suggestions.value = []
    loading.value = false
    return
  }
  timer = setTimeout(async () => {
    timer = null
    const tk = ++token
    loading.value = true
    try {
      const res = await mediaApi.list({search: t, limit: 5})
      if (tk !== token) return
      suggestions.value = res?.items ?? []
    } catch { /* network blip — drop silently */
    } finally {
      if (tk === token) loading.value = false
    }
  }, 180)
})

onBeforeUnmount(() => {
  if (timer) clearTimeout(timer)
})

const hasContent = computed(() =>
    loading.value
    || suggestions.value.length > 0
    || matchingRecent.value.length > 0,
)

function pickMedia(item: MediaItem) {
  emit('navigate', `/player?id=${encodeURIComponent(item.id)}`)
  emit('close')
}

function pickRecent(q: string) {
  emit('select', q)
  emit('close')
}

function dropRecent(e: Event, q: string) {
  e.stopPropagation()
  e.preventDefault()
  removeRecent(q)
}
</script>

<template>
  <div
      v-if="open && hasContent"
      class="absolute left-0 right-0 top-full mt-1 bg-elevated border border-default rounded-md shadow-lg z-40 max-h-[60vh] overflow-y-auto"
      role="listbox"
  >
    <!-- Recent searches -->
    <div v-if="matchingRecent.length > 0" class="py-1">
      <div class="px-3 py-1 text-[11px] font-semibold uppercase tracking-wider text-muted">Recent</div>
      <button
          v-for="r in matchingRecent"
          :key="`r:${r}`"
          type="button"
          class="group w-full flex items-center justify-between px-3 py-1.5 text-sm hover:bg-muted/40 text-left"
          role="option"
          @click.prevent="pickRecent(r)"
      >
        <span class="flex items-center gap-2 min-w-0">
          <UIcon name="i-lucide-clock" class="size-3.5 text-muted shrink-0"/>
          <span class="truncate">{{ r }}</span>
        </span>
        <span
            class="opacity-0 group-hover:opacity-100 text-muted hover:text-error transition-opacity"
            :aria-label="`Remove ${r} from recent searches`"
            role="button"
            tabindex="0"
            @click="dropRecent($event, r)"
            @keydown.enter="dropRecent($event, r)"
        >
          <UIcon name="i-lucide-x" class="size-3.5"/>
        </span>
      </button>
    </div>

    <!-- Loading hint -->
    <div v-if="loading && suggestions.length === 0" class="px-3 py-2 text-xs text-muted flex items-center gap-2">
      <UIcon name="i-lucide-loader-2" class="size-3.5 animate-spin"/>
      Searching…
    </div>

    <!-- Media suggestions -->
    <div v-if="suggestions.length > 0" class="py-1 border-t border-default">
      <div class="px-3 py-1 text-[11px] font-semibold uppercase tracking-wider text-muted">Top matches</div>
      <button
          v-for="item in suggestions"
          :key="`m:${item.id}`"
          type="button"
          class="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted/40 text-left"
          role="option"
          @click.prevent="pickMedia(item)"
      >
        <UIcon
            :name="item.type === 'audio' ? 'i-lucide-music' : 'i-lucide-play'"
            class="size-3.5 text-muted shrink-0"
        />
        <span class="truncate">{{ getDisplayTitle(item) }}</span>
        <span v-if="item.category" class="ml-auto text-[10px] text-muted shrink-0">{{ item.category }}</span>
      </button>
    </div>
  </div>
</template>
