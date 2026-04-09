<script setup lang="ts">
import { formatDuration } from '~/utils/format'

const props = defineProps<{
  isPlaying: boolean
  currentTime: number
  duration: number
  volume: number
  playbackSpeed: number
  loopMode: 'off' | 'one'
  isFullscreen: boolean
  isPiP: boolean
  pipSupported: boolean
  qualities: Array<{ name: string; index: number }>
  currentQuality: number
  thumbnailPreviews: string[]
  showControls: boolean
  showShortcuts: boolean
}>()

const emit = defineEmits<{
  'toggle-play': []
  seek: [delta: number]
  'seek-to-fraction': [fraction: number]
  'set-volume': [v: number]
  'cycle-speed': []
  'quality-select': [index: number]
  'toggle-fullscreen': []
  'toggle-pip': []
  'cycle-loop': []
  'update:showShortcuts': [value: boolean]
}>()

const toast = useToast()

// Seek bar hover state (internal — only needed by this component)
const seekBarHovering = ref(false)
const seekBarHoverX = ref(0)
const seekBarHoverTime = ref(0)

const seekBarPreviewUrl = computed(() => {
  if (!props.thumbnailPreviews.length || !props.duration) return null
  const idx = Math.min(
    Math.floor((seekBarHoverTime.value / props.duration) * props.thumbnailPreviews.length),
    props.thumbnailPreviews.length - 1,
  )
  return props.thumbnailPreviews[idx] ?? null
})

const qualityMenuItems = computed(() => [[
  { label: 'Auto', click: () => emit('quality-select', -1) },
  ...props.qualities.map(q => ({ label: q.name, click: () => emit('quality-select', q.index) })),
]])

const currentQualityLabel = computed(() => {
  if (props.currentQuality === -1) return 'Auto'
  return props.qualities[props.currentQuality]?.name ?? 'Auto'
})

function onSeekBarMouseMove(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const fraction = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
  seekBarHoverTime.value = fraction * props.duration
  seekBarHoverX.value = e.clientX - rect.left
}

function onSeekBarClick(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const fraction = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
  emit('seek-to-fraction', fraction)
}

function onSeekBarTouch(e: TouchEvent) {
  const touch = e.touches[0]
  if (!touch) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const fraction = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width))
  seekBarHoverTime.value = fraction * props.duration
  seekBarHoverX.value = touch.clientX - rect.left
  if (e.type === 'touchend') {
    emit('seek-to-fraction', fraction)
    seekBarHovering.value = false
  } else {
    seekBarHovering.value = true
  }
}

function onSeekBarTouchEnd(e: TouchEvent) {
  const touch = e.changedTouches[0]
  if (!touch) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const fraction = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width))
  emit('seek-to-fraction', fraction)
  seekBarHovering.value = false
}

function copyLinkAtTime() {
  const t = Math.floor(props.currentTime)
  const url = new URL(window.location.href)
  url.searchParams.set('t', String(t))
  navigator.clipboard.writeText(url.toString()).then(() => {
    toast.add({ title: `Link copied at ${formatDuration(t)}`, color: 'success', icon: 'i-lucide-link' })
  }).catch(() => {
    toast.add({ title: 'Failed to copy link', color: 'error', icon: 'i-lucide-x' })
  })
}
</script>

<template>
  <!-- Controls overlay -->
  <div
    class="absolute bottom-0 left-0 right-0 p-3 bg-gradient-to-t from-black/80 to-transparent transition-opacity"
    :class="showControls ? 'opacity-100' : 'opacity-0 pointer-events-none'"
    @click.stop
  >
    <!-- Progress bar -->
    <div
      class="relative w-full h-1.5 bg-white/20 rounded-full mb-3 cursor-pointer"
      @click="onSeekBarClick"
      @mousemove="onSeekBarMouseMove"
      @mouseenter="seekBarHovering = true"
      @mouseleave="seekBarHovering = false"
      @touchstart.prevent="onSeekBarTouch"
      @touchmove.prevent="onSeekBarTouch"
      @touchend.prevent="onSeekBarTouchEnd"
    >
      <Transition name="fade">
        <div
          v-if="seekBarHovering && seekBarPreviewUrl"
          class="absolute bottom-4 -translate-x-1/2 pointer-events-none z-10"
          :style="{ left: `${seekBarHoverX}px` }"
        >
          <img :src="seekBarPreviewUrl" class="w-28 h-16 object-cover rounded border border-white/20 shadow-lg" />
          <p class="text-center text-white text-xs mt-0.5 drop-shadow">{{ formatDuration(seekBarHoverTime) }}</p>
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
        @click="emit('toggle-play')"
      />
      <UButton icon="i-lucide-rewind" aria-label="Rewind 10 seconds" variant="ghost" color="neutral" size="sm" class="text-white" @click="emit('seek', -10)" />
      <UButton icon="i-lucide-fast-forward" aria-label="Forward 10 seconds" variant="ghost" color="neutral" size="sm" class="text-white" @click="emit('seek', 10)" />

      <span class="text-white text-xs font-mono ml-1">
        {{ formatDuration(currentTime) }} / {{ formatDuration(duration) }}
      </span>

      <div class="ml-auto flex items-center gap-2">
        <UButton :label="`${playbackSpeed}x`" :aria-label="`Playback speed: ${playbackSpeed}x`" variant="ghost" color="neutral" size="sm" class="text-white text-xs" @click="emit('cycle-speed')" />

        <!-- Quality selector (HLS only) -->
        <UDropdownMenu v-if="qualities.length > 0" :items="qualityMenuItems">
          <UButton
            :label="currentQualityLabel"
            icon="i-lucide-layers"
            :aria-label="`Video quality: ${currentQualityLabel}`"
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
          @input="emit('set-volume', +($event.target as HTMLInputElement).value)"
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
          @click="emit('toggle-pip')"
        />
        <UButton
          :icon="loopMode === 'one' ? 'i-lucide-repeat-1' : 'i-lucide-repeat'"
          :aria-label="loopMode === 'off' ? 'Loop off' : 'Loop one'"
          variant="ghost"
          color="neutral"
          size="sm"
          :class="loopMode !== 'off' ? 'text-primary' : 'text-white'"
          @click="emit('cycle-loop')"
        />
        <UButton
          icon="i-lucide-keyboard"
          aria-label="Keyboard shortcuts"
          variant="ghost"
          color="neutral"
          size="sm"
          class="text-white"
          @click.stop="emit('update:showShortcuts', !showShortcuts)"
        />
        <UButton
          :icon="isFullscreen ? 'i-lucide-minimize' : 'i-lucide-maximize'"
          :aria-label="isFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'"
          variant="ghost"
          color="neutral"
          size="sm"
          class="text-white"
          @click="emit('toggle-fullscreen')"
        />
      </div>
    </div>
  </div>

  <!-- Keyboard shortcuts overlay -->
  <Transition name="fade">
    <div
      v-if="showShortcuts"
      class="absolute inset-0 flex items-center justify-center bg-black/80 z-30"
      @click.stop="emit('update:showShortcuts', false)"
    >
      <div class="bg-elevated rounded-xl p-6 max-w-xs w-full mx-4 shadow-xl" @click.stop>
        <div class="flex items-center justify-between mb-4">
          <h3 class="font-semibold text-highlighted text-base">Keyboard Shortcuts</h3>
          <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" aria-label="Close" @click="emit('update:showShortcuts', false)" />
        </div>
        <div class="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">Space / K</span><span class="text-muted">Play / Pause</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">J / L</span><span class="text-muted">Skip ±10 seconds</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">← →</span><span class="text-muted">Skip ±5 seconds</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">↑ ↓</span><span class="text-muted">Volume ±5%</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">0–9</span><span class="text-muted">Seek to 0–90%</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">Home / End</span><span class="text-muted">Jump to start / end</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">, / .</span><span class="text-muted">Frame step (paused)</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">&lt; / &gt;</span><span class="text-muted">Decrease / increase speed</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">F</span><span class="text-muted">Toggle fullscreen</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">T</span><span class="text-muted">Toggle theater mode</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">M</span><span class="text-muted">Mute / Unmute</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">?</span><span class="text-muted">Show this overlay</span>
          <span class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center">Esc</span><span class="text-muted">Close overlay</span>
        </div>
      </div>
    </div>
  </Transition>
</template>
