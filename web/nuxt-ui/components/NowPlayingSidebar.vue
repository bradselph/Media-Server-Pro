<script setup lang="ts">
/**
 * NowPlayingSidebar.vue
 * ---------------------------------------------------------------
 * Right-docked Now Playing + Queue + Playlist sidebar. Replaces the
 * previous bottom-overlay MiniPlayer.
 *
 * Mounts in layouts/default.vue. Hidden on /player and auth routes.
 * Mobile (<md) collapses to a bottom dock that expands to a full sheet.
 *
 * Wires to:
 *   usePlaybackStore() — current media, position, duration, isPlaying
 *   useQueueStore()    — Up Next list
 *   usePlaylistApi()   — pinned playlist (auto-set when player starts
 *                        playback FROM a playlist via the
 *                        'msp:playlist-context' window CustomEvent)
 *
 * Keyboard shortcuts (Q / [ / ] / N) are handled here, with the same
 * input-tag guard the rest of the app uses.
 */
import type {Playlist} from '~/types/api'
import {formatDuration} from '~/utils/format'
import {getMediaGradient} from '~/utils/gradient'
import {getDisplayTitle} from '~/utils/mediaTitle'
import {useQueueStore} from '~/stores/queue'
import {useSidebarState} from '~/composables/useSidebarState'

const route = useRoute()
const router = useRouter()
const playback = usePlaybackStore()
const queue = useQueueStore()
const playlistApi = usePlaylistApi()
const authStore = useAuthStore()
const toast = useToast()

const sb = useSidebarState()
const {open, tab, pinnedPlaylistId} = sb
const pinnedPlaylist = ref<Playlist | null>(null)

const mobileSheetOpen = ref(false)

async function loadPinnedPlaylist() {
  if (!pinnedPlaylistId.value) {
    pinnedPlaylist.value = null
    return
  }
  try {
    pinnedPlaylist.value = await playlistApi.get(pinnedPlaylistId.value)
  } catch {
    pinnedPlaylist.value = null
  }
}

onMounted(() => {
  if (pinnedPlaylistId.value) loadPinnedPlaylist()
})

watch(pinnedPlaylistId, () => {
  loadPinnedPlaylist()
})

// ── Visibility ──────────────────────────────────────────────────
// Hidden on /player (full controls available there), all auth pages
// (sidebar references playback state that's irrelevant there), and the
// account/admin login pages (no chrome until signed in).
const HIDDEN_ROUTES = new Set(['/player', '/login', '/signup', '/register', '/admin-login'])
const visible = computed(() => !HIDDEN_ROUTES.has(route.path))

// ── Keyboard shortcuts ──────────────────────────────────────────
function shouldIgnoreShortcut(e: KeyboardEvent): boolean {
  if (e.ctrlKey || e.metaKey || e.altKey) return true
  const tgt = e.target as HTMLElement | null
  const tagName = tgt?.tagName?.toLowerCase()
  if (tagName === 'input' || tagName === 'textarea' || tagName === 'select') return true
  if (tgt?.isContentEditable) return true
  return false
}

function onKeydown(e: KeyboardEvent) {
  if (!visible.value) return
  if (shouldIgnoreShortcut(e)) return
  const k = e.key
  if (k === 'q' || k === 'Q') {
    e.preventDefault();
    sb.toggle()
  } else if (k === ']') {
    e.preventDefault();
    sb.collapse()
  } else if (k === '[') {
    e.preventDefault();
    sb.expand()
  } else if (k === 'n' || k === 'N') {
    e.preventDefault();
    playNext()
  } else if (k === 'Escape' && mobileSheetOpen.value) {
    e.preventDefault()
    mobileSheetOpen.value = false
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))

// ── Derived ─────────────────────────────────────────────────────
const progressPct = computed(() => {
  const dur = playback.mediaInfo?.duration ?? playback.duration
  const pos = playback.position
  if (!dur || !Number.isFinite(dur) || !Number.isFinite(pos)) return 0
  return Math.min(100, Math.max(0, (pos / dur) * 100))
})

const queueTotalDuration = computed(() =>
    queue.items.reduce((sum, it) => sum + (it.duration || 0), 0),
)

const resumeUrl = computed(() => {
  const id = playback.currentMediaId
  if (!id) return '/'
  const t = Math.floor(playback.position)
  return t > 5
      ? `/player?id=${encodeURIComponent(id)}&t=${t}`
      : `/player?id=${encodeURIComponent(id)}`
})

// ── Actions ─────────────────────────────────────────────────────
function openPlayer() {
  const id = playback.currentMediaId
  if (!id) return
  router.push(resumeUrl.value)
}

const advancing = ref(false)

function playNext() {
  // Guard against a rapid double-click dequeuing two items but only navigating
  // to the second — which silently drops the first from the queue.
  if (advancing.value) return
  const next = queue.shift()
  if (!next) {
    toast.add({title: 'Queue is empty', color: 'neutral', icon: 'i-lucide-list'})
    return
  }
  advancing.value = true
  void router.push(`/player?id=${encodeURIComponent(next.id)}`).finally(() => {
    advancing.value = false
  })
}

function playFromQueue(id: string) {
  queue.remove(id)
  router.push(`/player?id=${encodeURIComponent(id)}`)
}

function removeFromQueue(id: string) {
  queue.remove(id)
}

function clearQueue() {
  queue.clear()
  toast.add({title: 'Queue cleared', icon: 'i-lucide-trash'})
}

function moveUp(id: string) {
  queue.moveUp(id)
}

function moveDown(id: string) {
  queue.moveDown(id)
}

function unpinPlaylist() {
  sb.pinPlaylist(null)
  pinnedPlaylist.value = null
  toast.add({title: 'Playlist unpinned', icon: 'i-lucide-pin-off'})
}

// Autoplay-similar mirror (B.3). The sidebar empty state exposes the
// preference toggle so users can flip it without leaving home. Local
// ref is hydrated from authStore and persisted through updatePreferences.
const autoplaySimilar = ref(authStore.user?.preferences?.autoplay_similar ?? true)
watch(
    () => authStore.user?.preferences?.autoplay_similar,
    (v) => {
      if (typeof v === 'boolean') autoplaySimilar.value = v
    },
)

function toggleAutoplaySimilar() {
  autoplaySimilar.value = !autoplaySimilar.value
  if (!authStore.isLoggedIn) return
  const {updatePreferences} = useApiEndpoints()
  updatePreferences({autoplay_similar: autoplaySimilar.value}).catch(() => {
  })
  if (authStore.user) {
    authStore.user.preferences = {
      ...authStore.user.preferences,
      autoplay_similar: autoplaySimilar.value,
    }
  }
}

// ── Auto-pin the last playlist the user played from ────────────
// pages/playlists.vue and pages/player.vue dispatch this when a user
// starts playback from a playlist context.
function onPlaylistContext(e: Event) {
  const id = (e as CustomEvent<{ id: string }>).detail?.id
  if (id && id !== pinnedPlaylistId.value) {
    sb.pinPlaylist(id)
  }
}

onMounted(() => window.addEventListener('msp:playlist-context', onPlaylistContext))
onUnmounted(() => window.removeEventListener('msp:playlist-context', onPlaylistContext))

// ── Mobile body-scroll lock + focus trap when sheet is open ─────
const sheetRef = ref<HTMLElement | null>(null)
let lastFocusedBeforeSheet: HTMLElement | null = null

watch(mobileSheetOpen, async (v) => {
  if (typeof document === 'undefined') return
  document.body.style.overflow = v ? 'hidden' : ''
  if (v) {
    lastFocusedBeforeSheet = (document.activeElement as HTMLElement | null) ?? null
    await nextTick()
    // Move focus into the sheet so subsequent Tab cycles inside the
    // trap. Targeting the first focusable element keeps screen-reader
    // ordering predictable.
    const firstFocusable = sheetRef.value?.querySelector<HTMLElement>(
        'button, [href], input, [tabindex]:not([tabindex="-1"])',
    )
    firstFocusable?.focus()
  } else {
    // Restore focus to the caller (typically the dock button) so
    // keyboard users land back where they started.
    lastFocusedBeforeSheet?.focus()
    lastFocusedBeforeSheet = null
  }
})

// Constrain Tab key navigation to the sheet while it's open (A.3.9 focus
// trap). Skips when no sheet element is mounted yet.
function trapTab(e: KeyboardEvent) {
  if (!mobileSheetOpen.value || e.key !== 'Tab' || !sheetRef.value) return
  const focusables = sheetRef.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )
  if (focusables.length === 0) return
  const first = focusables[0]
  const last = focusables[focusables.length - 1]
  const active = document.activeElement as HTMLElement | null
  if (e.shiftKey && active === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && active === last) {
    e.preventDefault()
    first.focus()
  }
}

onMounted(() => document.addEventListener('keydown', trapTab))
onUnmounted(() => {
  if (typeof document !== 'undefined') {
    document.body.style.overflow = ''
    document.removeEventListener('keydown', trapTab)
  }
})

// ── Determine layout mode (avoid SSR mismatch) ──────────────────
const isMobile = ref(false)
// Tracks whether the desktop viewport is narrow enough that an open
// sidebar would crowd <main> (handoff A.6 risk #1, "auto-collapse to
// rail when viewport < 1100px"). When true and the user hasn't
// explicitly re-expanded it for this width, force the rail.
const isNarrowDesktop = ref(false)
let userOverrodeNarrow = false

function syncIsMobile() {
  if (typeof window === 'undefined') return
  isMobile.value = window.matchMedia('(max-width: 768px)').matches
  const narrow = !isMobile.value && window.matchMedia('(max-width: 1099px)').matches
  if (narrow !== isNarrowDesktop.value) {
    isNarrowDesktop.value = narrow
    // Auto-collapse on entering narrow range; restore on leaving.
    // Skip when the user explicitly re-expanded — their choice wins
    // until the next time they re-enter narrow range from wide.
    if (narrow && open.value && !userOverrodeNarrow) {
      sb.collapse()
    } else if (!narrow) {
      userOverrodeNarrow = false
    }
  }
}

// Track explicit expand events while narrow so we don't immediately
// re-collapse on the next resize event. Wrapping sb.expand directly is
// cleaner than a watch since collapse() can also fire from the user.
watch(open, (v) => {
  if (v && isNarrowDesktop.value) userOverrodeNarrow = true
})

onMounted(() => {
  syncIsMobile()
  window.addEventListener('resize', syncIsMobile)
})
onUnmounted(() => {
  if (typeof window !== 'undefined') window.removeEventListener('resize', syncIsMobile)
})

const railState = computed<'open' | 'rail'>(() => open.value ? 'open' : 'rail')
</script>

<template>
  <Teleport to="body">
    <!-- Desktop sidebar (md and up) -->
    <aside
        v-if="visible && !isMobile"
        class="sb-root"
        :class="{ 'sb-root--open': open, 'sb-root--rail': !open }"
        :data-state="railState"
        role="complementary"
        aria-label="Now playing"
    >
      <!-- Expanded -->
      <div v-if="open" class="sb">
        <header class="sb__head">
          <h2 class="sb__heading">Now playing</h2>
          <div class="sb__head-actions">
            <button
                v-if="playback.currentMediaId"
                class="sb__icon-btn"
                aria-label="Open in full player"
                title="Open in full player"
                @click="openPlayer"
            >
              <UIcon name="i-lucide-chevron-right" class="size-3.5"/>
            </button>
            <button class="sb__icon-btn" aria-label="Collapse sidebar" title="Collapse (])" @click="sb.collapse()">
              <UIcon name="i-lucide-panel-right" class="size-3.5"/>
            </button>
          </div>
        </header>

        <!-- Now Playing card -->
        <div v-if="playback.mediaInfo" class="np">
          <div
              class="np__art"
              :style="{ background: playback.mediaInfo.thumbnail_url ? '#000' : getMediaGradient(playback.currentMediaId || 'x') }"
          >
            <img
                v-if="playback.mediaInfo.thumbnail_url"
                :src="playback.mediaInfo.thumbnail_url"
                :alt="getDisplayTitle(playback.mediaInfo)"
                class="np__art-img"
                @error="($event.target as HTMLImageElement).style.display='none'"
            />
            <UIcon
                v-else
                :name="playback.mediaInfo.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                class="np__art-glyph size-7"
            />
            <span class="np__live"><span class="np__live-dot"/>Now playing</span>
          </div>
          <p class="np__title" :title="getDisplayTitle(playback.mediaInfo)">{{
              getDisplayTitle(playback.mediaInfo)
            }}</p>
          <div class="np__scrub">
            <div class="np__bar">
              <div class="np__bar-fill" :style="{ width: `${progressPct}%` }"/>
            </div>
            <div class="np__time">
              <span>{{ formatDuration(Math.floor(playback.position)) }}</span>
              <span class="np__time-dim">{{ formatDuration(playback.mediaInfo.duration ?? playback.duration) }}</span>
            </div>
          </div>
          <div class="np__controls">
            <button class="np-btn" aria-label="Previous" @click="playNext">
              <UIcon name="i-lucide-skip-back" class="size-4"/>
            </button>
            <button class="np-btn np-btn--play" aria-label="Open player" @click="openPlayer">
              <UIcon :name="playback.isPlaying ? 'i-lucide-pause' : 'i-lucide-play'" class="size-4"/>
            </button>
            <button class="np-btn" aria-label="Next" @click="playNext">
              <UIcon name="i-lucide-skip-forward" class="size-4"/>
            </button>
          </div>
        </div>

        <!-- Empty playing state with CTAs (B.3 + A.3.2). -->
        <div v-else class="empty">
          <div class="empty__icon">
            <UIcon name="i-lucide-music-2" class="size-5"/>
          </div>
          <p class="empty__title">Nothing playing yet</p>
          <p class="empty__sub">Press play on any item — controls and queue will appear here.</p>
          <div class="empty__cta">
            <NuxtLink to="/" class="empty__btn empty__btn--primary">
              <UIcon name="i-lucide-compass" class="size-3.5"/>
              Browse library
            </NuxtLink>
            <button
                v-if="authStore.isLoggedIn"
                type="button"
                class="empty__btn empty__btn--ghost"
                :aria-pressed="autoplaySimilar"
                :title="autoplaySimilar ? 'Click to turn off' : 'Click to turn on'"
                @click="toggleAutoplaySimilar"
            >
              <UIcon :name="autoplaySimilar ? 'i-lucide-toggle-right' : 'i-lucide-toggle-left'" class="size-3.5"/>
              Autoplay similar — {{ autoplaySimilar ? 'ON' : 'OFF' }}
            </button>
          </div>
        </div>

        <!-- Tabs -->
        <div class="sb__tabs" role="tablist">
          <button
              role="tab"
              :aria-selected="tab === 'queue'"
              class="sb__tab"
              :class="{ 'sb__tab--on': tab === 'queue' }"
              @click="sb.setTab('queue')"
          >
            <UIcon name="i-lucide-list" class="size-3.5"/>
            Queue
            <span class="sb__tab-count">{{ queue.items.length }}</span>
          </button>
          <button
              role="tab"
              :aria-selected="tab === 'playlist'"
              class="sb__tab"
              :class="{ 'sb__tab--on': tab === 'playlist' }"
              @click="sb.setTab('playlist')"
          >
            <UIcon name="i-lucide-music-2" class="size-3.5"/>
            Playlist
            <span v-if="pinnedPlaylist" class="sb__tab-count">{{ pinnedPlaylist.items?.length ?? 0 }}</span>
          </button>
        </div>

        <!-- Queue body -->
        <div v-if="tab === 'queue'" class="sb__body scroll-thin">
          <template v-if="queue.items.length > 0">
            <div class="sb__sub">
              <span class="sb__sub-title">Up next</span>
              <button class="sb__sub-link sb__sub-link--danger" @click="clearQueue">
                <UIcon name="i-lucide-trash" class="size-3"/>
                Clear
              </button>
            </div>
            <ul class="rows">
              <li v-for="(item, i) in queue.items" :key="item.id" class="row">
                <span class="row__index">{{ String(i + 1).padStart(2, '0') }}</span>
                <div class="thumb" :style="{ background: getMediaGradient(item.id) }">
                  <img v-if="item.thumbnail_url" :src="item.thumbnail_url" :alt="getDisplayTitle(item)"
                       class="thumb__img" @error="($event.target as HTMLImageElement).style.display='none'"/>
                  <UIcon v-else :name="item.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                         class="thumb__glyph size-3.5"/>
                </div>
                <div class="row__meta">
                  <p class="row__title" :title="getDisplayTitle(item)">{{ getDisplayTitle(item) }}</p>
                  <p class="row__sub"><span class="row__dur">{{ formatDuration(item.duration) }}</span></p>
                </div>
                <div class="row__actions">
                  <button class="row-btn" aria-label="Move up" :disabled="i === 0" @click="moveUp(item.id)">
                    <UIcon name="i-lucide-chevron-up" class="size-3"/>
                  </button>
                  <button class="row-btn" aria-label="Move down" :disabled="i === queue.items.length - 1"
                          @click="moveDown(item.id)">
                    <UIcon name="i-lucide-chevron-down" class="size-3"/>
                  </button>
                  <button class="row-btn" aria-label="Play now" @click="playFromQueue(item.id)">
                    <UIcon name="i-lucide-play" class="size-3"/>
                  </button>
                  <button class="row-btn" aria-label="Remove" @click="removeFromQueue(item.id)">
                    <UIcon name="i-lucide-x" class="size-3.5"/>
                  </button>
                </div>
              </li>
            </ul>
            <div class="sb__hint">
              <UIcon name="i-lucide-clock" class="size-3"/>
              {{ formatDuration(queueTotalDuration) }} remaining · {{ queue.items.length }} item<span
                v-if="queue.items.length !== 1">s</span>
            </div>
          </template>
          <div v-else class="empty">
            <div class="empty__icon">
              <UIcon name="i-lucide-list" class="size-5"/>
            </div>
            <p class="empty__title">Nothing queued up</p>
            <p class="empty__sub">Use the "Add to queue" menu on any card.</p>
          </div>
        </div>

        <!-- Playlist body -->
        <div v-else class="sb__body scroll-thin">
          <template v-if="pinnedPlaylist">
            <div class="pl-head">
              <p class="pl-head__name">{{ pinnedPlaylist.name }}</p>
              <div class="pl-head__row">
                <p class="pl-head__sub">{{ pinnedPlaylist.items?.length ?? 0 }} items</p>
                <button class="sb__sub-link" @click="unpinPlaylist">
                  <UIcon name="i-lucide-pin-off" class="size-3"/>
                  Unpin
                </button>
              </div>
            </div>
            <ul v-if="(pinnedPlaylist.items?.length ?? 0) > 0" class="rows">
              <li v-for="(item, i) in pinnedPlaylist.items ?? []" :key="(item.id ?? item.media_id) + '-' + i"
                  class="row">
                <span class="row__index">{{ String(i + 1).padStart(2, '0') }}</span>
                <div class="thumb" :style="{ background: getMediaGradient(item.media_id) }">
                  <UIcon name="i-lucide-film" class="thumb__glyph size-3.5"/>
                </div>
                <div class="row__meta">
                  <p class="row__title">{{ getDisplayTitle(item) }}</p>
                </div>
                <div class="row__actions">
                  <NuxtLink :to="`/player?id=${encodeURIComponent(item.media_id)}`" class="row-btn" aria-label="Play">
                    <UIcon name="i-lucide-play" class="size-3"/>
                  </NuxtLink>
                </div>
              </li>
            </ul>
            <div v-else class="empty">
              <div class="empty__icon">
                <UIcon name="i-lucide-music-2" class="size-5"/>
              </div>
              <p class="empty__title">Playlist is empty</p>
              <p class="empty__sub">Add items in
                <NuxtLink to="/playlists" class="empty__link">Playlists</NuxtLink>
                .
              </p>
            </div>
          </template>
          <div v-else class="empty">
            <div class="empty__icon">
              <UIcon name="i-lucide-music-2" class="size-5"/>
            </div>
            <p class="empty__title">No playlist pinned</p>
            <p class="empty__sub">Open
              <NuxtLink to="/playlists" class="empty__link">Playlists</NuxtLink>
              and play one — it'll appear here.
            </p>
          </div>
        </div>
      </div>

      <!-- Collapsed rail -->
      <div v-else class="rail">
        <button class="rail__icon-btn rail__icon-btn--accent" aria-label="Expand" title="Expand ([)"
                @click="sb.expand()">
          <UIcon name="i-lucide-panel-left" class="size-4"/>
        </button>
        <div v-if="playback.mediaInfo" class="rail__art"
             :style="{ background: getMediaGradient(playback.currentMediaId || 'x') }">
          <img v-if="playback.mediaInfo.thumbnail_url" :src="playback.mediaInfo.thumbnail_url"
               :alt="getDisplayTitle(playback.mediaInfo)" class="rail__art-img"
               @error="($event.target as HTMLImageElement).style.display='none'"/>
          <UIcon v-else :name="playback.mediaInfo.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                 class="size-3.5"/>
        </div>
        <!-- EQ-style activity bars — only animate when isPlaying, frozen
             otherwise. Respects reduced-motion via main.css's global rule. -->
        <div
            v-if="playback.mediaInfo"
            class="rail__eq"
            :class="{ 'rail__eq--on': playback.isPlaying }"
            :aria-label="playback.isPlaying ? 'Playing' : 'Paused'"
            aria-hidden="true"
        >
          <span class="rail__eq-bar"/>
          <span class="rail__eq-bar"/>
          <span class="rail__eq-bar"/>
        </div>
        <button class="rail__icon-btn" aria-label="Previous" @click="playNext">
          <UIcon name="i-lucide-skip-back" class="size-3.5"/>
        </button>
        <button class="rail__icon-btn rail__icon-btn--play" aria-label="Open player" @click="openPlayer">
          <UIcon :name="playback.isPlaying ? 'i-lucide-pause' : 'i-lucide-play'" class="size-3.5"/>
        </button>
        <button class="rail__icon-btn" aria-label="Next" @click="playNext">
          <UIcon name="i-lucide-skip-forward" class="size-3.5"/>
        </button>
        <div class="rail__counts">
          <div class="rail__count" :title="`${queue.items.length} in queue`">
            <UIcon name="i-lucide-list" class="size-3"/>
            <span>{{ queue.items.length }}</span>
          </div>
          <div v-if="pinnedPlaylist" class="rail__count"
               :title="`${pinnedPlaylist.items?.length ?? 0} in pinned playlist`">
            <UIcon name="i-lucide-music-2" class="size-3"/>
            <span>{{ pinnedPlaylist.items?.length ?? 0 }}</span>
          </div>
        </div>
      </div>
    </aside>

    <!-- Mobile dock (always visible at bottom on small screens, when sidebar is in scope) -->
    <div v-if="visible && isMobile" class="dock-root">
      <button
          class="dock"
          :aria-label="playback.mediaInfo ? `Now playing: ${getDisplayTitle(playback.mediaInfo)}` : 'Open sidebar'"
          @click="mobileSheetOpen = true"
      >
        <div v-if="playback.mediaInfo" class="dock__art"
             :style="{ background: getMediaGradient(playback.currentMediaId || 'x') }">
          <img v-if="playback.mediaInfo.thumbnail_url" :src="playback.mediaInfo.thumbnail_url"
               :alt="getDisplayTitle(playback.mediaInfo)" class="dock__art-img"
               @error="($event.target as HTMLImageElement).style.display='none'"/>
          <UIcon v-else :name="playback.mediaInfo.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                 class="size-4"/>
        </div>
        <div v-else class="dock__art dock__art--empty">
          <UIcon name="i-lucide-music-2" class="size-4"/>
        </div>
        <div class="dock__meta">
          <p v-if="playback.mediaInfo" class="dock__title">{{ getDisplayTitle(playback.mediaInfo) }}</p>
          <p v-else class="dock__title">Now Playing</p>
          <p class="dock__sub">
            <UIcon name="i-lucide-list" class="size-3"/>
            {{ queue.items.length }} in queue
          </p>
        </div>
        <UIcon name="i-lucide-chevron-up" class="size-4 text-[var(--text-muted)]"/>
      </button>
      <div v-if="playback.mediaInfo" class="dock__bar">
        <div class="dock__bar-fill" :style="{ width: `${progressPct}%` }"/>
      </div>
    </div>

    <!-- Mobile full-height sheet -->
    <Transition name="sheet">
      <div
          v-if="visible && isMobile && mobileSheetOpen"
          class="sheet-backdrop"
          @click="mobileSheetOpen = false"
      >
        <div
            ref="sheetRef"
            class="sheet"
            role="dialog"
            aria-modal="true"
            aria-label="Now playing"
            @click.stop
        >
          <header class="sheet__head">
            <button class="sb__icon-btn" aria-label="Close" @click="mobileSheetOpen = false">
              <UIcon name="i-lucide-chevron-down" class="size-4"/>
            </button>
            <h2 class="sheet__heading">Now playing</h2>
            <button
                v-if="playback.currentMediaId"
                class="sb__icon-btn"
                aria-label="Open in full player"
                @click="openPlayer"
            >
              <UIcon name="i-lucide-maximize-2" class="size-4"/>
            </button>
            <span v-else class="sb__icon-btn" aria-hidden="true"/>
          </header>

          <div class="sheet__body scroll-thin">
            <div v-if="playback.mediaInfo" class="np">
              <div class="np__art"
                   :style="{ background: playback.mediaInfo.thumbnail_url ? '#000' : getMediaGradient(playback.currentMediaId || 'x') }">
                <img v-if="playback.mediaInfo.thumbnail_url" :src="playback.mediaInfo.thumbnail_url"
                     :alt="getDisplayTitle(playback.mediaInfo)" class="np__art-img"/>
                <UIcon v-else :name="playback.mediaInfo.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                       class="np__art-glyph size-7"/>
                <span class="np__live"><span class="np__live-dot"/>Now playing</span>
              </div>
              <p class="np__title" :title="getDisplayTitle(playback.mediaInfo)">{{
                  getDisplayTitle(playback.mediaInfo)
                }}</p>
              <div class="np__scrub">
                <div class="np__bar">
                  <div class="np__bar-fill" :style="{ width: `${progressPct}%` }"/>
                </div>
                <div class="np__time">
                  <span>{{ formatDuration(Math.floor(playback.position)) }}</span>
                  <span class="np__time-dim">{{
                      formatDuration(playback.mediaInfo.duration ?? playback.duration)
                    }}</span>
                </div>
              </div>
              <div class="np__controls">
                <button class="np-btn" aria-label="Previous" @click="playNext">
                  <UIcon name="i-lucide-skip-back" class="size-4"/>
                </button>
                <button class="np-btn np-btn--play" aria-label="Open player" @click="openPlayer">
                  <UIcon :name="playback.isPlaying ? 'i-lucide-pause' : 'i-lucide-play'" class="size-4"/>
                </button>
                <button class="np-btn" aria-label="Next" @click="playNext">
                  <UIcon name="i-lucide-skip-forward" class="size-4"/>
                </button>
              </div>
            </div>
            <div v-else class="empty">
              <div class="empty__icon">
                <UIcon name="i-lucide-music-2" class="size-5"/>
              </div>
              <p class="empty__title">Nothing playing yet</p>
              <p class="empty__sub">Press play on any item — controls and queue will appear here.</p>
              <div class="empty__cta">
                <NuxtLink to="/" class="empty__btn empty__btn--primary" @click="mobileSheetOpen = false">
                  <UIcon name="i-lucide-compass" class="size-3.5"/>
                  Browse library
                </NuxtLink>
                <button
                    v-if="authStore.isLoggedIn"
                    type="button"
                    class="empty__btn empty__btn--ghost"
                    :aria-pressed="autoplaySimilar"
                    @click="toggleAutoplaySimilar"
                >
                  <UIcon :name="autoplaySimilar ? 'i-lucide-toggle-right' : 'i-lucide-toggle-left'" class="size-3.5"/>
                  Autoplay similar — {{ autoplaySimilar ? 'ON' : 'OFF' }}
                </button>
              </div>
            </div>

            <div class="sb__tabs" role="tablist">
              <button role="tab" :aria-selected="tab === 'queue'" class="sb__tab"
                      :class="{ 'sb__tab--on': tab === 'queue' }" @click="sb.setTab('queue')">
                <UIcon name="i-lucide-list" class="size-3.5"/>
                Queue
                <span class="sb__tab-count">{{ queue.items.length }}</span>
              </button>
              <button role="tab" :aria-selected="tab === 'playlist'" class="sb__tab"
                      :class="{ 'sb__tab--on': tab === 'playlist' }" @click="sb.setTab('playlist')">
                <UIcon name="i-lucide-music-2" class="size-3.5"/>
                Playlist
                <span v-if="pinnedPlaylist" class="sb__tab-count">{{ pinnedPlaylist.items?.length ?? 0 }}</span>
              </button>
            </div>

            <div v-if="tab === 'queue'">
              <template v-if="queue.items.length > 0">
                <div class="sb__sub">
                  <span class="sb__sub-title">Up next</span>
                  <button class="sb__sub-link sb__sub-link--danger" @click="clearQueue">
                    <UIcon name="i-lucide-trash" class="size-3"/>
                    Clear
                  </button>
                </div>
                <ul class="rows">
                  <li v-for="(item, i) in queue.items" :key="item.id" class="row">
                    <span class="row__index">{{ String(i + 1).padStart(2, '0') }}</span>
                    <div class="thumb" :style="{ background: getMediaGradient(item.id) }">
                      <img v-if="item.thumbnail_url" :src="item.thumbnail_url" :alt="getDisplayTitle(item)"
                           class="thumb__img" @error="($event.target as HTMLImageElement).style.display='none'"/>
                      <UIcon v-else :name="item.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                             class="thumb__glyph size-3.5"/>
                    </div>
                    <div class="row__meta">
                      <p class="row__title">{{ getDisplayTitle(item) }}</p>
                      <p class="row__sub"><span class="row__dur">{{ formatDuration(item.duration) }}</span></p>
                    </div>
                    <div class="row__actions row__actions--mobile">
                      <button class="row-btn" aria-label="Play now"
                              @click="() => { mobileSheetOpen = false; playFromQueue(item.id) }">
                        <UIcon name="i-lucide-play" class="size-3.5"/>
                      </button>
                      <button class="row-btn" aria-label="Remove" @click="removeFromQueue(item.id)">
                        <UIcon name="i-lucide-x" class="size-4"/>
                      </button>
                    </div>
                  </li>
                </ul>
              </template>
              <div v-else class="empty">
                <div class="empty__icon">
                  <UIcon name="i-lucide-list" class="size-5"/>
                </div>
                <p class="empty__title">Nothing queued up</p>
              </div>
            </div>

            <div v-else>
              <template v-if="pinnedPlaylist">
                <div class="pl-head">
                  <p class="pl-head__name">{{ pinnedPlaylist.name }}</p>
                  <p class="pl-head__sub">{{ pinnedPlaylist.items?.length ?? 0 }} items</p>
                </div>
                <ul v-if="(pinnedPlaylist.items?.length ?? 0) > 0" class="rows">
                  <li v-for="(item, i) in pinnedPlaylist.items ?? []" :key="(item.id ?? item.media_id) + '-' + i"
                      class="row">
                    <span class="row__index">{{ String(i + 1).padStart(2, '0') }}</span>
                    <div class="thumb" :style="{ background: getMediaGradient(item.media_id) }">
                      <UIcon name="i-lucide-film" class="thumb__glyph size-3.5"/>
                    </div>
                    <div class="row__meta"><p class="row__title">{{ getDisplayTitle(item) }}</p></div>
                    <div class="row__actions row__actions--mobile">
                      <NuxtLink :to="`/player?id=${encodeURIComponent(item.media_id)}`" class="row-btn"
                                aria-label="Play" @click="mobileSheetOpen = false">
                        <UIcon name="i-lucide-play" class="size-3.5"/>
                      </NuxtLink>
                    </div>
                  </li>
                </ul>
              </template>
              <div v-else class="empty">
                <div class="empty__icon">
                  <UIcon name="i-lucide-music-2" class="size-5"/>
                </div>
                <p class="empty__title">No playlist pinned</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* Tokens (--accent, --surface-card, --hairline, etc.) come from main.css. */

.sb-root {
  position: fixed;
  top: 60px;
  right: 0;
  bottom: 0;
  z-index: 30;
  display: flex;
  background: var(--surface-card);
  border-left: 1px solid var(--hairline);
  transition: width var(--motion-hover, 150ms linear);
}

.sb-root--open {
  width: var(--sb-width-open, 340px);
}

.sb-root--rail {
  width: var(--sb-width-rail, 56px);
}

.sb {
  width: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.sb__head {
  height: 48px;
  padding: 0 14px 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--hairline);
}

.sb__heading {
  margin: 0;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.sb__head-actions {
  display: flex;
  gap: 4px;
}

.sb__icon-btn {
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 6px;
  color: var(--text-muted);
  background: transparent;
  cursor: pointer;
  transition: background var(--motion-hover), color var(--motion-hover);
}

.sb__icon-btn:hover {
  color: var(--text-strong);
  background: rgba(255, 255, 255, 0.05);
}

/* Now Playing card */
.np {
  padding: 16px;
  border-bottom: 1px solid var(--hairline);
}

.np__art {
  position: relative;
  width: 100%;
  aspect-ratio: 16/9;
  border-radius: 12px;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 12px;
  box-shadow: 0 8px 22px -6px rgba(0, 0, 0, 0.5);
}

.np__art-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.np__art-glyph {
  color: rgba(255, 255, 255, 0.85);
}

.np__live {
  position: absolute;
  top: 10px;
  left: 10px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: white;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(6px);
  border-radius: 999px;
  padding: 4px 9px 4px 7px;
  border: 1px solid rgba(255, 255, 255, 0.12);
}

.np__live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #ef4444;
  box-shadow: 0 0 8px rgba(239, 68, 68, 0.8);
  animation: sb-pulse 1.4s ease-in-out infinite;
}

@keyframes sb-pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}

@media (prefers-reduced-motion: reduce) {
  .np__live-dot {
    animation: none;
  }
}

.np__title {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
  color: var(--text-strong);
  line-height: 1.3;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.np__scrub {
  margin: 10px 0;
}

.np__bar {
  height: 4px;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 2px;
  overflow: hidden;
}

.np__bar-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 2px;
  transition: width 250ms linear;
}

.np__time {
  display: flex;
  justify-content: space-between;
  margin-top: 6px;
  font-family: ui-monospace, monospace;
  font-size: 10.5px;
  color: var(--text-med);
}

.np__time-dim {
  color: var(--text-muted);
}

.np__controls {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.np-btn {
  width: 34px;
  height: 34px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--text-med);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: background var(--motion-hover), color var(--motion-hover);
}

.np-btn:hover {
  color: var(--text-strong);
  background: rgba(255, 255, 255, 0.05);
}

.np-btn--play {
  width: 42px;
  height: 42px;
  border-radius: 50%;
  background: var(--text-strong);
  color: var(--surface-page);
}

.np-btn--play:hover {
  background: white;
}

/* Tabs */
.sb__tabs {
  display: flex;
  padding: 0 12px;
  border-bottom: 1px solid var(--hairline);
  gap: 4px;
}

.sb__tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 12px 10px;
  border: 0;
  background: transparent;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  cursor: pointer;
  transition: color var(--motion-hover);
  font-family: inherit;
}

.sb__tab--on {
  color: var(--text-strong);
}

.sb__tab--on::after {
  content: "";
  position: absolute;
  left: 10px;
  right: 10px;
  bottom: -1px;
  height: 2px;
  background: var(--accent);
  border-radius: 1px 1px 0 0;
}

.sb__tab-count {
  font-family: ui-monospace, monospace;
  font-size: 10px;
  padding: 1px 6px;
  background: rgba(255, 255, 255, 0.04);
  border-radius: 999px;
  color: var(--text-muted);
  border: 1px solid var(--hairline);
  font-weight: 600;
}

.sb__tab--on .sb__tab-count {
  background: var(--accent-bg-med);
  border-color: var(--accent-border);
  color: var(--accent-soft);
}

/* Body + rows */
.sb__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 10px 8px 14px;
}

.sb__sub {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px 8px;
}

.sb__sub-title {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.sb__sub-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: transparent;
  border: 0;
  color: var(--text-muted);
  font-size: 10.5px;
  font-weight: 600;
  padding: 4px 8px;
  border-radius: 5px;
  cursor: pointer;
  font-family: inherit;
}

.sb__sub-link:hover {
  color: var(--text-strong);
  background: rgba(255, 255, 255, 0.04);
}

.sb__sub-link--danger:hover {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.08);
}

.rows {
  list-style: none;
  padding: 0;
  margin: 0;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px;
  border-radius: 8px;
  transition: background var(--motion-hover);
}

.row:hover {
  background: rgba(255, 255, 255, 0.03);
}

.row:hover .row__actions {
  opacity: 1;
}

.row__index {
  width: 18px;
  text-align: center;
  font-family: ui-monospace, monospace;
  font-size: 10px;
  color: var(--text-muted);
}

.thumb {
  position: relative;
  width: 44px;
  aspect-ratio: 16/9;
  border-radius: 7px;
  overflow: hidden;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.thumb__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.thumb__glyph {
  color: rgba(255, 255, 255, 0.85);
}

.row__meta {
  flex: 1;
  min-width: 0;
}

.row__title {
  margin: 0;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--text-strong);
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.row__sub {
  margin: 2px 0 0;
  font-size: 10.5px;
  color: var(--text-muted);
}

.row__dur {
  font-family: ui-monospace, monospace;
  font-size: 10px;
  color: var(--text-faint);
}

.row__actions {
  display: flex;
  gap: 2px;
  opacity: 0;
  transition: opacity var(--motion-hover);
}

.row__actions--mobile {
  opacity: 1;
  gap: 4px;
}

.row-btn {
  width: 26px;
  height: 26px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  border-radius: 5px;
  cursor: pointer;
}

.row-btn:hover:not(:disabled) {
  color: var(--text-strong);
  background: rgba(255, 255, 255, 0.06);
}

.row-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.sb__hint {
  margin: 10px 14px 4px;
  padding-top: 12px;
  border-top: 1px dashed var(--hairline);
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10.5px;
  color: var(--text-muted);
}

/* Empty */
.empty {
  margin: 24px 12px;
  padding: 22px 16px;
  border: 1px dashed var(--hairline-strong);
  border-radius: 12px;
  text-align: center;
  background: rgba(255, 255, 255, 0.015);
}

.empty__icon {
  width: 40px;
  height: 40px;
  margin: 0 auto 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--accent-bg-weak);
  color: var(--accent-soft);
}

.empty__title {
  margin: 0 0 6px;
  font-size: 13px;
  font-weight: 700;
  color: var(--text-strong);
}

.empty__sub {
  margin: 0;
  font-size: 11.5px;
  color: var(--text-muted);
  line-height: 1.5;
}

.empty__sub kbd {
  font-family: ui-monospace, monospace;
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 3px;
  border: 1px solid var(--hairline-strong);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-strong);
}

.empty__link {
  color: var(--accent-soft);
  text-decoration: underline;
}

/* CTAs inside the "nothing playing" empty state — A.3.2 + B.3. The
   Autoplay-similar button is a true toggle so users can flip the
   preference without leaving the sidebar. */
.empty__cta {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 14px;
}

.empty__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 12px;
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
  border: 0;
  font-family: inherit;
  transition: background var(--motion-hover), color var(--motion-hover);
}

.empty__btn--primary {
  background: var(--accent);
  color: white;
}

.empty__btn--primary:hover {
  filter: brightness(1.1);
}

.empty__btn--ghost {
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-med);
  border: 1px solid var(--hairline);
}

.empty__btn--ghost:hover {
  color: var(--text-strong);
  background: rgba(255, 255, 255, 0.07);
}

.empty__btn--ghost[aria-pressed="true"] {
  color: var(--accent-soft);
  border-color: var(--accent-border);
  background: var(--accent-bg-weak);
}

/* Playlist header */
.pl-head {
  padding: 12px 12px 12px;
  margin: -10px -8px 6px;
  border-bottom: 1px solid var(--hairline);
}

.pl-head__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 2px;
}

.pl-head__name {
  margin: 0;
  font-size: 13px;
  font-weight: 700;
  color: var(--text-strong);
}

.pl-head__sub {
  margin: 0;
  font-size: 11px;
  color: var(--text-muted);
}

/* Collapsed rail */
.rail {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 10px 0;
  gap: 10px;
}

.rail__icon-btn {
  width: 34px;
  height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.rail__icon-btn:hover {
  background: rgba(255, 255, 255, 0.05);
  color: var(--text-strong);
}

.rail__icon-btn--accent {
  color: var(--accent-soft);
  background: var(--accent-bg-weak);
}

.rail__icon-btn--play {
  background: var(--text-strong);
  color: var(--surface-page);
  width: 36px;
  height: 36px;
}

.rail__art {
  position: relative;
  width: 36px;
  height: 36px;
  border-radius: 7px;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255, 255, 255, 0.85);
}

.rail__art-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

/* EQ activity indicator on the rail. Three thin bars; animation only
   runs when the parent has --on. Reduced-motion users see them frozen. */
.rail__eq {
  display: flex;
  align-items: flex-end;
  justify-content: center;
  gap: 2px;
  height: 14px;
  padding: 0 2px;
}

.rail__eq-bar {
  width: 3px;
  height: 4px;
  background: var(--text-faint);
  border-radius: 1px;
  transition: background var(--motion-hover);
}

.rail__eq--on .rail__eq-bar {
  background: var(--accent);
  animation: sb-eq 0.9s ease-in-out infinite;
}

.rail__eq--on .rail__eq-bar:nth-child(2) {
  animation-delay: 0.15s;
}

.rail__eq--on .rail__eq-bar:nth-child(3) {
  animation-delay: 0.3s;
}

@keyframes sb-eq {
  0%, 100% {
    height: 4px;
  }
  50% {
    height: 12px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .rail__eq--on .rail__eq-bar {
    animation: none;
    height: 8px;
  }
}

.rail__counts {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  margin-top: auto;
}

.rail__count {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
  color: var(--text-muted);
  font-family: ui-monospace, monospace;
  font-size: 10px;
  font-weight: 600;
}

/* ── Mobile dock ─────────────────────────────────────────────────── */
.dock-root {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 40;
  background: var(--surface-card);
  border-top: 1px solid var(--hairline);
  box-shadow: 0 -8px 24px -10px rgba(0, 0, 0, 0.4);
}

.dock {
  width: 100%;
  height: var(--sb-width-mobile, 60px);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 12px;
  background: transparent;
  border: 0;
  cursor: pointer;
  font-family: inherit;
  color: inherit;
  text-align: left;
}

.dock__art {
  width: 40px;
  aspect-ratio: 16/9;
  border-radius: 6px;
  overflow: hidden;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255, 255, 255, 0.85);
}

.dock__art--empty {
  background: rgba(255, 255, 255, 0.05);
  color: var(--text-muted);
}

.dock__art-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.dock__meta {
  flex: 1;
  min-width: 0;
}

.dock__title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-strong);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dock__sub {
  margin: 1px 0 0;
  font-size: 11px;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
}

.dock__bar {
  height: 2px;
  background: rgba(255, 255, 255, 0.04);
}

.dock__bar-fill {
  height: 100%;
  background: var(--accent);
  transition: width 250ms linear;
}

/* ── Mobile full-height sheet ─────────────────────────────────── */
.sheet-backdrop {
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: flex-end;
}

.sheet {
  width: 100%;
  max-height: 86vh;
  background: var(--surface-card);
  border-top: 1px solid var(--hairline);
  border-radius: 16px 16px 0 0;
  box-shadow: 0 -8px 30px -8px rgba(0, 0, 0, 0.6);
  display: flex;
  flex-direction: column;
}

.sheet__head {
  height: 52px;
  padding: 0 12px;
  display: grid;
  grid-template-columns: 32px 1fr 32px;
  align-items: center;
  border-bottom: 1px solid var(--hairline);
}

.sheet__heading {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
  text-align: center;
  color: var(--text-strong);
}

.sheet__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-bottom: 16px;
}

/* Slide-up transition for the mobile sheet. Respects reduced-motion via the
   global rule in main.css. */
.sheet-enter-active,
.sheet-leave-active {
  transition: opacity 200ms ease;
}

.sheet-enter-active .sheet,
.sheet-leave-active .sheet {
  transition: transform 240ms ease;
}

.sheet-enter-from,
.sheet-leave-to {
  opacity: 0;
}

.sheet-enter-from .sheet,
.sheet-leave-to .sheet {
  transform: translateY(100%);
}
</style>
