<script setup lang="ts">
import type { MediaItem, Suggestion, Playlist, PlaylistItem } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration, formatBytes, formatBitrate } from '~/utils/format'

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
    try {
      playlists.value = (await playlistApi.list()) ?? []
    } catch (e: unknown) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to load playlists', color: 'error', icon: 'i-lucide-alert-circle' })
    }
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

// Auto-play preference
const autoPlay = computed(() => userPrefs.value?.auto_play ?? false)

// Keep volume / speed in sync when session or preferences load or update after mount.
watch(
  userPrefs,
  (p) => {
    if (p == null) return
    if (typeof p.volume === 'number') volume.value = p.volume
    if (typeof p.playback_speed === 'number') playbackSpeed.value = p.playback_speed
  },
  { deep: true },
)

// Auto-next: play next suggestion when current media ends (non-playlist context)
const autoNextEnabled = ref(true)

// Share at timestamp
const linkCopied = ref(false)
let linkCopiedTimer: ReturnType<typeof setTimeout> | undefined

function copyTimestampLink() {
  const t = Math.floor(currentTime.value)
  const url = new URL(globalThis.location.href)
  if (t > 0) url.searchParams.set('t', String(t))
  else url.searchParams.delete('t')
  navigator.clipboard.writeText(url.toString())
  linkCopied.value = true
  clearTimeout(linkCopiedTimer)
  linkCopiedTimer = setTimeout(() => { linkCopied.value = false }, 2000)
}

// Graphic Equalizer (Web Audio API)
const eqEnabled = ref(false)
const eqBands = ref([0, 0, 0, 0, 0, 0, 0, 0, 0, 0])
const EQ_FREQUENCIES = [32, 64, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
const EQ_PRESETS: Record<string, number[]> = {
  flat:       [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  bass_boost: [6, 5, 4, 2, 0, 0, 0, 0, 0, 0],
  treble:     [0, 0, 0, 0, 0, 1, 2, 4, 5, 6],
  vocal:      [-2, -1, 0, 2, 4, 4, 3, 1, 0, -1],
  rock:       [4, 3, 1, 0, -1, 0, 1, 3, 4, 4],
  electronic: [4, 3, 1, 0, -2, 0, 1, 2, 4, 5],
  acoustic:   [3, 2, 1, 0, 1, 1, 2, 3, 3, 2],
  jazz:       [2, 1, 0, 1, 2, 2, 1, 1, 2, 3],
  classical:  [3, 2, 1, 0, 0, 0, 0, 1, 2, 3],
  loudness:   [4, 3, 0, 0, -2, 0, -1, -3, 4, 2],
}
let audioCtx: AudioContext | null = null
let eqFilters: BiquadFilterNode[] = []
let sourceNode: MediaElementAudioSourceNode | null = null
let analyserNode: AnalyserNode | null = null
const visualizerAnalyser = ref<AnalyserNode | null>(null)

/**
 * Ensures the shared AudioContext and MediaElementSource are created.
 * Safe to call multiple times — no-ops if already initialised.
 * Chain: source → [EQ filters] → analyser → destination
 */
function ensureAudioGraph() {
  if (!videoRef.value || audioCtx) return
  try {
    audioCtx = new AudioContext()
    sourceNode = audioCtx.createMediaElementSource(videoRef.value)
    analyserNode = audioCtx.createAnalyser()
    // 2048-point FFT → 1024 bins @ ~22kHz = ~21 Hz/bin resolution.
    // Fine enough for the EQ peaking bands (60 Hz–16 kHz) to be
    // individually visible in the frequency display.
    analyserNode.fftSize = 2048
    analyserNode.smoothingTimeConstant = 0.75
    // Default chain (no EQ): source → analyser → destination
    sourceNode.connect(analyserNode)
    analyserNode.connect(audioCtx.destination)
    visualizerAnalyser.value = analyserNode
  } catch {
    // Web Audio unavailable
  }
}

function initEqualizer() {
  if (!videoRef.value) return
  ensureAudioGraph()
  if (!audioCtx || !sourceNode || !analyserNode) return
  if (eqFilters.length > 0) {
    // Already have EQ filters — just ensure enabled
    eqEnabled.value = true
    return
  }
  eqFilters = EQ_FREQUENCIES.map((freq, i) => {
    const filter = audioCtx!.createBiquadFilter()
    filter.type = 'peaking'
    filter.frequency.value = freq
    filter.Q.value = 1.4
    filter.gain.value = eqBands.value[i]
    return filter
  })
  // Rewire: source → f0 → ... → f9 → analyser → destination
  sourceNode.disconnect()
  analyserNode.disconnect()
  sourceNode.connect(eqFilters[0])
  for (let i = 0; i < eqFilters.length - 1; i++) eqFilters[i].connect(eqFilters[i + 1])
  eqFilters[eqFilters.length - 1].connect(analyserNode)
  analyserNode.connect(audioCtx.destination)
  eqEnabled.value = true
}

function setEqBand(index: number, gain: number) {
  eqBands.value[index] = gain
  if (eqFilters[index]) eqFilters[index].gain.value = gain
}

function applyEqPreset(name: string) {
  const preset = EQ_PRESETS[name]
  if (!preset) return
  if (!eqEnabled.value) initEqualizer()
  preset.forEach((gain, i) => setEqBand(i, gain))
  if (authStore.isLoggedIn) updatePreferences({ equalizer_preset: name }).catch(() => {})
}

function toggleEqualizer() {
  if (eqEnabled.value) {
    // Bypass: set all to 0
    eqBands.value.forEach((_v, i) => setEqBand(i, 0))
    eqEnabled.value = false
  } else {
    initEqualizer()
    // Restore saved preset
    const saved = userPrefs.value?.equalizer_preset
    if (saved && EQ_PRESETS[saved]) applyEqPreset(saved)
  }
}

const showEqualizer = ref(false)

// HLS — delegate to composable
const mediaIdRef = computed(() => mediaId.value ?? '')
const {
  hlsAvailable,
  hlsActivated,
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
let preMuteVolume = 1
let _restorePiP = false

function resetControlsTimer() {
  showControls.value = true
  if (controlsTimer) clearTimeout(controlsTimer)
  controlsTimer = setTimeout(() => { if (isPlaying.value) showControls.value = false }, 3000)
}

async function loadMedia(id: string) {
  // Only show loading spinner on initial load — switching media keeps the video element
  // alive in the DOM so PiP continues working across auto-next transitions.
  const isSwitch = !!media.value
  if (!isSwitch) loading.value = true
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
    // Skip restoring if position is 0, within the first 5 seconds (fresh start),
    // or at/near the end (≥95% complete — media was finished, restart from beginning).
    const dur = videoRef.value?.duration
    const nearEnd = dur && dur > 0 && position >= dur * 0.95
    if (position > 5 && !nearEnd && videoRef.value) {
      videoRef.value.currentTime = position
    }
  } catch {}
}

// Called when media reaches the end. Resets the stored position to 0 so that
// returning to this item later starts from the beginning rather than immediately
// re-triggering the ended/auto-next behaviour.
async function onMediaEnded() {
  if (mediaId.value) {
    try {
      await playbackApi.savePosition(mediaId.value, 0, videoRef.value?.duration ?? 0)
    } catch {}
  }
  trackComplete()
  if (loopMode.value === 'off') autoNextFromSuggestions()
}

async function savePosition() {
  if (!mediaId.value || !videoRef.value) return
  const pos = videoRef.value.currentTime
  const dur = videoRef.value.duration || 0
  if (pos > 0) {
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
  // For audio media, pre-wire the audio graph so the visualizer works immediately.
  // Called every time loadedmetadata fires — ensureAudioGraph() is idempotent.
  // Also handles the HLS re-load case: when HLS activates it calls attachMedia() which
  // resets the element and fires loadedmetadata again; the AudioContext and
  // MediaElementAudioSourceNode survive across src/srcObject changes.
  if (media.value?.type === 'audio') {
    ensureAudioGraph()
  }
  // Resume AudioContext on play for ALL media types (audio and video).
  // Without this, video media with EQ enabled has no path to resume the AudioContext
  // after the browser suspends it when the tab is hidden and shown again.
  videoRef.value?.addEventListener('play', () => {
    if (audioCtx?.state === 'suspended') audioCtx.resume()
  }, { once: true })
  restorePosition()
  playbackStore.startAutoSave()
  // Auto-play when preference is enabled
  if (autoPlay.value && videoRef.value && videoRef.value.paused) {
    videoRef.value.play().catch(() => {})
  }
  // Restore PiP if we were in PiP before an auto-next transition
  if (_restorePiP && videoRef.value) {
    _restorePiP = false
    videoRef.value.requestPictureInPicture()
      .then(() => { isPiP.value = true })
      .catch(() => { _restorePiP = false })
  }
}

// Auto-next: navigate to next similar item when video ends (non-playlist context)
function autoNextFromSuggestions() {
  if (!autoNextEnabled.value) return
  // Playlist auto-advance takes priority
  if (nextPlaylistItem.value) { startUpNextCountdown(); return }
  // Pick first similar item that is not the current media
  const next = similar.value.find(s => s.media_id !== mediaId.value)
    ?? personalized.value.find(s => s.media_id !== mediaId.value)
  if (next) {
    showUpNext.value = true
    upNextCountdown.value = 8
    upNextTimer = setInterval(() => {
      upNextCountdown.value -= 1
      if (upNextCountdown.value <= 0) {
        cancelUpNext()
        exitPiPForTransition()
        navigateTo(`/player?id=${encodeURIComponent(next.media_id)}`)
      }
    }, 1000)
  }
}

let lastTimeUpdateAt = 0
function onTimeUpdate() {
  // Throttle to ~4 updates/sec (250ms) instead of the browser's ~30/sec
  const now = performance.now()
  if (now - lastTimeUpdateAt < 250) return
  lastTimeUpdateAt = now
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
  if (document.fullscreenElement) {
    document.exitFullscreen()
    isFullscreen.value = false
  } else {
    el.requestFullscreen()
    isFullscreen.value = true
  }
}

const isPiP = ref(false)
const pipSupported: boolean = !!(import.meta.client && 'pictureInPictureEnabled' in document)

// Playlist context (passed from playlists page via URL query params)
const playlistIdParam = computed(() => route.query.playlist_id as string | undefined)
const playlistIdxParam = computed(() => {
  const v = Number.parseInt(route.query.playlist_idx as string ?? '', 10)
  return Number.isNaN(v) ? -1 : v
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
  exitPiPForTransition()
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
    // Sync isPiP with actual browser state in case the request/exit call threw
    isPiP.value = document.pictureInPictureElement === videoRef.value
  }
}

// Keep isPiP in sync if user closes PiP via browser chrome
function onPiPChange() {
  isPiP.value = document.pictureInPictureElement === videoRef.value
}

// Exit PiP before media transition, flag for restoration after new media loads
async function exitPiPForTransition() {
  if (document.pictureInPictureElement) {
    _restorePiP = true
    try { await document.exitPictureInPicture() } catch {}
    isPiP.value = false
  }
}

// Keyboard shortcuts overlay
const showShortcuts = ref(false)
const showInfoOverlay = ref(false)

function toggleMute() {
  if (!videoRef.value) return
  if (videoRef.value.volume > 0) {
    preMuteVolume = volume.value || 0.5
    setVolume(0)
  } else {
    setVolume(preMuteVolume)
  }
}

const SPEED_OPTIONS = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]

function changeSpeed(delta: number) {
  const curIdx = SPEED_OPTIONS.indexOf(playbackSpeed.value)
  const newIdx = Math.max(0, Math.min(SPEED_OPTIONS.length - 1, (curIdx === -1 ? 3 : curIdx) + delta))
  playbackSpeed.value = SPEED_OPTIONS[newIdx]
  if (videoRef.value) videoRef.value.playbackRate = playbackSpeed.value
  if (authStore.isLoggedIn) updatePreferences({ playback_speed: playbackSpeed.value }).catch(() => {})
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
    case 'i':
    case 'I':
      e.preventDefault()
      showInfoOverlay.value = !showInfoOverlay.value
      break
    case 'Escape':
      if (showShortcuts.value) { e.preventDefault(); showShortcuts.value = false }
      if (showInfoOverlay.value) { e.preventDefault(); showInfoOverlay.value = false }
      break
    default:
      // 0-9: seek to percentage
      if (e.key >= '0' && e.key <= '9' && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault()
        seekToFraction(Number.parseInt(e.key, 10) / 10)
        resetControlsTimer()
      }
      break
  }
}

function onFullscreenChange() {
  isFullscreen.value = !!document.fullscreenElement
}

// Save position when user closes the tab/browser (best-effort via sendBeacon)
function onBeforeUnload() {
  if (!mediaId.value || !videoRef.value) return
  const pos = videoRef.value.currentTime
  const dur = videoRef.value.duration || 0
  if (pos > 0) {
    const body = JSON.stringify({ id: mediaId.value, position: Math.round(pos), duration: Math.round(dur) })
    navigator.sendBeacon('/api/playback', new Blob([body], { type: 'application/json' }))
  }
}

onMounted(() => {
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('keydown', onKeyDown)
  window.addEventListener('beforeunload', onBeforeUnload)
})
onUnmounted(() => {
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('beforeunload', onBeforeUnload)
})

function cycleSpeed() {
  const idx = SPEED_OPTIONS.indexOf(playbackSpeed.value)
  playbackSpeed.value = SPEED_OPTIONS[(idx + 1) % SPEED_OPTIONS.length]
  if (videoRef.value) videoRef.value.playbackRate = playbackSpeed.value
  if (authStore.isLoggedIn) updatePreferences({ playback_speed: playbackSpeed.value }).catch(() => {})
}

// formatDuration imported from ~/utils/format

// formatBandwidth replaced by formatBitrate from ~/utils/format
const formatBandwidth = formatBitrate

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
  if (playEventSent) {
    // Subsequent play after pause = resume
    analyticsApi.submitEvent({ type: 'resume', media_id: mediaId.value }).catch(() => {})
  } else {
    playEventSent = true
    analyticsApi.submitEvent({ type: 'play', media_id: mediaId.value }).catch(() => {})
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
      data: { position: pos === undefined ? 0 : Math.round(pos) },
    }).catch(() => {})
    seekTimer = null
  }, 500)
}
function trackQualityChange(index: number) {
  if (!mediaId.value) return
  const qLabel = index === -1 ? 'auto' : (qualities.value[index]?.name ?? String(index))
  analyticsApi.submitEvent({ type: 'quality_change', media_id: mediaId.value, data: { quality: qLabel } }).catch(() => {})
}
function onVideoError(e?: Event) {
  if (!mediaId.value) return
  // Log the underlying MediaError for debugging; otherwise playback failures are invisible.
  const el = videoRef.value
  if (el?.error) {
    const code = el.error.code
    const msg = el.error.message ?? ''
    console.error(`[player] MediaError code=${code} message=${msg}`, el.error)
    // Show user-visible error with actionable info
    let desc: string
    if (code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED) desc = 'This file format may not be supported by your browser'
    else if (code === MediaError.MEDIA_ERR_NETWORK) desc = 'Network error — check your connection'
    else desc = 'Playback error'
    toast.add({ title: desc, color: 'error', icon: 'i-lucide-alert-circle' })
  }
  analyticsApi.submitEvent({ type: 'error', media_id: mediaId.value }).catch(() => {})
}
function trackComplete() {
  if (!mediaId.value) return
  const dur = videoRef.value?.duration
  analyticsApi.submitEvent({ type: 'complete', media_id: mediaId.value, duration: dur ? Math.round(dur) : undefined }).catch(() => {})
}
watch(mediaId, () => {
  playEventSent = false
  if (seekTimer) { clearTimeout(seekTimer); seekTimer = null }
  if (volumeSaveTimer) { clearTimeout(volumeSaveTimer); volumeSaveTimer = null }
})

// Save position on pause and unmount
onUnmounted(() => {
  savePosition()
  playbackStore.stopAutoSave()
  if (controlsTimer) clearTimeout(controlsTimer)
  if (seekTimer) clearTimeout(seekTimer)
  if (volumeSaveTimer) clearTimeout(volumeSaveTimer)
  if (upNextTimer) clearInterval(upNextTimer)
  clearTimeout(linkCopiedTimer)
})

watch(mediaId, (id, oldId) => {
  if (oldId && oldId !== id) savePosition()
  if (id) loadMedia(id)
}, { immediate: true })
</script>

<template>
  <div
    class="mx-auto w-full"
    :class="[
      isTheater ? 'max-w-full' : 'max-w-7xl',
      media && mediaId && !loading && !error
        ? 'max-md:px-0 max-md:py-0 md:px-6 md:py-6'
        : 'px-4 sm:px-6 py-6',
    ]"
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
            @ended="onMediaEnded()"
            @error="onVideoError"
            @leavepictureinpicture="onPiPChange"
            @enterpictureinpicture="onPiPChange"
          />

          <!-- Up Next overlay (playlist auto-advance or auto-next from suggestions) -->
          <Transition name="fade">
            <div
              v-if="showUpNext"
              class="absolute inset-0 flex flex-col items-center justify-center bg-black/75 z-20 gap-4"
              @click.stop
            >
              <p class="text-white/70 text-sm uppercase tracking-widest">Up Next in {{ upNextCountdown }}s</p>
              <p class="text-white font-semibold text-lg text-center px-8">
                {{ nextPlaylistItem ? (nextPlaylistItem.title || nextPlaylistItem.media_id) : 'Next recommendation' }}
              </p>
              <div class="flex gap-3 mt-2">
                <UButton v-if="nextPlaylistItem" label="Play Now" color="primary" size="sm" @click="navigateToNextItem" />
                <UButton label="Cancel" variant="outline" color="neutral" size="sm" class="text-white border-white/30" @click="cancelUpNext" />
              </div>
            </div>
          </Transition>

          <!-- Mobile skip buttons (visible on touch devices) -->
          <div class="absolute inset-0 flex items-center justify-between pointer-events-none md:hidden z-10">
            <button
              class="pointer-events-auto w-1/4 h-full flex items-center justify-center active:bg-white/10 transition-colors"
              aria-label="Skip back 10 seconds"
              @dblclick.stop="seek(-10)"
              @click.stop
            >
              <UIcon name="i-lucide-rewind" class="size-8 text-white/50 opacity-0 active:opacity-100 transition-opacity" />
            </button>
            <div class="w-1/2" />
            <button
              class="pointer-events-auto w-1/4 h-full flex items-center justify-center active:bg-white/10 transition-colors"
              aria-label="Skip forward 10 seconds"
              @dblclick.stop="seek(10)"
              @click.stop
            >
              <UIcon name="i-lucide-fast-forward" class="size-8 text-white/50 opacity-0 active:opacity-100 transition-opacity" />
            </button>
          </div>

          <!-- HLS loading overlay -->
          <div v-if="hlsLoading" class="absolute inset-0 flex items-center justify-center bg-black/60">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
          </div>

          <!-- Media info overlay (press I) -->
          <Transition name="fade">
            <div
              v-if="showInfoOverlay && media"
              class="absolute top-3 left-3 z-20 bg-black/80 text-white text-xs rounded-lg px-3 py-2.5 space-y-1 backdrop-blur-sm max-w-xs pointer-events-none"
            >
              <div class="font-semibold text-sm truncate">{{ getDisplayTitle(media) }}</div>
              <div v-if="media.codec" class="flex gap-1.5"><span class="text-white/50">Codec</span><span class="uppercase font-mono">{{ media.codec }}</span></div>
              <div v-if="media.width && media.height" class="flex gap-1.5"><span class="text-white/50">Resolution</span><span class="font-mono">{{ media.width }}×{{ media.height }}</span></div>
              <div v-if="media.bitrate" class="flex gap-1.5"><span class="text-white/50">Bitrate</span><span class="font-mono">{{ (media.bitrate / 1000).toFixed(0) }} kbps</span></div>
              <div v-if="hlsActivated && currentQuality >= 0 && qualities[currentQuality]" class="flex gap-1.5"><span class="text-white/50">Quality</span><span>{{ qualities[currentQuality]?.name }}</span></div>
              <div v-if="hlsActivated && bandwidth > 0" class="flex gap-1.5"><span class="text-white/50">Bandwidth</span><span class="font-mono">{{ (bandwidth / 1_000_000).toFixed(1) }} Mbps</span></div>
              <div v-if="media.size" class="flex gap-1.5"><span class="text-white/50">File size</span><span>{{ formatBytes(media.size) }}</span></div>
              <div class="flex gap-1.5 text-white/40 mt-0.5"><span>Press I to close</span></div>
            </div>
          </Transition>

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
          <UCard class="overflow-hidden">
            <div class="flex flex-col items-center py-8 px-4 bg-linear-to-b from-primary/8 to-transparent">
              <AudioVisualizer :analyser-node="visualizerAnalyser" :bars="48" :height="160" class="w-full max-w-md mb-4" />
              <p class="font-bold text-xl text-highlighted text-center max-w-md">{{ getDisplayTitle(media) }}</p>
              <div class="flex items-center gap-2 mt-1.5 text-xs text-muted">
                <span v-if="media.codec" class="uppercase font-medium">{{ media.codec }}</span>
                <span v-if="media.codec && media.bitrate">·</span>
                <span v-if="media.bitrate">{{ formatBitrate(media.bitrate) }}</span>
                <span v-if="(media.codec || media.bitrate) && media.size">·</span>
                <span v-if="media.size">{{ formatBytes(media.size) }}</span>
              </div>
              <div v-if="media.category" class="mt-2">
                <UBadge :label="media.category" color="primary" variant="subtle" size="xs" />
              </div>
            </div>
            <audio
              ref="videoRef"
              :src="hlsActivated ? undefined : mediaApi.getStreamUrl(media.id)"
              controls
              class="w-full"
              @loadedmetadata="onVideoLoaded"
              @timeupdate="onTimeUpdate"
              @play="onPlayPause(); trackPlay()"
              @pause="onPlayPause(); trackPause()"
              @ended="onMediaEnded()"
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
            <div v-if="media.duration || duration"><span class="text-muted">Duration:</span> {{ formatDuration(media.duration || duration) }}</div>
            <div v-if="media.size"><span class="text-muted">Size:</span> {{ formatBytes(media.size) }}</div>
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
              v-if="authStore.isLoggedIn && authStore.user?.permissions?.can_download"
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
            <UButton
              icon="i-lucide-sliders-horizontal"
              :label="eqEnabled ? 'EQ On' : 'Equalizer'"
              :variant="eqEnabled ? 'solid' : 'outline'"
              :color="eqEnabled ? 'primary' : 'neutral'"
              size="sm"
              @click="showEqualizer = !showEqualizer; if (!eqEnabled) toggleEqualizer()"
            />
            <UButton
              :icon="autoNextEnabled ? 'i-lucide-skip-forward' : 'i-lucide-circle-stop'"
              :label="autoNextEnabled ? 'Auto-Next' : 'Auto-Next Off'"
              :variant="autoNextEnabled ? 'solid' : 'outline'"
              :color="autoNextEnabled ? 'primary' : 'neutral'"
              size="sm"
              @click="autoNextEnabled = !autoNextEnabled"
            />
            <UButton
              icon="i-lucide-share-2"
              :label="linkCopied ? 'Copied!' : 'Share'"
              :variant="linkCopied ? 'solid' : 'outline'"
              :color="linkCopied ? 'success' : 'neutral'"
              size="sm"
              @click="copyTimestampLink"
            />
          </div>

          <!-- Graphic Equalizer -->
          <div v-if="showEqualizer" class="mt-4 p-4 rounded-lg bg-muted/50 space-y-3">
            <div class="flex items-center justify-between">
              <h4 class="text-sm font-semibold text-highlighted">Equalizer</h4>
              <div class="flex gap-1.5">
                <UButton
                  v-for="(_, name) in EQ_PRESETS"
                  :key="name"
                  :label="String(name).replace('_', ' ')"
                  size="xs"
                  variant="outline"
                  color="neutral"
                  class="capitalize text-xs"
                  @click="applyEqPreset(String(name))"
                />
              </div>
            </div>
            <div class="flex items-end justify-between gap-1.5 h-32">
              <div v-for="(freq, i) in EQ_FREQUENCIES" :key="freq" class="flex flex-col items-center flex-1 gap-1">
                <input
                  type="range"
                  min="-12"
                  max="12"
                  step="1"
                  :value="eqBands[i]"
                  class="w-full accent-primary appearance-none [writing-mode:vertical-lr] h-24 rotate-180"
                  :aria-label="`${freq >= 1000 ? (freq / 1000) + 'k' : freq} Hz`"
                  @input="setEqBand(i, +($event.target as HTMLInputElement).value)"
                />
                <span class="text-[10px] text-muted">{{ freq >= 1000 ? (freq / 1000) + 'k' : freq }}</span>
              </div>
            </div>
            <div class="flex justify-between items-center">
              <UButton label="Flat" size="xs" variant="ghost" color="neutral" @click="applyEqPreset('flat')" />
              <UButton
                :label="eqEnabled ? 'Bypass EQ' : 'Enable EQ'"
                size="xs"
                :variant="eqEnabled ? 'outline' : 'solid'"
                :color="eqEnabled ? 'neutral' : 'primary'"
                @click="toggleEqualizer"
              />
            </div>
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
            <div class="relative w-20 h-12 rounded overflow-hidden bg-muted shrink-0">
              <img :src="mediaApi.getThumbnailUrl(item.media_id)" :alt="getDisplayTitle(item)" class="w-full h-full object-cover" loading="lazy" />
              <div v-if="item.duration" class="absolute bottom-0 right-0 bg-black/70 text-white text-[9px] font-mono px-0.5 rounded-tl">
                {{ formatDuration(item.duration) }}
              </div>
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
            <div class="relative w-20 h-12 rounded overflow-hidden bg-muted shrink-0">
              <img :src="mediaApi.getThumbnailUrl(item.media_id)" :alt="getDisplayTitle(item)" class="w-full h-full object-cover" loading="lazy" />
              <div v-if="item.duration" class="absolute bottom-0 right-0 bg-black/70 text-white text-[9px] font-mono px-0.5 rounded-tl">
                {{ formatDuration(item.duration) }}
              </div>
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
