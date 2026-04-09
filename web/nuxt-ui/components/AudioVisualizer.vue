<script setup lang="ts">
/**
 * Real-time audio frequency visualizer using Web Audio API.
 * Connects to an <audio> or <video> element and draws frequency bars
 * via canvas. Falls back to static bars if Web Audio is unavailable.
 */

const props = defineProps<{
  /** The media element to visualize */
  mediaElement?: HTMLMediaElement | null
  /** Number of bars (default 32) */
  bars?: number
  /** Height in px (default 120) */
  height?: number
  /** Bar color (CSS color string, default uses primary) */
  color?: string
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const barCount = computed(() => props.bars ?? 32)
const canvasHeight = computed(() => props.height ?? 120)

let audioCtx: AudioContext | null = null
let analyser: AnalyserNode | null = null
let source: MediaElementAudioSourceNode | null = null
let animationFrame = 0
let connectedElement: HTMLMediaElement | null = null

function getBarColor(): string {
  if (props.color) return props.color
  // Read CSS custom property for primary color
  const el = canvasRef.value
  if (!el) return '#6366f1'
  const style = getComputedStyle(el)
  return style.getPropertyValue('--ui-primary') || '#6366f1'
}

function connectAudio(el: HTMLMediaElement) {
  if (connectedElement === el && audioCtx) return
  cleanup()

  try {
    audioCtx = new AudioContext()
    analyser = audioCtx.createAnalyser()
    analyser.fftSize = 128
    analyser.smoothingTimeConstant = 0.8
    source = audioCtx.createMediaElementSource(el)
    source.connect(analyser)
    analyser.connect(audioCtx.destination)
    connectedElement = el
    draw()
  } catch {
    // Web Audio not available — the canvas stays blank, CSS fallback shows
  }
}

function draw() {
  const canvas = canvasRef.value
  if (!canvas || !analyser) return

  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const bufferLength = analyser.frequencyBinCount
  const dataArray = new Uint8Array(bufferLength)

  function render() {
    if (!analyser || !canvas || !ctx) return
    animationFrame = requestAnimationFrame(render)

    analyser.getByteFrequencyData(dataArray)

    const width = canvas.width
    const height = canvas.height
    ctx.clearRect(0, 0, width, height)

    const numBars = barCount.value
    const gap = 2
    const barWidth = Math.max(2, (width - gap * (numBars - 1)) / numBars)
    const color = getBarColor()

    for (let i = 0; i < numBars; i++) {
      // Map bar index to frequency bin — use lower frequencies more (they're more interesting)
      const binIndex = Math.floor((i / numBars) * bufferLength * 0.8)
      const value = dataArray[binIndex] ?? 0
      const barHeight = (value / 255) * height * 0.95

      const x = i * (barWidth + gap)
      const y = height - barHeight
      const radius = Math.min(barWidth / 2, 3)

      ctx.fillStyle = color
      ctx.globalAlpha = 0.6 + (value / 255) * 0.4
      ctx.beginPath()
      ctx.roundRect(x, y, barWidth, barHeight, [radius, radius, 0, 0])
      ctx.fill()
    }
    ctx.globalAlpha = 1
  }

  render()
}

function cleanup() {
  if (animationFrame) {
    cancelAnimationFrame(animationFrame)
    animationFrame = 0
  }
  // Don't disconnect the source — once a MediaElementSource is created for an
  // element it cannot be re-created. The AudioContext stays alive for the
  // component's lifetime.
}

watch(() => props.mediaElement, (el) => {
  if (el) connectAudio(el)
}, { immediate: true })

onUnmounted(() => {
  cleanup()
  if (audioCtx) {
    audioCtx.close().catch(() => {})
    audioCtx = null
    analyser = null
    source = null
    connectedElement = null
  }
})
</script>

<template>
  <div class="audio-visualizer relative" :style="{ height: `${canvasHeight}px` }">
    <canvas
      ref="canvasRef"
      :width="barCount * 8"
      :height="canvasHeight"
      class="w-full h-full"
    />
  </div>
</template>
