<script setup lang="ts">
import type { MediaItem, Suggestion } from '~/types/api'
import type { HLSQuality } from '~/composables/useHLS'

definePageMeta({
  title: 'Player',
})

const route = useRoute()
const authStore = useAuthStore()
const mediaApi = useMediaApi()
const playbackApi = usePlaybackApi()
const suggestionsApi = useSuggestionsApi()

// ── Media ID from query ──
const mediaId = computed(() => (route.query.id as string) || '')

// ── Media data ──
const media = ref<MediaItem | null>(null)
const mediaLoading = ref(false)
const mediaError = ref<string | null>(null)

// ── Player state ──
const videoRef = ref<HTMLVideoElement | null>(null)
const audioRef = ref<HTMLAudioElement | null>(null)
const isPlaying = ref(false)
const currentTime = ref(0)
const duration = ref(0)
const volume = ref(1)
const isMuted = ref(false)
const isLooping = ref(false)
const playbackRate = ref(1)
const showControls = ref(true)
const controlsTimer = ref<ReturnType<typeof setTimeout> | null>(null)

// ── Playback position tracking ──
const positionSaveTimer = ref<ReturnType<typeof setInterval> | null>(null)
const lastSavedPosition = ref(0)

// ── HLS ──
const {
  hlsAvailable,
  hlsUrl,
  hlsLoading,
  hlsError,
  qualities,
  currentQuality,
  autoLevel,
  bandwidth,
  selectQuality,
  activateHLS,
  jobProgress,
  jobRunning,
} = useHLS(videoRef, mediaId)

// ── Similar media ──
const similarItems = ref<Suggestion[]>([])
const similarLoading = ref(false)
const similarError = ref(false)

// ── Derived state ──
const isVideo = computed(() => media.value?.type === 'video')
const isAudio = computed(() => media.value?.type === 'audio')
const activeElement = computed<HTMLMediaElement | null>(() =>
  isAudio.value ? audioRef.value : videoRef.value,
)

const progress = computed(() => {
  if (!duration.value) return 0
  return (currentTime.value / duration.value) * 100
})

const qualityBadge = computed(() => {
  if (qualities.value.length === 0) return null
  const idx = currentQuality.value === -1 ? autoLevel.value : currentQuality.value
  const q = qualities.value[idx]
  return q ? q.name : null
})

// ── Format helpers ──
function formatDuration(seconds: number): string {
  if (!seconds || seconds < 0) return '0:00'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

function formatFileSize(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

function formatTitle(name: string): string {
  const base = name.split('/').pop()?.split('\\').pop() || name
  return base.replace(/\.[^/.]+$/, '').replace(/[._-]/g, ' ')
}

function formatBitrate(bps: number): string {
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`
  if (bps >= 1_000) return `${Math.round(bps / 1_000)} kbps`
  return `${bps} bps`
}

// ── Fetch media ──
async function fetchMedia(id: string) {
  if (!id) return
  mediaLoading.value = true
  mediaError.value = null
  media.value = null

  try {
    media.value = await mediaApi.getById(id)
  } catch (err: unknown) {
    const apiErr = err as { message?: string; status?: number }
    if (apiErr.status === 403) {
      mediaError.value = 'Access denied. You may need to enable mature content in your profile.'
    } else {
      mediaError.value = apiErr.message || 'Failed to load media'
    }
  } finally {
    mediaLoading.value = false
  }
}

// ── Fetch similar ──
async function fetchSimilar(id: string) {
  if (!id) return
  similarLoading.value = true
  similarError.value = false
  try {
    similarItems.value = await suggestionsApi.getSimilar(id)
  } catch {
    similarError.value = true
  } finally {
    similarLoading.value = false
  }
}

// ── Media source setup ──
async function setupMediaSource(id: string) {
  const el = activeElement.value
  if (!el || !media.value) return

  // Set direct stream source (HLS composable will override for video if available)
  if (!isVideo.value || !hlsAvailable.value) {
    el.src = mediaApi.getStreamUrl(id)
  }
  el.volume = volume.value
  el.loop = isLooping.value
  el.playbackRate = playbackRate.value

  // Restore playback position
  if (authStore.user?.preferences?.resume_playback !== false) {
    try {
      const data = await playbackApi.getPosition(id)
      const pos = data?.position ?? 0
      if (pos > 5 && el.duration > 0 && pos < el.duration - 5 && el.currentTime < 2) {
        el.currentTime = pos
      } else {
        lastSavedPosition.value = pos
      }
    } catch {
      // No saved position
    }
  }
}

// ── Player event handlers ──
function handleTimeUpdate() {
  const el = activeElement.value
  if (!el) return
  currentTime.value = el.currentTime
}

function handleLoadedMetadata() {
  const el = activeElement.value
  if (!el) return
  duration.value = el.duration

  // Apply saved position if element was not ready before
  if (lastSavedPosition.value > 5 && lastSavedPosition.value < el.duration - 5 && el.currentTime < 2) {
    el.currentTime = lastSavedPosition.value
    lastSavedPosition.value = 0
  }
}

function handleDurationChange() {
  const el = activeElement.value
  if (el) duration.value = el.duration
}

function handlePlay() {
  isPlaying.value = true
  startPositionSaving()
}

function handlePause() {
  isPlaying.value = false
  saveCurrentPosition()
}

function handleEnded() {
  isPlaying.value = false
  saveCurrentPosition()
}

// ── Controls ──
function togglePlay() {
  const el = activeElement.value
  if (!el) return
  if (el.paused) el.play()
  else el.pause()
}

function toggleMute() {
  const el = activeElement.value
  if (!el) return
  isMuted.value = !isMuted.value
  el.muted = isMuted.value
}

function setVolume(val: number) {
  const el = activeElement.value
  volume.value = val
  if (el) el.volume = val
  if (val > 0 && isMuted.value) {
    isMuted.value = false
    if (el) el.muted = false
  }
}

function seekTo(seconds: number) {
  const el = activeElement.value
  if (!el) return
  el.currentTime = Math.max(0, Math.min(seconds, el.duration || 0))
}

function seekBack() {
  seekTo(currentTime.value - 10)
}

function seekForward() {
  seekTo(currentTime.value + 10)
}

function cycleSpeed() {
  const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]
  const idx = speeds.indexOf(playbackRate.value)
  const nextIdx = (idx + 1) % speeds.length
  playbackRate.value = speeds[nextIdx]
  const el = activeElement.value
  if (el) el.playbackRate = playbackRate.value
}

function toggleLoop() {
  isLooping.value = !isLooping.value
  const el = activeElement.value
  if (el) el.loop = isLooping.value
}

function handleFullscreen() {
  const wrapper = document.querySelector('.player-video-wrapper')
  if (!wrapper) return
  if (document.fullscreenElement) {
    document.exitFullscreen()
  } else {
    wrapper.requestFullscreen()
  }
}

function handleProgressClick(e: MouseEvent) {
  const target = e.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  const fraction = (e.clientX - rect.left) / rect.width
  seekTo(fraction * duration.value)
}

// ── Controls auto-hide for video ──
function resetControlsTimer() {
  showControls.value = true
  if (controlsTimer.value) clearTimeout(controlsTimer.value)
  controlsTimer.value = setTimeout(() => {
    if (isPlaying.value && isVideo.value) {
      showControls.value = false
    }
  }, 3000)
}

// ── Position saving ──
function startPositionSaving() {
  if (positionSaveTimer.value) return
  positionSaveTimer.value = setInterval(() => {
    saveCurrentPosition()
  }, 15000) // Save every 15 seconds
}

function stopPositionSaving() {
  if (positionSaveTimer.value) {
    clearInterval(positionSaveTimer.value)
    positionSaveTimer.value = null
  }
}

function saveCurrentPosition() {
  if (!mediaId.value || !duration.value || currentTime.value < 5) return
  playbackApi.savePosition(mediaId.value, currentTime.value, duration.value).catch(() => {
    // Ignore save errors
  })
}

// ── Share link ──
function copyShareLink() {
  if (!media.value) return
  const url = `${window.location.origin}/player?id=${encodeURIComponent(media.value.id)}`
  navigator.clipboard.writeText(url).catch(() => {
    // Fallback: silently fail
  })
}

// ── Watchers ──
watch(mediaId, async (id) => {
  if (!id) {
    media.value = null
    return
  }
  stopPositionSaving()
  await fetchMedia(id)
  fetchSimilar(id)
  // Wait for next tick so the video/audio ref is available
  await nextTick()
  setupMediaSource(id)
}, { immediate: true })

// ── Keyboard shortcuts ──
function handleKeydown(e: KeyboardEvent) {
  // Ignore when typing in inputs
  if (['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement)?.tagName)) return

  switch (e.key.toLowerCase()) {
    case ' ':
    case 'k':
      e.preventDefault()
      togglePlay()
      break
    case 'j':
      e.preventDefault()
      seekBack()
      break
    case 'l':
      e.preventDefault()
      seekForward()
      break
    case 'arrowleft':
      e.preventDefault()
      seekTo(currentTime.value - 5)
      break
    case 'arrowright':
      e.preventDefault()
      seekTo(currentTime.value + 5)
      break
    case 'arrowup':
      e.preventDefault()
      setVolume(Math.min(1, volume.value + 0.05))
      break
    case 'arrowdown':
      e.preventDefault()
      setVolume(Math.max(0, volume.value - 0.05))
      break
    case 'f':
      e.preventDefault()
      if (isVideo.value) handleFullscreen()
      break
    case 'm':
      e.preventDefault()
      toggleMute()
      break
    case 'home':
      e.preventDefault()
      seekTo(0)
      break
    case 'end':
      e.preventDefault()
      seekTo(duration.value)
      break
  }

  // Number keys 0-9 seek to percentage
  if (e.key >= '0' && e.key <= '9') {
    e.preventDefault()
    seekTo(duration.value * parseInt(e.key) / 10)
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  stopPositionSaving()
  saveCurrentPosition()
})
</script>

<template>
  <UContainer class="py-6">
    <!-- No media ID -->
    <div v-if="!mediaId" class="text-center py-16 space-y-4">
      <UIcon name="i-lucide-film" class="text-6xl text-(--ui-text-dimmed)" />
      <h1 class="text-xl font-semibold text-(--ui-text-highlighted)">
        No Media Selected
      </h1>
      <p class="text-(--ui-text-muted)">
        Select a media item from the library to start playback.
      </p>
      <UButton to="/" variant="soft" label="Browse Library" icon="i-lucide-library" />
    </div>

    <!-- Loading -->
    <div v-else-if="mediaLoading" class="flex flex-col items-center justify-center py-16 space-y-4">
      <UIcon name="i-lucide-loader-2" class="animate-spin text-4xl text-(--ui-text-dimmed)" />
      <p class="text-(--ui-text-muted)">Loading media...</p>
    </div>

    <!-- Error -->
    <div v-else-if="mediaError || !media" class="text-center py-16 space-y-4">
      <UIcon name="i-lucide-alert-circle" class="text-6xl text-red-500" />
      <h1 class="text-xl font-semibold text-(--ui-text-highlighted)">
        Failed to Load Media
      </h1>
      <p class="text-(--ui-text-muted)">
        {{ mediaError || 'Media not found.' }}
      </p>
      <div class="flex gap-2 justify-center">
        <UButton to="/" variant="soft" label="Back to Library" icon="i-lucide-arrow-left" />
        <UButton variant="outline" label="Retry" icon="i-lucide-refresh-cw" @click="fetchMedia(mediaId)" />
      </div>
    </div>

    <!-- Player content -->
    <div v-else class="space-y-6">
      <!-- Header -->
      <div class="flex items-center justify-between">
        <UButton to="/" variant="ghost" icon="i-lucide-arrow-left" label="Back to Library" />
      </div>

      <div class="flex flex-col lg:flex-row gap-6">
        <!-- Main column -->
        <div class="flex-1 min-w-0 space-y-4">
          <!-- Video player -->
          <div
            v-if="isVideo"
            class="player-video-wrapper relative aspect-video bg-black rounded-lg overflow-hidden group"
            @mousemove="resetControlsTimer"
            @mouseleave="isPlaying && (showControls = false)"
            @click="togglePlay"
          >
            <video
              ref="videoRef"
              class="w-full h-full object-contain"
              preload="auto"
              playsinline
              @timeupdate="handleTimeUpdate"
              @loadedmetadata="handleLoadedMetadata"
              @durationchange="handleDurationChange"
              @play="handlePlay"
              @pause="handlePause"
              @ended="handleEnded"
            />

            <!-- Loading overlay -->
            <div v-if="hlsLoading" class="absolute inset-0 flex items-center justify-center bg-black/50">
              <UIcon name="i-lucide-loader-2" class="animate-spin text-4xl text-white" />
            </div>

            <!-- Video controls overlay -->
            <div
              :class="['absolute bottom-0 left-0 right-0 px-4 pb-3 pt-8 bg-gradient-to-t from-black/80 to-transparent transition-opacity duration-300', showControls || !isPlaying ? 'opacity-100' : 'opacity-0 pointer-events-none']"
              @click.stop
            >
              <!-- Progress bar -->
              <div
                class="w-full h-1.5 bg-white/30 rounded-full mb-3 cursor-pointer group/progress"
                @click="handleProgressClick"
              >
                <div
                  class="h-full bg-primary rounded-full relative transition-all"
                  :style="{ width: `${progress}%` }"
                >
                  <div class="absolute right-0 top-1/2 -translate-y-1/2 w-3 h-3 bg-white rounded-full opacity-0 group-hover/progress:opacity-100 transition-opacity" />
                </div>
              </div>

              <!-- Control buttons -->
              <div class="flex items-center gap-2 text-white">
                <button class="p-1.5 hover:bg-white/20 rounded transition-colors" title="Back 10s" @click="seekBack">
                  <UIcon name="i-lucide-rewind" class="text-lg" />
                </button>
                <button class="p-1.5 hover:bg-white/20 rounded transition-colors" :title="isPlaying ? 'Pause (K)' : 'Play (K)'" @click="togglePlay">
                  <UIcon :name="isPlaying ? 'i-lucide-pause' : 'i-lucide-play'" class="text-xl" />
                </button>
                <button class="p-1.5 hover:bg-white/20 rounded transition-colors" title="Forward 10s" @click="seekForward">
                  <UIcon name="i-lucide-fast-forward" class="text-lg" />
                </button>

                <!-- Volume -->
                <div class="flex items-center gap-1 ml-1">
                  <button class="p-1.5 hover:bg-white/20 rounded transition-colors" :title="isMuted ? 'Unmute (M)' : 'Mute (M)'" @click="toggleMute">
                    <UIcon :name="isMuted || volume === 0 ? 'i-lucide-volume-x' : volume < 0.5 ? 'i-lucide-volume-1' : 'i-lucide-volume-2'" class="text-lg" />
                  </button>
                  <input
                    type="range"
                    :value="isMuted ? 0 : volume"
                    min="0"
                    max="1"
                    step="0.05"
                    class="w-16 h-1 accent-white"
                    @input="setVolume(Number(($event.target as HTMLInputElement).value))"
                    @click.stop
                  />
                </div>

                <!-- Time -->
                <span class="text-xs font-mono ml-2 whitespace-nowrap">
                  {{ formatDuration(currentTime) }} / {{ formatDuration(duration) }}
                </span>

                <div class="flex-1" />

                <!-- Quality badge -->
                <span v-if="qualityBadge" class="text-xs bg-white/20 px-1.5 py-0.5 rounded">
                  {{ qualityBadge }}
                </span>

                <!-- Speed badge -->
                <span v-if="playbackRate !== 1" class="text-xs bg-white/20 px-1.5 py-0.5 rounded font-mono">
                  {{ playbackRate }}x
                </span>

                <!-- Loop toggle -->
                <button
                  :class="['p-1.5 rounded transition-colors', isLooping ? 'bg-primary/50 text-white' : 'hover:bg-white/20']"
                  title="Loop"
                  @click="toggleLoop"
                >
                  <UIcon name="i-lucide-repeat" class="text-lg" />
                </button>

                <!-- Speed -->
                <button class="p-1.5 hover:bg-white/20 rounded transition-colors text-xs font-mono font-semibold min-w-[38px]" title="Playback speed" @click="cycleSpeed">
                  {{ playbackRate }}x
                </button>

                <!-- Fullscreen -->
                <button class="p-1.5 hover:bg-white/20 rounded transition-colors" title="Fullscreen (F)" @click="handleFullscreen">
                  <UIcon name="i-lucide-maximize" class="text-lg" />
                </button>
              </div>
            </div>
          </div>

          <!-- Audio player -->
          <div v-if="isAudio" class="space-y-4">
            <audio
              ref="audioRef"
              preload="auto"
              @timeupdate="handleTimeUpdate"
              @loadedmetadata="handleLoadedMetadata"
              @durationchange="handleDurationChange"
              @play="handlePlay"
              @pause="handlePause"
              @ended="handleEnded"
            />

            <!-- Audio visualizer card -->
            <UCard>
              <div class="flex flex-col items-center py-8 space-y-4">
                <div class="w-24 h-24 rounded-full bg-(--ui-bg) flex items-center justify-center">
                  <UIcon name="i-lucide-music" class="text-4xl text-primary" />
                </div>
                <h2 class="text-lg font-semibold text-(--ui-text-highlighted) text-center">
                  {{ formatTitle(media.name) }}
                </h2>
              </div>

              <!-- Audio progress -->
              <div class="px-2">
                <div
                  class="w-full h-2 bg-(--ui-bg) rounded-full cursor-pointer mb-2"
                  @click="handleProgressClick"
                >
                  <div
                    class="h-full bg-primary rounded-full transition-all"
                    :style="{ width: `${progress}%` }"
                  />
                </div>

                <!-- Audio controls -->
                <div class="flex items-center gap-2 flex-wrap">
                  <button class="p-2 rounded-full hover:bg-(--ui-bg-elevated) transition-colors" title="Back 10s" @click="seekBack">
                    <UIcon name="i-lucide-rewind" class="text-lg" />
                  </button>
                  <button class="p-3 rounded-full bg-primary text-white hover:bg-primary/90 transition-colors" :title="isPlaying ? 'Pause' : 'Play'" @click="togglePlay">
                    <UIcon :name="isPlaying ? 'i-lucide-pause' : 'i-lucide-play'" class="text-xl" />
                  </button>
                  <button class="p-2 rounded-full hover:bg-(--ui-bg-elevated) transition-colors" title="Forward 10s" @click="seekForward">
                    <UIcon name="i-lucide-fast-forward" class="text-lg" />
                  </button>

                  <span class="text-xs font-mono text-(--ui-text-muted) ml-2">
                    {{ formatDuration(currentTime) }} / {{ formatDuration(duration) }}
                  </span>

                  <div class="flex-1" />

                  <button
                    :class="['p-2 rounded-full transition-colors', isLooping ? 'text-primary bg-primary/10' : 'hover:bg-(--ui-bg-elevated)']"
                    title="Loop"
                    @click="toggleLoop"
                  >
                    <UIcon name="i-lucide-repeat" class="text-lg" />
                  </button>

                  <button
                    :class="['p-2 rounded-full transition-colors text-xs font-mono font-semibold', playbackRate !== 1 ? 'text-primary' : '']"
                    title="Speed"
                    @click="cycleSpeed"
                  >
                    {{ playbackRate }}x
                  </button>

                  <button class="p-2 rounded-full hover:bg-(--ui-bg-elevated) transition-colors" :title="isMuted ? 'Unmute' : 'Mute'" @click="toggleMute">
                    <UIcon :name="isMuted || volume === 0 ? 'i-lucide-volume-x' : volume < 0.5 ? 'i-lucide-volume-1' : 'i-lucide-volume-2'" class="text-lg" />
                  </button>
                  <input
                    type="range"
                    :value="isMuted ? 0 : volume"
                    min="0"
                    max="1"
                    step="0.05"
                    class="w-20 h-1 accent-primary"
                    @input="setVolume(Number(($event.target as HTMLInputElement).value))"
                    @click.stop
                  />
                </div>
              </div>
            </UCard>
          </div>

          <!-- HLS available banner -->
          <div v-if="hlsAvailable && hlsUrl && isVideo" class="flex items-center justify-between gap-3 px-4 py-3 rounded-lg bg-primary/10 border border-primary/20">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-zap" class="text-primary" />
              <span class="text-sm font-medium">HLS adaptive stream ready</span>
            </div>
            <div class="flex gap-2">
              <UButton size="sm" variant="soft" label="Switch to HLS" icon="i-lucide-play-circle" @click="activateHLS" />
              <UButton size="sm" variant="ghost" label="Dismiss" @click="hlsAvailable = false" />
            </div>
          </div>

          <!-- HLS generating banner -->
          <div v-if="jobRunning" class="px-4 py-3 rounded-lg bg-(--ui-bg-elevated) border border-(--ui-border) space-y-2">
            <div class="flex items-center gap-2 text-sm text-(--ui-text-muted)">
              <UIcon name="i-lucide-loader-2" class="animate-spin" />
              <span>Generating HLS adaptive stream...</span>
            </div>
            <div class="w-full h-1.5 bg-(--ui-bg) rounded-full overflow-hidden">
              <div class="h-full bg-primary rounded-full transition-all" :style="{ width: `${jobProgress}%` }" />
            </div>
            <p class="text-xs text-(--ui-text-muted)">{{ jobProgress }}% complete</p>
          </div>

          <!-- HLS error -->
          <div v-if="hlsError" class="px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/20">
            <p class="text-sm text-red-500">{{ hlsError }}</p>
          </div>

          <!-- Media info card -->
          <UCard>
            <div class="space-y-4">
              <h1 class="text-xl font-bold text-(--ui-text-highlighted)">
                {{ formatTitle(media.name) }}
              </h1>

              <!-- Stats row -->
              <div class="flex flex-wrap gap-x-4 gap-y-1 text-sm text-(--ui-text-muted)">
                <span class="flex items-center gap-1">
                  <UIcon name="i-lucide-eye" class="text-xs" />
                  {{ media.views }} views
                </span>
                <span v-if="media.date_added" class="flex items-center gap-1">
                  <UIcon name="i-lucide-calendar" class="text-xs" />
                  {{ new Date(media.date_added).toLocaleDateString() }}
                </span>
                <span class="flex items-center gap-1">
                  <UIcon name="i-lucide-hard-drive" class="text-xs" />
                  {{ formatFileSize(media.size) }}
                </span>
                <span class="flex items-center gap-1">
                  <UIcon name="i-lucide-clock" class="text-xs" />
                  {{ formatDuration(media.duration) }}
                </span>
                <span v-if="media.width && media.height" class="flex items-center gap-1">
                  <UIcon name="i-lucide-monitor" class="text-xs" />
                  {{ media.width }}x{{ media.height }}
                </span>
                <span v-if="media.container" class="flex items-center gap-1">
                  <UIcon name="i-lucide-file-video" class="text-xs" />
                  {{ media.container }}
                </span>
                <span v-if="bandwidth > 0" class="flex items-center gap-1">
                  <UIcon name="i-lucide-wifi" class="text-xs" />
                  {{ formatBitrate(bandwidth) }}
                </span>
              </div>

              <!-- Action buttons -->
              <div class="flex flex-wrap gap-2">
                <UButton
                  v-if="authStore.permissions.can_download"
                  :href="mediaApi.getDownloadUrl(media.id)"
                  variant="outline"
                  icon="i-lucide-download"
                  label="Download"
                  tag="a"
                />
                <UButton variant="outline" icon="i-lucide-share" label="Share" @click="copyShareLink" />
              </div>

              <!-- Category -->
              <div v-if="media.category" class="flex items-center gap-2 text-sm">
                <span class="text-(--ui-text-muted)">Category:</span>
                <UBadge variant="subtle">{{ media.category }}</UBadge>
                <UBadge v-if="media.is_mature" color="error" variant="subtle">18+</UBadge>
              </div>
            </div>
          </UCard>

          <!-- Quality selector card (for HLS) -->
          <UCard v-if="qualities.length > 0">
            <template #header>
              <h2 class="font-semibold text-(--ui-text-highlighted) text-sm">Quality</h2>
            </template>
            <div class="flex flex-wrap gap-2">
              <UButton
                :variant="currentQuality === -1 ? 'solid' : 'outline'"
                size="sm"
                :label="`Auto${autoLevel >= 0 && qualities[autoLevel] ? ' (' + qualities[autoLevel].name + ')' : ''}`"
                @click="selectQuality(-1)"
              />
              <UButton
                v-for="q in qualities"
                :key="q.index"
                :variant="currentQuality === q.index ? 'solid' : 'outline'"
                size="sm"
                :label="q.name"
                @click="selectQuality(q.index)"
              />
            </div>
          </UCard>
        </div>

        <!-- Sidebar -->
        <div class="w-full lg:w-80 shrink-0 space-y-4">
          <!-- Similar media -->
          <UCard>
            <template #header>
              <h3 class="font-semibold text-(--ui-text-highlighted) text-sm flex items-center gap-2">
                <UIcon name="i-lucide-sparkles" />
                Similar Media
              </h3>
            </template>

            <div v-if="similarLoading" class="flex justify-center py-4">
              <UIcon name="i-lucide-loader-2" class="animate-spin text-(--ui-text-dimmed)" />
            </div>

            <div v-else-if="similarError" class="text-center py-4">
              <p class="text-sm text-(--ui-text-muted) mb-2">Failed to load suggestions</p>
              <UButton variant="ghost" size="sm" label="Retry" icon="i-lucide-refresh-cw" @click="fetchSimilar(mediaId)" />
            </div>

            <div v-else-if="similarItems.length === 0" class="text-center py-4">
              <p class="text-sm text-(--ui-text-muted)">No suggestions available</p>
            </div>

            <div v-else class="space-y-2">
              <NuxtLink
                v-for="item in similarItems"
                :key="item.media_id"
                :to="`/player?id=${encodeURIComponent(item.media_id)}`"
                class="flex items-center gap-3 p-2 rounded-lg hover:bg-(--ui-bg-elevated) transition-colors"
              >
                <div class="w-16 h-10 rounded bg-(--ui-bg) flex-shrink-0 flex items-center justify-center overflow-hidden">
                  <img
                    v-if="item.thumbnail_url"
                    :src="item.thumbnail_url"
                    :alt="item.title"
                    class="w-full h-full object-cover"
                  />
                  <UIcon v-else :name="item.media_type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'" class="text-(--ui-text-dimmed)" />
                </div>
                <div class="min-w-0">
                  <p class="text-sm font-medium text-(--ui-text-highlighted) truncate">
                    {{ item.title || formatTitle(item.media_id) }}
                  </p>
                  <p v-if="item.category" class="text-xs text-(--ui-text-muted)">
                    {{ item.category }}
                  </p>
                </div>
              </NuxtLink>
            </div>
          </UCard>

          <!-- Keyboard shortcuts -->
          <UCard>
            <template #header>
              <h3 class="font-semibold text-(--ui-text-highlighted) text-sm flex items-center gap-2">
                <UIcon name="i-lucide-keyboard" />
                Shortcuts
              </h3>
            </template>
            <div class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-xs">
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">Space</kbd>
              <span class="text-(--ui-text-muted)">Play/Pause</span>
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">J / L</kbd>
              <span class="text-(--ui-text-muted)">Back / Forward 10s</span>
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">&larr; &rarr;</kbd>
              <span class="text-(--ui-text-muted)">Back / Forward 5s</span>
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">&uarr; &darr;</kbd>
              <span class="text-(--ui-text-muted)">Volume</span>
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">F</kbd>
              <span class="text-(--ui-text-muted)">Fullscreen</span>
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">M</kbd>
              <span class="text-(--ui-text-muted)">Mute</span>
              <kbd class="px-1.5 py-0.5 bg-(--ui-bg) rounded text-(--ui-text-muted) font-mono text-center">0-9</kbd>
              <span class="text-(--ui-text-muted)">Seek to %</span>
            </div>
          </UCard>
        </div>
      </div>
    </div>
  </UContainer>
</template>
