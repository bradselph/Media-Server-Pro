<script setup lang="ts">
import type { MediaItem, Suggestion, Playlist } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'

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

// Player refs
const videoRef = ref<HTMLVideoElement | null>(null)
const isPlaying = ref(false)
const volume = ref(userPrefs.value?.volume ?? 1)
const currentTime = ref(0)
const duration = ref(0)
const showControls = ref(true)
const isFullscreen = ref(false)
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
const seekBarHoverTime = ref(0)
const seekBarHoverX = ref(0)
const seekBarHovering = ref(false)

function onSeekBarMouseMove(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const fraction = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
  seekBarHoverTime.value = fraction * duration.value
  seekBarHoverX.value = e.clientX - rect.left
}

const seekBarPreviewUrl = computed(() => {
  if (!thumbnailPreviews.value.length || !duration.value) return null
  const idx = Math.min(
    Math.floor((seekBarHoverTime.value / duration.value) * thumbnailPreviews.value.length),
    thumbnailPreviews.value.length - 1,
  )
  return thumbnailPreviews.value[idx] ?? null
})

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
    await playbackApi.savePosition(mediaId.value, pos, dur).catch(() => {})
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

function seekTo(e: MouseEvent) {
  if (!videoRef.value) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  videoRef.value.currentTime = ((e.clientX - rect.left) / rect.width) * duration.value
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
const pipSupported = import.meta.client && 'pictureInPictureEnabled' in document

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

onMounted(() => { document.addEventListener('fullscreenchange', () => { isFullscreen.value = !!document.fullscreenElement }) })
onUnmounted(() => { if (controlsTimer) clearTimeout(controlsTimer) })

function cycleSpeed() {
  const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]
  const idx = speeds.indexOf(playbackSpeed.value)
  playbackSpeed.value = speeds[(idx + 1) % speeds.length]
  if (videoRef.value) videoRef.value.playbackRate = playbackSpeed.value
}

function copyLinkAtTime() {
  const t = Math.floor(currentTime.value)
  if (!mediaId.value) return
  const url = new URL(window.location.href)
  url.searchParams.set('t', String(t))
  navigator.clipboard.writeText(url.toString()).then(() => {
    toast.add({ title: `Link copied at ${formatTime(t)}`, color: 'success', icon: 'i-lucide-link' })
  }).catch(() => {
    toast.add({ title: 'Failed to copy link', color: 'error', icon: 'i-lucide-x' })
  })
}

function formatTime(s: number): string {
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2,'0')}:${String(sec).padStart(2,'0')}` : `${m}:${String(sec).padStart(2,'0')}`
}

function formatBandwidth(bps: number): string {
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`
  if (bps >= 1_000) return `${(bps / 1_000).toFixed(0)} Kbps`
  return `${bps} bps`
}

const qualityMenuItems = computed(() => [[
  { label: 'Auto', click: () => handleQualitySelect(-1) },
  ...qualities.value.map(q => ({ label: q.name, click: () => handleQualitySelect(q.index) })),
]])

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
  if (seekTimer) clearTimeout(seekTimer)
  seekTimer = setTimeout(() => {
    const pos = videoRef.value?.currentTime
    analyticsApi.submitEvent({
      type: 'seek',
      media_id: mediaId.value!,
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
})

watch(mediaId, id => { if (id) loadMedia(id) }, { immediate: true })
</script>

<template>
  <div
    class="mx-auto w-full max-w-7xl"
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
    <div v-else-if="media" class="grid grid-cols-1 xl:grid-cols-3 md:gap-6">
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
            @ended="savePosition(); trackComplete()"
            @error="onVideoError"
            @leavepictureinpicture="onPiPChange"
            @enterpictureinpicture="onPiPChange"
          />

          <!-- HLS loading overlay -->
          <div v-if="hlsLoading" class="absolute inset-0 flex items-center justify-center bg-black/60">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
          </div>

          <!-- Controls overlay -->
          <div
            class="absolute bottom-0 left-0 right-0 p-3 bg-gradient-to-t from-black/80 to-transparent transition-opacity"
            :class="showControls ? 'opacity-100' : 'opacity-0'"
            @click.stop
          >
            <!-- Progress bar -->
            <div
              class="relative w-full h-1.5 bg-white/20 rounded-full mb-3 cursor-pointer"
              @click="seekTo"
              @mousemove="onSeekBarMouseMove"
              @mouseenter="seekBarHovering = true"
              @mouseleave="seekBarHovering = false"
            >
              <Transition name="fade">
                <div
                  v-if="seekBarHovering && seekBarPreviewUrl"
                  class="absolute bottom-4 -translate-x-1/2 pointer-events-none z-10"
                  :style="{ left: `${seekBarHoverX}px` }"
                >
                  <img :src="seekBarPreviewUrl" class="w-28 h-16 object-cover rounded border border-white/20 shadow-lg" />
                  <p class="text-center text-white text-xs mt-0.5 drop-shadow">{{ formatTime(seekBarHoverTime) }}</p>
                </div>
              </Transition>
              <div
                class="h-full bg-primary rounded-full pointer-events-none"
                :style="{ width: `${duration ? (currentTime / duration) * 100 : 0}%` }"
              />
            </div>

            <div class="flex items-center gap-3">
              <UButton
                :icon="isPlaying ? 'i-lucide-pause' : 'i-lucide-play'"
                :aria-label="isPlaying ? 'Pause' : 'Play'"
                variant="ghost"
                color="neutral"
                size="sm"
                class="text-white hover:text-white"
                @click="togglePlay"
              />
              <UButton icon="i-lucide-rewind" aria-label="Rewind 10 seconds" variant="ghost" color="neutral" size="sm" class="text-white" @click="seek(-10)" />
              <UButton icon="i-lucide-fast-forward" aria-label="Forward 10 seconds" variant="ghost" color="neutral" size="sm" class="text-white" @click="seek(10)" />

              <span class="text-white text-xs font-mono ml-1">
                {{ formatTime(currentTime) }} / {{ formatTime(duration) }}
              </span>

              <div class="ml-auto flex items-center gap-2">
                <UButton :label="`${playbackSpeed}x`" aria-label="Playback speed" variant="ghost" color="neutral" size="sm" class="text-white text-xs" @click="cycleSpeed" />

                <!-- Quality selector (HLS only) -->
                <UDropdownMenu v-if="qualities.length > 0" :items="qualityMenuItems">
                  <UButton
                    :label="currentQualityLabel"
                    icon="i-lucide-layers"
                    aria-label="Video quality"
                    variant="ghost"
                    color="neutral"
                    size="sm"
                    class="text-white text-xs"
                    @click.stop
                  />
                </UDropdownMenu>

                <input
                  type="range"
                  min="0"
                  max="1"
                  step="0.05"
                  :value="volume"
                  aria-label="Volume"
                  class="w-16 h-1 accent-primary"
                  @input="setVolume(+($event.target as HTMLInputElement).value)"
                  @click.stop
                />

                <UButton
                  icon="i-lucide-link"
                  aria-label="Copy link at current time"
                  variant="ghost"
                  color="neutral"
                  size="sm"
                  class="text-white"
                  @click="copyLinkAtTime"
                />
                <UButton
                  v-if="pipSupported"
                  :icon="isPiP ? 'i-lucide-picture-in-picture-2' : 'i-lucide-picture-in-picture'"
                  :aria-label="isPiP ? 'Exit picture-in-picture' : 'Picture-in-picture'"
                  variant="ghost"
                  color="neutral"
                  size="sm"
                  class="text-white"
                  @click="togglePiP"
                />
                <UButton
                  :icon="isFullscreen ? 'i-lucide-minimize' : 'i-lucide-maximize'"
                  :aria-label="isFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'"
                  variant="ghost"
                  color="neutral"
                  size="sm"
                  class="text-white"
                  @click="toggleFullscreen"
                />
              </div>
            </div>
          </div>
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
              @ended="savePosition(); trackComplete()"
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
            <div v-if="media.duration"><span class="text-muted">Duration:</span> {{ formatTime(media.duration) }}</div>
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
