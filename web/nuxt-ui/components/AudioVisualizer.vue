<script setup lang="ts">
/**
 * Real-time audio frequency visualizer using Web Audio API.
 *
 * Accepts either a pre-wired AnalyserNode (preferred — avoids double-source
 * conflicts when an equalizer is also connected) or a raw media element
 * (creates its own AudioContext internally).
 *
 * Falls back to a static gradient when no audio source is available.
 */

const props = defineProps<{
  /** Pre-wired AnalyserNode from the parent audio graph (preferred). */
  analyserNode?: AnalyserNode | null
  /** Fallback: media element to create an isolated audio graph from. */
  mediaElement?: HTMLMediaElement | null
  /** Number of bars (default 40) */
  bars?: number
  /** Height in px (default 140) */
  height?: number
  /** Bar color (CSS color string, default uses primary) */
  color?: string
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const barCount = computed(() => props.bars ?? 40)
const canvasHeight = computed(() => props.height ?? 140)

// Internal state when using mediaElement fallback
let ownAudioCtx: AudioContext | null = null
let ownAnalyser: AnalyserNode | null = null
let ownSource: MediaElementAudioSourceNode | null = null
let connectedElement: HTMLMediaElement | null = null

let animationFrame = 0
let resizeObserver: ResizeObserver | null = null

function getBarColor(): string {
  if (props.color) return props.color
  const el = canvasRef.value
  if (!el) return '#6366f1'
  const style = getComputedStyle(document.documentElement)
  return style.getPropertyValue('--color-primary-500') || '#6366f1'
}

function getActiveAnalyser(): AnalyserNode | null {
  return props.analyserNode ?? ownAnalyser
}

function connectMediaElement(el: HTMLMediaElement) {
  if (connectedElement === el && ownAudioCtx) return
  disconnectOwn()
  try {
    ownAudioCtx = new AudioContext()
    ownAnalyser = ownAudioCtx.createAnalyser()
    ownAnalyser.fftSize = 256
    ownAnalyser.smoothingTimeConstant = 0.8
    ownSource = ownAudioCtx.createMediaElementSource(el)
    ownSource.connect(ownAnalyser)
    ownAnalyser.connect(ownAudioCtx.destination)
    connectedElement = el
  } catch {
    // Web Audio not available or element already has a source
    ownAudioCtx = null
    ownAnalyser = null
    ownSource = null
  }
}

function disconnectOwn() {
  if (animationFrame) {
    cancelAnimationFrame(animationFrame)
    animationFrame = 0
  }
  if (ownAudioCtx) {
    ownAudioCtx.close().catch(() => {})
    ownAudioCtx = null
    ownAnalyser = null
    ownSource = null
    connectedElement = null
  }
}

function startDraw() {
  if (animationFrame) return
  const canvas = canvasRef.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const render = () => {
    const analyser = getActiveAnalyser()
    if (!analyser || !canvas || !ctx) {
      animationFrame = 0
      return
    }
    animationFrame = requestAnimationFrame(render)

    const bufferLength = analyser.frequencyBinCount
    const dataArray = new Uint8Array(bufferLength)
    analyser.getByteFrequencyData(dataArray)

    const width = canvas.width
    const height = canvas.height
    ctx.clearRect(0, 0, width, height)

    const numBars = barCount.value
    const gap = 3
    const barWidth = Math.max(3, (width - gap * (numBars - 1)) / numBars)
    const color = getBarColor()

    // Logarithmic frequency mapping: human hearing is logarithmic, and EQ bands
    // (60 Hz, 170 Hz, 310 Hz … 16 kHz) are distributed log-evenly.
    // Map bar i → a frequency range [fLow, fHigh] on a log scale from 20 Hz to 20 kHz,
    // then average the FFT bins that fall within that range.
    const sampleRate = analyser.context.sampleRate
    const nyquist = sampleRate / 2                 // max frequency (e.g. 22050 Hz)
    const fMin = 20                                // Hz — lowest bar
    const fMax = Math.min(20000, nyquist)          // Hz — highest bar
    const logFMin = Math.log10(fMin)
    const logFMax = Math.log10(fMax)

    for (let i = 0; i < numBars; i++) {
      // Frequency range for this bar
      const freqLow  = Math.pow(10, logFMin + (i / numBars) * (logFMax - logFMin))
      const freqHigh = Math.pow(10, logFMin + ((i + 1) / numBars) * (logFMax - logFMin))

      // Convert Hz to FFT bin indices
      const binLow  = Math.floor(freqLow  / nyquist * bufferLength)
      const binHigh = Math.ceil(freqHigh  / nyquist * bufferLength)
      const lo = Math.max(0, binLow)
      const hi = Math.min(bufferLength - 1, binHigh)

      // Average magnitude across the bin range
      let sum = 0
      const count = hi - lo + 1
      for (let b = lo; b <= hi; b++) sum += dataArray[b] ?? 0
      const value = count > 0 ? sum / count : 0

      const barHeight = Math.max(2, (value / 255) * height * 0.97)
      const x = i * (barWidth + gap)
      const y = height - barHeight
      const radius = Math.min(barWidth / 2, 4)

      const alpha = 0.45 + (value / 255) * 0.55
      ctx.fillStyle = color
      ctx.globalAlpha = alpha
      ctx.beginPath()
      ctx.roundRect(x, y, barWidth, barHeight, [radius, radius, 0, 0])
      ctx.fill()
    }
    ctx.globalAlpha = 1
  }

  render()
}

// Watch analyserNode prop (from parent audio graph)
watch(() => props.analyserNode, (node) => {
  if (node) {
    // Stop any own context — parent's analyser takes over
    disconnectOwn()
    startDraw()
  }
}, { immediate: true })

// Watch mediaElement prop (fallback)
watch(() => props.mediaElement, (el) => {
  if (el && !props.analyserNode) {
    connectMediaElement(el)
    startDraw()
  }
}, { immediate: true })

onMounted(() => {
  const canvas = canvasRef.value
  if (!canvas) return
  // Keep canvas intrinsic size in sync with CSS layout so it's never blurry
  // after window resize or theater mode toggle.
  resizeObserver = new ResizeObserver((entries) => {
    const entry = entries[0]
    if (!entry || !canvas) return
    const { width, height } = entry.contentRect
    canvas.width = Math.round(width)
    canvas.height = Math.round(height)
  })
  resizeObserver.observe(canvas.parentElement ?? canvas)
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
  disconnectOwn()
  if (animationFrame) {
    cancelAnimationFrame(animationFrame)
    animationFrame = 0
  }
})
</script>

<template>
  <div class="audio-visualizer relative overflow-hidden" :style="{ height: `${canvasHeight}px` }">
    <canvas
      ref="canvasRef"
      class="w-full h-full"
    />
  </div>
</template>
