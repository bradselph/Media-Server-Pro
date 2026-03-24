<script setup lang="ts">
import type { MediaItem, HLSAvailability, Suggestion } from '~/types/api'

definePageMeta({ layout: 'default', title: 'Player' })

const route = useRoute()
const mediaApi = useMediaApi()
const hlsApi = useHlsApi()
const playbackApi = usePlaybackApi()
const suggestionsApi = useSuggestionsApi()
const playbackStore = usePlaybackStore()
const toast = useToast()

const mediaId = computed(() => route.query.id as string | undefined)
const media = ref<MediaItem | null>(null)
const loading = ref(true)
const error = ref('')

// Player refs
const videoRef = ref<HTMLVideoElement | null>(null)
const isPlaying = ref(false)
const volume = ref(1)
const currentTime = ref(0)
const duration = ref(0)
const showControls = ref(true)
const isFullscreen = ref(false)
const playbackSpeed = ref(1)

// HLS
const hlsAvail = ref<HLSAvailability | null>(null)
const hlsEnabled = ref(false)
let hlsInstance: unknown = null

// Similar
const similar = ref<Suggestion[]>([])

let controlsTimer: ReturnType<typeof setTimeout> | null = null

function resetControlsTimer() {
  showControls.value = true
  if (controlsTimer) clearTimeout(controlsTimer)
  controlsTimer = setTimeout(() => { if (isPlaying.value) showControls.value = false }, 3000)
}

async function loadMedia(id: string) {
  loading.value = true
  error.value = ''
  hlsEnabled.value = false
  hlsAvail.value = null
  if (hlsInstance) {
    ;(hlsInstance as { destroy: () => void }).destroy()
    hlsInstance = null
  }
  try {
    media.value = await mediaApi.getById(id)
    playbackStore.setMedia(id)
    // Load similar suggestions
    suggestionsApi.getSimilar(id).then(r => { similar.value = r ?? [] }).catch(() => {})
    // Check HLS
    if (media.value?.type === 'video') {
      hlsApi.check(id).then(r => { hlsAvail.value = r }).catch(() => {})
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load media'
  } finally {
    loading.value = false
  }
}

function getResolvedHlsUrl(): string | null {
  if (!hlsAvail.value) return null
  if (hlsAvail.value.hls_url) return hlsAvail.value.hls_url
  if (hlsAvail.value.job_id) return hlsApi.getMasterPlaylistUrl(hlsAvail.value.job_id)
  if (mediaId.value) return hlsApi.getMasterPlaylistUrl(mediaId.value)
  return null
}

async function restorePosition() {
  if (!mediaId.value || !videoRef.value) return
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
}

function seekTo(e: MouseEvent) {
  if (!videoRef.value) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  videoRef.value.currentTime = ((e.clientX - rect.left) / rect.width) * duration.value
}

function setVolume(v: number) {
  volume.value = v
  if (videoRef.value) videoRef.value.volume = v
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

function cycleSpeed() {
  const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]
  const idx = speeds.indexOf(playbackSpeed.value)
  playbackSpeed.value = speeds[(idx + 1) % speeds.length]
  if (videoRef.value) videoRef.value.playbackRate = playbackSpeed.value
}

function formatTime(s: number): string {
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2,'0')}:${String(sec).padStart(2,'0')}` : `${m}:${String(sec).padStart(2,'0')}`
}

async function enableHLS() {
  if (!videoRef.value || !mediaId.value) return
  const hlsUrl = getResolvedHlsUrl()
  if (!hlsUrl) {
    toast.add({ title: 'HLS playlist URL not available', color: 'error', icon: 'i-lucide-alert-circle' })
    return
  }
  try {
    const { default: Hls } = await import('hls.js')
    if (!Hls.isSupported()) {
      videoRef.value.src = hlsUrl
      return
    }
    hlsInstance = new Hls()
    ;(hlsInstance as InstanceType<typeof Hls>).loadSource(hlsUrl)
    ;(hlsInstance as InstanceType<typeof Hls>).attachMedia(videoRef.value)
    hlsEnabled.value = true
    toast.add({ title: 'HLS streaming enabled', color: 'success', icon: 'i-lucide-check', duration: 2000 })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to start HLS', color: 'error', icon: 'i-lucide-alert-circle' })
  }
}

// Save position on pause and unmount
onUnmounted(() => {
  savePosition()
  playbackStore.stopAutoSave()
  if (hlsInstance) (hlsInstance as { destroy: () => void }).destroy()
  if (controlsTimer) clearTimeout(controlsTimer)
})

watch(mediaId, id => { if (id) loadMedia(id) }, { immediate: true })
</script>

<template>
  <UContainer class="py-6">
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

    <!-- Player -->
    <div v-else-if="media" class="grid grid-cols-1 xl:grid-cols-3 gap-6">
      <div class="xl:col-span-2 space-y-4">
        <!-- Video player -->
        <div
          v-if="media.type !== 'audio'"
          class="player-wrapper relative bg-black rounded-xl overflow-hidden group"
          @mousemove="resetControlsTimer"
          @touchstart="resetControlsTimer"
          @click="togglePlay"
        >
          <video
            ref="videoRef"
            class="w-full aspect-video"
            :src="hlsEnabled ? undefined : mediaApi.getStreamUrl(media.id)"
            @loadedmetadata="onVideoLoaded"
            @timeupdate="onTimeUpdate"
            @play="onPlayPause"
            @pause="onPlayPause"
            @ended="savePosition"
          />

          <!-- Controls overlay -->
          <div
            class="absolute bottom-0 left-0 right-0 p-3 bg-gradient-to-t from-black/80 to-transparent transition-opacity"
            :class="showControls ? 'opacity-100' : 'opacity-0'"
            @click.stop
          >
            <!-- Progress bar -->
            <div class="w-full h-1.5 bg-white/20 rounded-full mb-3 cursor-pointer" @click="seekTo">
              <div
                class="h-full bg-primary rounded-full pointer-events-none"
                :style="{ width: `${duration ? (currentTime / duration) * 100 : 0}%` }"
              />
            </div>

            <div class="flex items-center gap-3">
              <UButton
                :icon="isPlaying ? 'i-lucide-pause' : 'i-lucide-play'"
                variant="ghost"
                color="neutral"
                size="xs"
                class="text-white hover:text-white"
                @click="togglePlay"
              />
              <UButton icon="i-lucide-rewind" variant="ghost" color="neutral" size="xs" class="text-white" @click="seek(-10)" />
              <UButton icon="i-lucide-fast-forward" variant="ghost" color="neutral" size="xs" class="text-white" @click="seek(10)" />

              <span class="text-white text-xs font-mono ml-1">
                {{ formatTime(currentTime) }} / {{ formatTime(duration) }}
              </span>

              <div class="ml-auto flex items-center gap-2">
                <UButton :label="`${playbackSpeed}x`" variant="ghost" color="neutral" size="xs" class="text-white text-xs" @click="cycleSpeed" />

                <input
                  type="range"
                  min="0"
                  max="1"
                  step="0.05"
                  :value="volume"
                  class="w-16 h-1 accent-primary"
                  @input="setVolume(+($event.target as HTMLInputElement).value)"
                  @click.stop
                />

                <UButton
                  :icon="isFullscreen ? 'i-lucide-minimize' : 'i-lucide-maximize'"
                  variant="ghost"
                  color="neutral"
                  size="xs"
                  class="text-white"
                  @click="toggleFullscreen"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Audio player -->
        <UCard v-else class="text-center py-8 space-y-4">
          <UIcon name="i-lucide-music" class="size-16 text-primary mx-auto" />
          <p class="font-semibold text-lg text-highlighted">{{ media.name }}</p>
          <audio
            ref="videoRef"
            :src="mediaApi.getStreamUrl(media.id)"
            controls
            class="w-full mt-2"
            @loadedmetadata="onVideoLoaded"
            @timeupdate="onTimeUpdate"
            @ended="savePosition"
          />
        </UCard>

        <!-- HLS banner -->
        <UAlert
          v-if="hlsAvail?.available && !hlsEnabled"
          title="Adaptive HLS streaming available"
          description="Switch to HLS for adaptive quality and smoother playback."
          color="info"
          variant="soft"
          icon="i-lucide-zap"
        >
          <template #actions>
            <UButton label="Enable HLS" size="xs" @click="enableHLS" />
          </template>
        </UAlert>

        <!-- Media info -->
        <UCard>
          <template #header>
            <h2 class="font-bold text-lg text-highlighted">{{ media.name }}</h2>
          </template>
          <div class="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm">
            <div v-if="media.type"><span class="text-muted">Type:</span> <UBadge :label="media.type" color="neutral" variant="subtle" size="xs" /></div>
            <div v-if="media.duration"><span class="text-muted">Duration:</span> {{ formatTime(media.duration) }}</div>
            <div v-if="media.size"><span class="text-muted">Size:</span> {{ (media.size / 1048576).toFixed(1) }} MB</div>
            <div v-if="media.views != null"><span class="text-muted">Views:</span> {{ media.views.toLocaleString() }}</div>
            <div v-if="media.width && media.height"><span class="text-muted">Resolution:</span> {{ media.width }}x{{ media.height }}</div>
            <div v-if="media.codec"><span class="text-muted">Codec:</span> {{ media.codec }}</div>
            <div v-if="media.category"><span class="text-muted">Category:</span> {{ media.category }}</div>
          </div>
          <div class="flex gap-2 mt-4">
            <UButton
              icon="i-lucide-download"
              label="Download"
              variant="outline"
              color="neutral"
              size="sm"
              :to="mediaApi.getDownloadUrl(media.id)"
              target="_blank"
            />
          </div>
        </UCard>
      </div>

      <!-- Sidebar: similar -->
      <div v-if="similar.length > 0" class="space-y-3">
        <h3 class="font-semibold text-highlighted">Similar Media</h3>
        <NuxtLink
          v-for="item in similar"
          :key="item.media_id"
          :to="`/player?id=${encodeURIComponent(item.media_id)}`"
          class="flex gap-3 items-center hover:bg-muted rounded-lg p-2 transition-colors"
        >
          <div class="w-20 h-12 rounded overflow-hidden bg-muted shrink-0">
            <img :src="mediaApi.getThumbnailUrl(item.media_id)" :alt="item.title" class="w-full h-full object-cover" loading="lazy" />
          </div>
          <div class="min-w-0">
            <p class="text-sm font-medium truncate">{{ item.title }}</p>
            <p v-if="item.category" class="text-xs text-muted">{{ item.category }}</p>
          </div>
        </NuxtLink>
      </div>
    </div>
  </UContainer>
</template>
