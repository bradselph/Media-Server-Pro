<script setup lang="ts">
import type { MediaItem, Suggestion, Playlist, PlaylistItem } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration } from '~/utils/format'

definePageMeta({ layout: 'default', title: 'Player' })

const route = useRoute()
const mediaApi = useMediaApi()
const playbackApi = usePlaybackApi()
const suggestionsApi = useSuggestionsApi()
const ratingsApi = useRatingsApi()
const playlistApi = usePlaylistApi()
const analyticsApi = useAnalyticsApi()
const playbackStore = usePlaybackStore()
const authStore = useAuthStore()
const { updatePreferences } = useApiEndpoints()
const toast = useToast()

const userPrefs = computed(() => authStore.user?.preferences)

// Playlist add
const playlists = ref<Playlist[]>([])
const playlistOpen = ref(false)
const addingToPlaylist = ref(false)

async function openAddToPlaylist() {
  playlistOpen.value = true
  if (playlists.value.length === 0) {
    try { playlists.value = (await playlistApi.list()) ?? [] } catch { /* ignore */ }
  }
}

async function addToPlaylist(playlistId: string) {
  if (!mediaId.value) return
  addingToPlaylist.value = true
  try {
    await playlistApi.addItem(playlistId, mediaId.value)
    toast.add({ title: 'Added to playlist', color: 'success', icon: 'i-lucide-check' })
    playlistOpen.value = false
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { addingToPlaylist.value = false }
}

const mediaId = computed(() => route.query.id as string | undefined)
const media = ref<MediaItem | null>(null)
const loading = ref(true)
const error = ref('')

// Update browser tab title as soon as the media item loads
useHead(computed(() => ({
  title: media.value ? getDisplayTitle(media.value) : 'Player',
})))

// Player refs
const videoRef = ref<HTMLVideoElement | null>(null)
const isPlaying = ref(false)
const volume = ref(userPrefs.value?.volume ?? 1)
const currentTime = ref(0)
const duration = ref(0)
const showControls = ref(true)
const isFullscreen = ref(false)
const isTheater = ref(false)
const playbackSpeed = ref(userPrefs.value?.playback_speed ?? 1)

// HLS — delegate to composable
const mediaIdRef = computed(() => mediaId.value ?? '')
const {
  hlsAvailable,
  hlsActivated,
  hlsLoading,
  hlsError,
  qualities,
  currentQuality,
  selectQuality,
  activateHLS,
  jobProgress,
  jobRunning,
} = useHLS(videoRef, mediaIdRef)

// Request on-demand HLS generation
const hlsApi = useHlsApi()
const requestingHls = ref(false)

async function requestHlsGeneration() {
  if (!mediaId.value) return
  requestingHls.value = true
  try {
    await hlsApi.generate(mediaId.value)
    toast.add({ title: 'HLS generation started', color: 'info', icon: 'i-lucide-info' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to start HLS generation', color: 'error', icon: 'i-lucide-x' })
  } finally { requestingHls.value = false }
}

// Seek bar thumbnail previews
const thumbnailPreviews = ref<string[]>([])

// Similar & personalized recommendations
const similar = ref<Suggestion[]>([])
const personalized = ref<Suggestion[]>([])

// Mature content gate
const canViewMature = computed(() =>
  authStore.isLoggedIn &&
  (authStore.user?.preferences?.show_mature ?? false) &&
  (authStore.user?.permissions?.can_view_mature ?? false),
)
const matureGated = computed(() => !!(media.value?.is_mature && !canViewMature.value))

// Star rating (1-5). Optimistic update — fire and forget.
const userRating = ref(0)
function submitRating(star: number) {
  if (!mediaId.value || !authStore.isLoggedIn) return
  userRating.value = star
  ratingsApi.record(mediaId.value, star).catch(() => {})
}

let controlsTimer: ReturnType<typeof setTimeout> | null = null
let positionSaveController: AbortController | null = null

function resetControlsTimer() {
  showControls.value = true
  if (controlsTimer) clearTimeout(controlsTimer)
  controlsTimer = setTimeout(() => { if (isPlaying.value) showControls.value = false }, 3000)
}

async function loadMedia(id: string) {
  loading.value = true
  error.value = ''
  similar.value = []
  personalized.value = []
  thumbnailPreviews.value = []
  try {
    media.value = await mediaApi.getById(id)
    userRating.value = 0
    playbackStore.setMedia(id)
    suggestionsApi.getSimilar(id).then(r => { similar.value = r ?? [] }).catch(() => {})
    if (authStore.isLoggedIn) {
      suggestionsApi.getPersonalized(8).then(r => { personalized.value = r ?? [] }).catch(() => {})
    }
    mediaApi.getThumbnailPreviews(id).then(r => { thumbnailPreviews.value = r?.previews ?? [] }).catch(() => {})
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load media'
  } finally {
    loading.value = false
  }
}

async function restorePosition() {
  if (!mediaId.value || !videoRef.value) return
  // Honour ?t=N deep-link: seek to the given second, skipping the stored position.
  const tParam = Number(route.query.t)
  if (tParam > 0) {
    videoRef.value.currentTime = tParam
    return
  }
  // Respect the user's resume_playback preference (defaults to true when unset)
  if (userPrefs.value?.resume_playback === false) return
  try {
    const { position } = await playbackApi.getPosition(mediaId.value)
    if (position > 5 && videoRef.value) {
      videoRef.value.currentTime = position
    }
  } catch {}
}

async function savePosition() {
  if (!mediaId.value || !videoRef.value) return
  const pos = videoRef.value.currentTime
  const dur = videoRef.value.duration || 0
  if (pos > 0) {
    // Cancel any in-flight save to prevent out-of-order writes
    positionSaveController?.abort()
    positionSaveController = new AbortController()
    try {
      await playbackApi.savePosition(mediaId.value, pos, dur)
    } catch {
      // Position save is best-effort
    }
  }
}

function onVideoLoaded() {
  duration.value = videoRef.value?.duration ?? 0
  if (videoRef.value) {
    videoRef.value.playbackRate = playbackSpeed.value
    videoRef.value.volume = volume.value
  }
  restorePosition()
  playbackStore.startAutoSave()
}

function onTimeUpdate() {
  currentTime.value = videoRef.value?.currentTime ?? 0
  duration.value = videoRef.value?.duration ?? 0
  playbackStore.updatePosition(currentTime.value, duration.value)
}

function onPlayPause() {
  isPlaying.value = !videoRef.value?.paused
}

function togglePlay() {
  if (!videoRef.value) return
  videoRef.value.paused ? videoRef.value.play() : videoRef.value.pause()
}

function seek(delta: number) {
  if (!videoRef.value) return
  videoRef.value.currentTime = Math.max(0, Math.min(duration.value, currentTime.value + delta))
  trackSeek()
}

function seekToFraction(fraction: number) {
  if (!videoRef.value) return
  videoRef.value.currentTime = fraction * duration.value
  trackSeek()
}

function handleQualitySelect(index: number) {
  selectQuality(index)
  trackQualityChange(index)
}

function setVolume(v: number) {
  volume.value = v
  if (videoRef.value) videoRef.value.volume = v
  // Persist volume preference (debounced 1 s, fire-and-forget, logged-in users only)
  if (authStore.isLoggedIn) {
    if (volumeSaveTimer) clearTimeout(volumeSaveTimer)
    volumeSaveTimer = setTimeout(() => {
      updatePreferences({ volume: v }).catch(() => {})
      volumeSaveTimer = null
    }, 1000)
  }
}

function toggleFullscreen() {
  const el = document.querySelector('.player-wrapper') as HTMLElement
  if (!el) return
  if (!document.fullscreenElement) {
    el.requestFullscreen()
    isFullscreen.value = true
  } else {
    document.exitFullscreen()
    isFullscreen.value = false
  }
}

const isPiP = ref(false)
const pipSupported: boolean = !!(import.meta.client && 'pictureInPictureEnabled' in document)

// Playlist context (passed from playlists page via URL query params)
const playlistIdParam = computed(() => route.query.playlist_id as string | undefined)
const playlistIdxParam = computed(() => {
  const v = parseInt(route.query.playlist_idx as string ?? '')
  return isNaN(v) ? -1 : v
})
const playlistItems = ref<PlaylistItem[]>([])
const nextPlaylistItem = computed(() =>
  playlistIdxParam.value >= 0 ? (playlistItems.value[playlistIdxParam.value + 1] ?? null) : null,
)

let upNextTimer: ReturnType<typeof setInterval> | null = null
const showUpNext = ref(false)
const upNextCountdown = ref(5)

function startUpNextCountdown() {
  if (!nextPlaylistItem.value) return
  showUpNext.value = true
  upNextCountdown.value = 5
  upNextTimer = setInterval(() => {
    upNextCountdown.value -= 1
    if (upNextCountdown.value <= 0) navigateToNextItem()
  }, 1000)
}

function cancelUpNext() {
  if (upNextTimer) { clearInterval(upNextTimer); upNextTimer = null }
  showUpNext.value = false
}

function navigateToNextItem() {
  cancelUpNext()
  const next = nextPlaylistItem.value
  if (!next) return
  const newIdx = playlistIdxParam.value + 1
  navigateTo(`/player?id=${encodeURIComponent(next.media_id)}&playlist_id=${encodeURIComponent(playlistIdParam.value ?? '')}&playlist_idx=${newIdx}`)
}

watch(playlistIdParam, async id => {
  if (!id) { playlistItems.value = []; return }
  try {
    const pl = await playlistApi.get(id)
    playlistItems.value = pl?.items ?? []
  } catch { playlistItems.value = [] }
}, { immediate: true })

// Loop mode: 'off' | 'one'
const loopMode = ref<'off' | 'one'>('off')

function cycleLoop() {
  loopMode.value = loopMode.value === 'off' ? 'one' : 'off'
  if (videoRef.value) videoRef.value.loop = loopMode.value === 'one'
}

watch(loopMode, mode => {
  if (videoRef.value) videoRef.value.loop = mode === 'one'
})

async function togglePiP() {
  if (!videoRef.value) return
  try {
    if (document.pictureInPictureElement) {
      await document.exitPictureInPicture()
      isPiP.value = false
    } else {
      await videoRef.value.requestPictureInPicture()
      isPiP.value = true
    }
  } catch {
    // PiP not supported or denied — silently ignore
  }
}

// Keep isPiP in sync if user closes PiP via browser chrome
function onPiPChange() {
  isPiP.value = document.pictureInPictureElement === videoRef.value
}

// Keyboard shortcuts overlay
const showShortcuts = ref(false)

function toggleMute() {
  if (!videoRef.value) return
  setVolume(videoRef.value.volume > 0 ? 0 : Math.max(volume.value, 0.5))
}

function changeSpeed(delta: number) {
  const speeds = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
  const curIdx = speeds.indexOf(playbackSpeed.value)
  const newIdx = Math.max(0, Math.min(speeds.length - 1, curIdx + delta))
  playbackSpeed.value = speeds[newIdx]
  if (videoRef.value) videoRef.value.playbackRate = playbackSpeed.value
}

function stepFrame(direction: number) {
  if (!videoRef.value || !videoRef.value.paused) return
  // Approximate frame duration (~30fps = 0.033s)
  videoRef.value.currentTime = Math.max(0, Math.min(
    videoRef.value.duration,
    videoRef.value.currentTime + (direction * (1 / 30)),
  ))
}

function toggleTheater() {
  isTheater.value = !isTheater.value
}

function onKeyDown(e: KeyboardEvent) {
  // Don't intercept when user is typing in an input/textarea
  const tag = (e.target as HTMLElement)?.tagName?.toLowerCase()
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return

  switch (e.key) {
    case ' ':
    case 'k':
    case 'K':
      e.preventDefault()
      togglePlay()
      resetControlsTimer()
      break
    case 'j':
    case 'J':
      e.preventDefault()
      seek(-10)
      resetControlsTimer()
      break
    case 'l':
    case 'L':
      e.preventDefault()
      seek(10)
      resetControlsTimer()
      break
    case 'ArrowLeft':
      e.preventDefault()
      seek(-5)
      resetControlsTimer()
      break
    case 'ArrowRight':
      e.preventDefault()
      seek(5)
      resetControlsTimer()
      break
    case 'ArrowUp':
      e.preventDefault()
      setVolume(Math.min(1, volume.value + 0.05))
      break
    case 'ArrowDown':
      e.preventDefault()
      setVolume(Math.max(0, volume.value - 0.05))
      break
    case 'f':
    case 'F':
      e.preventDefault()
      toggleFullscreen()
      break
    case 't':
    case 'T':
      e.preventDefault()
      toggleTheater()
      break
    case 'm':
    case 'M':
      e.preventDefault()
      toggleMute()
      break
    case 'Home':
      e.preventDefault()
      if (videoRef.value) videoRef.value.currentTime = 0
      break
    case 'End':
      e.preventDefault()
      if (videoRef.value) videoRef.value.currentTime = videoRef.value.duration
      break
    case ',':
      e.preventDefault()
      stepFrame(-1)
      break
    case '.':
      e.preventDefault()
      stepFrame(1)
      break
    case '<':
      e.preventDefault()
      changeSpeed(-1)
      break
    case '>':
      e.preventDefault()
      changeSpeed(1)
      break
    case '?':
      e.preventDefault()
      showShortcuts.value = !showShortcuts.value
      break
    case 'Escape':
      if (showShortcuts.value) { e.preventDefault(); showShortcuts.value = false }
      break
    default:
      // 0-9: seek to percentage
      if (e.key >= '0' && e.key <= '9' && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault()
        seekToFraction(parseInt(e.key) / 10)
        resetControlsTimer()
      }
      break
  }
}

function onFullscreenChange() {
  isFullscreen.value = !!document.fullscreenElement
}

onMounted(() => {
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('keydown', onKeyDown)
})
onUnmounted(() => {
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('keydown', onKeyDown)
  if (controlsTimer) clearTimeout(controlsTimer)
})

function cycleSpeed() {
  const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]
  const idx = speeds.indexOf(playbackSpeed.value)
  playbackSpeed.value = speeds[(idx + 1) % speeds.length]
  if (videoRef.value) videoRef.value.playbackRate = playbackSpeed.value
  updatePreferences({ playback_speed: playbackSpeed.value }).catch(() => {})
}

// formatDuration imported from ~/utils/format

function formatBandwidth(bps: number): string {
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`
  if (bps >= 1_000) return `${(bps / 1_000).toFixed(0)} Kbps`
  return `${bps} bps`
}

const currentQualityLabel = computed(() => {
  if (currentQuality.value === -1) return 'Auto'
  return qualities.value[currentQuality.value]?.name ?? 'Auto'
})

// Analytics event helpers (fire-and-forget, never block playback)
let playEventSent = false
let seekTimer: ReturnType<typeof setTimeout> | null = null
let volumeSaveTimer: ReturnType<typeof setTimeout> | null = null

function trackPlay() {
  if (!mediaId.value) return
  if (!playEventSent) {
    playEventSent = true
    analyticsApi.submitEvent({ type: 'play', media_id: mediaId.value }).catch(() => {})
  } else {
    // Subsequent play after pause = resume
    analyticsApi.submitEvent({ type: 'resume', media_id: mediaId.value }).catch(() => {})
  }
}
function trackPause() {
  // Skip the synthetic pause that fires when the video reaches end (complete handles that)
  if (!mediaId.value || videoRef.value?.ended) return
  analyticsApi.submitEvent({ type: 'pause', media_id: mediaId.value }).catch(() => {})
}
function trackSeek() {
  if (!mediaId.value) return
  const seekMediaId = mediaId.value
  if (seekTimer) clearTimeout(seekTimer)
  seekTimer = setTimeout(() => {
    const pos = videoRef.value?.currentTime
    analyticsApi.submitEvent({
      type: 'seek',
      media_id: seekMediaId,
      data: { position: pos !== undefined ? Math.round(pos) : 0 },
    }).catch(() => {})
    seekTimer = null
  }, 500)
}
function trackQualityChange(index: number) {
  if (!mediaId.value) return
  const qLabel = index === -1 ? 'auto' : (qualities.value[index]?.name ?? String(index))
  analyticsApi.submitEvent({ type: 'quality_change', media_id: mediaId.value, data: { quality: qLabel } }).catch(() => {})
}
function onVideoError() {
  if (!mediaId.value) return
  analyticsApi.submitEvent({ type: 'error', media_id: mediaId.value }).catch(() => {})
}
function trackComplete() {
  if (!mediaId.value) return
  const dur = videoRef.value?.duration
  analyticsApi.submitEvent({ type: 'complete', media_id: mediaId.value, duration: dur ? Math.round(dur) : undefined }).catch(() => {})
}
watch(mediaId, () => { playEventSent = false })

// Save position on pause and unmount
onUnmounted(() => {
  savePosition()
  playbackStore.stopAutoSave()
  if (controlsTimer) clearTimeout(controlsTimer)
  if (seekTimer) clearTimeout(seekTimer)
  if (volumeSaveTimer) clearTimeout(volumeSaveTimer)
  if (upNextTimer) clearInterval(upNextTimer)
})

watch(mediaId, id => { if (id) loadMedia(id) }, { immediate: true })
</script>

<template>
  <div
    class="mx-auto w-full"
    :class="isTheater ? 'max-w-full' : 'max-w-7xl'"
    :class="
      media && mediaId && !loading && !error
        ? 'max-md:px-0 max-md:py-0 md:px-6 md:py-6'
        : 'px-4 sm:px-6 py-6'
    "
  >
    <!-- No media selected -->
    <div v-if="!mediaId" class="flex flex-col items-center justify-center py-24 gap-4">
      <UIcon name="i-lucide-film" class="size-16 text-muted" />
      <p class="text-muted">No media selected. Browse the <NuxtLink to="/" class="text-primary underline">library</NuxtLink> to find something to watch.</p>
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="flex justify-center py-24">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-10 text-primary" />
    </div>

    <!-- Error -->
    <div v-else-if="error" class="flex flex-col items-center py-16 gap-4">
      <UIcon name="i-lucide-x-circle" class="size-12 text-error" />
      <p class="text-error">{{ error }}</p>
      <UButton to="/" variant="outline" label="Back to Library" />
    </div>

    <!-- Mature gate -->
    <div v-else-if="media && matureGated" class="flex flex-col items-center justify-center py-24 gap-4 text-center px-4">
      <UIcon name="i-lucide-lock" class="size-16 text-muted" />
      <h2 class="text-xl font-semibold">Age-Restricted Content</h2>
      <p class="text-muted max-w-sm">
        <template v-if="!authStore.isLoggedIn">Sign in to access mature content.</template>
        <template v-else>Enable mature content in your profile settings to watch this.</template>
      </p>
      <div class="flex gap-3">
        <UButton v-if="!authStore.isLoggedIn" to="/login" label="Sign In" color="primary" />
        <UButton v-else to="/profile" label="Profile Settings" color="primary" />
        <UButton to="/" variant="outline" color="neutral" label="Back to Library" />
      </div>
    </div>

    <!-- Player -->
    <div v-else-if="media" class="grid grid-cols-1 md:gap-6" :class="isTheater ? '' : 'xl:grid-cols-3'">
      <div class="xl:col-span-2 flex flex-col gap-0 md:gap-4">
        <!-- Video player -->
        <div
          v-if="media.type !== 'audio'"
          class="player-wrapper relative bg-black overflow-hidden group touch-manipulation max-md:rounded-none md:rounded-xl max-md:h-[calc(100dvh-3.5rem-env(safe-area-inset-bottom,0px))] max-md:w-full md:aspect-video"
          @mousemove="resetControlsTimer"
          @touchstart="resetControlsTimer"
          @click="togglePlay"
        >
          <video
            ref="videoRef"
            class="max-md:absolute max-md:inset-0 max-md:h-full max-md:w-full max-md:object-contain md:relative md:inset-auto md:h-auto md:w-full md:aspect-video"
            :src="hlsActivated ? undefined : mediaApi.getStreamUrl(media.id)"
            @loadedmetadata="onVideoLoaded"
            @timeupdate="onTimeUpdate"
            @play="onPlayPause(); trackPlay()"
            @pause="onPlayPause(); trackPause()"
            @ended="savePosition(); trackComplete(); if (loopMode === 'off') startUpNextCountdown()"
            @error="onVideoError"
            @leavepictureinpicture="onPiPChange"
            @enterpictureinpicture="onPiPChange"
          />

          <!-- Up Next overlay (playlist auto-advance) -->
          <Transition name="fade">
            <div
              v-if="showUpNext && nextPlaylistItem"
              class="absolute inset-0 flex flex-col items-center justify-center bg-black/75 z-20 gap-4"
              @click.stop
            >
              <p class="text-white/70 text-sm uppercase tracking-widest">Up Next in {{ upNextCountdown }}s</p>
              <p class="text-white font-semibold text-lg text-center px-8">{{ nextPlaylistItem.title || nextPlaylistItem.media_id }}</p>
              <div class="flex gap-3 mt-2">
                <UButton label="Play Now" color="primary" size="sm" @click="navigateToNextItem" />
                <UButton label="Cancel" variant="outline" color="neutral" size="sm" class="text-white border-white/30" @click="cancelUpNext" />
              </div>
            </div>
          </Transition>

          <!-- HLS loading overlay -->
          <div v-if="hlsLoading" class="absolute inset-0 flex items-center justify-center bg-black/60">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
          </div>

          <!-- Controls + keyboard shortcuts -->
          <PlayerControls
            :is-playing="isPlaying"
            :current-time="currentTime"
            :duration="duration"
            :volume="volume"
            :playback-speed="playbackSpeed"
            :loop-mode="loopMode"
            :is-fullscreen="isFullscreen"
            :is-pi-p="isPiP"
            :pip-supported="pipSupported"
            :qualities="qualities"
            :current-quality="currentQuality"
            :thumbnail-previews="thumbnailPreviews"
            :show-controls="showControls"
            v-model:showShortcuts="showShortcuts"
            @toggle-play="togglePlay"
            @seek="seek"
            @seek-to-fraction="seekToFraction"
            @set-volume="setVolume"
            @cycle-speed="cycleSpeed"
            @quality-select="handleQualitySelect"
            @toggle-fullscreen="toggleFullscreen"
            @toggle-pip="togglePiP"
            @cycle-loop="cycleLoop"
          />
        </div>
        <div v-else class="max-md:px-4">
          <!-- Audio player -->
          <UCard class="text-center py-8 space-y-4">
            <UIcon name="i-lucide-music" class="size-16 text-primary mx-auto" />
            <p class="font-semibold text-lg text-highlighted">{{ getDisplayTitle(media) }}</p>
            <audio
              ref="videoRef"
              :src="mediaApi.getStreamUrl(media.id)"
              controls
              class="w-full mt-2"
              @loadedmetadata="onVideoLoaded"
              @timeupdate="onTimeUpdate"
              @play="onPlayPause(); trackPlay()"
              @pause="onPlayPause(); trackPause()"
              @ended="savePosition(); trackComplete(); if (loopMode === 'off') startUpNextCountdown()"
              @error="onVideoError"
            />
          </UCard>
        </div>

        <div class="flex flex-col gap-4 max-md:px-4 max-md:pt-4">
        <!-- HLS + media meta -->
        <UAlert
          v-if="jobRunning"
          title="Generating HLS stream…"
          :description="`Progress: ${jobProgress}%`"
          color="info"
          variant="soft"
          icon="i-lucide-loader-2"
        />

        <!-- HLS available banner -->
        <UAlert
          v-else-if="hlsAvailable && !hlsActivated"
          title="Adaptive HLS streaming available"
          description="Switch to HLS for adaptive quality and smoother playback."
          color="info"
          variant="soft"
          icon="i-lucide-zap"
        >
          <template #actions>
            <UButton label="Enable HLS" size="xs" :loading="hlsLoading" @click="activateHLS" />
          </template>
        </UAlert>

        <!-- HLS error -->
        <UAlert
          v-else-if="hlsError"
          :title="hlsError"
          color="error"
          variant="soft"
          icon="i-lucide-alert-circle"
        />

        <!-- Request HLS generation (video only, when HLS not available or running) -->
        <UAlert
          v-else-if="media && media.type !== 'audio' && !hlsAvailable && !jobRunning"
          title="Adaptive streaming not available"
          description="Generate HLS for adaptive quality and better playback performance."
          color="neutral"
          variant="soft"
          icon="i-lucide-video"
        >
          <template #actions>
            <UButton label="Generate HLS" size="xs" :loading="requestingHls" variant="outline" color="neutral" @click="requestHlsGeneration" />
          </template>
        </UAlert>

        <!-- Media info -->
        <UCard>
          <template #header>
            <h2 class="font-bold text-lg text-highlighted">{{ getDisplayTitle(media) }}</h2>
          </template>
          <div class="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm">
            <div v-if="media.type"><span class="text-muted">Type:</span> <UBadge :label="media.type" color="neutral" variant="subtle" size="xs" /></div>
            <div v-if="media.duration"><span class="text-muted">Duration:</span> {{ formatDuration(media.duration) }}</div>
            <div v-if="media.size"><span class="text-muted">Size:</span> {{ (media.size / 1048576).toFixed(1) }} MB</div>
            <div v-if="media.views != null"><span class="text-muted">Views:</span> {{ media.views.toLocaleString() }}</div>
            <div v-if="media.width && media.height"><span class="text-muted">Resolution:</span> {{ media.width }}x{{ media.height }}</div>
            <div v-if="media.codec"><span class="text-muted">Codec:</span> {{ media.codec }}</div>
            <div v-if="media.container"><span class="text-muted">Format:</span> {{ media.container.toUpperCase() }}</div>
            <div v-if="media.bitrate"><span class="text-muted">Bitrate:</span> {{ formatBandwidth(media.bitrate) }}</div>
            <div v-if="media.category"><span class="text-muted">Category:</span> {{ media.category }}</div>
            <div v-if="media.date_added"><span class="text-muted">Added:</span> {{ new Date(media.date_added).toLocaleDateString() }}</div>
            <div v-if="hlsActivated && qualities.length > 0">
              <span class="text-muted">Quality:</span> {{ currentQualityLabel }}
            </div>
          </div>
          <div v-if="media.tags && media.tags.length > 0" class="flex flex-wrap gap-1.5 mt-3">
            <UBadge v-for="tag in media.tags" :key="tag" :label="tag" color="primary" variant="subtle" size="xs" />
          </div>
          <div class="flex gap-2 mt-4 flex-wrap">
            <UButton
              icon="i-lucide-download"
              label="Download"
              variant="outline"
              color="neutral"
              size="sm"
              :to="mediaApi.getDownloadUrl(media.id)"
              target="_blank"
            />
            <UButton
              v-if="authStore.isLoggedIn"
              icon="i-lucide-list-plus"
              label="Add to Playlist"
              variant="outline"
              color="neutral"
              size="sm"
              @click="openAddToPlaylist"
            />
          </div>

          <!-- Star rating (logged-in users only) -->
          <div v-if="authStore.isLoggedIn" class="flex items-center gap-1.5 mt-3">
            <span class="text-sm text-muted">Rate:</span>
            <button
              v-for="star in 5"
              :key="star"
              class="text-2xl leading-none transition-colors focus:outline-none"
              :class="star <= userRating ? 'text-yellow-400' : 'text-muted hover:text-yellow-300'"
              :aria-label="`Rate ${star} star${star > 1 ? 's' : ''}`"
              @click="submitRating(star)"
            >★</button>
          </div>

          <!-- Add to playlist modal -->
          <UModal v-model:open="playlistOpen" title="Add to Playlist">
            <template #body>
              <div v-if="playlists.length === 0" class="text-center py-4 text-muted text-sm">
                No playlists yet.
                <NuxtLink to="/playlists" class="text-primary hover:underline ml-1">Create one</NuxtLink>
              </div>
              <div v-else class="space-y-1">
                <UButton
                  v-for="pl in playlists"
                  :key="pl.id"
                  :label="pl.name"
                  variant="ghost"
                  color="neutral"
                  class="w-full justify-start"
                  :loading="addingToPlaylist"
                  @click="addToPlaylist(pl.id)"
                />
              </div>
            </template>
            <template #footer>
              <UButton to="/playlists" icon="i-lucide-plus" label="New Playlist" variant="outline" color="neutral" size="sm" />
              <UButton variant="ghost" color="neutral" label="Cancel" @click="playlistOpen = false" />
            </template>
          </UModal>
        </UCard>
        </div>
      </div>

      <!-- Sidebar: similar + personalized -->
      <div class="space-y-6 max-md:px-4 max-md:pb-6 md:pb-0">
        <!-- Similar media -->
        <div v-if="similar.length > 0" class="space-y-3">
          <h3 class="font-semibold text-highlighted">Similar Media</h3>
          <NuxtLink
            v-for="item in similar"
            :key="item.media_id"
            :to="`/player?id=${encodeURIComponent(item.media_id)}`"
            class="flex gap-3 items-center hover:bg-muted rounded-lg p-2 transition-colors"
          >
            <div class="w-20 h-12 rounded overflow-hidden bg-muted shrink-0">
              <img :src="mediaApi.getThumbnailUrl(item.media_id)" :alt="getDisplayTitle(item)" class="w-full h-full object-cover" loading="lazy" />
            </div>
            <div class="min-w-0">
              <p class="text-sm font-medium truncate">{{ getDisplayTitle(item) }}</p>
              <p v-if="item.category" class="text-xs text-muted">{{ item.category }}</p>
              <p v-if="item.reasons && item.reasons.length > 0" class="text-xs text-primary/70 truncate" :title="item.reasons.join(' · ')">{{ item.reasons[0] }}</p>
            </div>
          </NuxtLink>
        </div>

        <!-- Personalized recommendations (logged-in users) -->
        <div v-if="authStore.isLoggedIn && personalized.length > 0" class="space-y-3">
          <h3 class="font-semibold text-highlighted">Recommended For You</h3>
          <NuxtLink
            v-for="item in personalized"
            :key="item.media_id"
            :to="`/player?id=${encodeURIComponent(item.media_id)}`"
            class="flex gap-3 items-center hover:bg-muted rounded-lg p-2 transition-colors"
          >
            <div class="w-20 h-12 rounded overflow-hidden bg-muted shrink-0">
              <img :src="mediaApi.getThumbnailUrl(item.media_id)" :alt="getDisplayTitle(item)" class="w-full h-full object-cover" loading="lazy" />
            </div>
            <div class="min-w-0">
              <p class="text-sm font-medium truncate">{{ getDisplayTitle(item) }}</p>
              <p v-if="item.category" class="text-xs text-muted">{{ item.category }}</p>
              <p v-if="item.reasons && item.reasons.length > 0" class="text-xs text-primary/70 truncate" :title="item.reasons.join(' · ')">{{ item.reasons[0] }}</p>
            </div>
          </NuxtLink>
        </div>
      </div>
    </div>
  </div>
</template>
