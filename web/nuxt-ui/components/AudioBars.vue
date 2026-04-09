<script setup lang="ts">
/**
 * Animated audio equalizer bars visual.
 * Pure CSS animation — no JS runtime cost.
 * Used as placeholder for audio-only media items across the app.
 */
defineProps<{
  /** Number of bars to render (default 5) */
  bars?: number
  /** Size variant */
  size?: 'xs' | 'sm' | 'md' | 'lg'
  /** Color class for the bars (default: text-primary) */
  color?: string
  /** Whether to animate (default true — set false for static display) */
  animate?: boolean
}>()
</script>

<template>
  <div
    class="audio-bars flex items-end justify-center gap-[2px]"
    :class="[
      size === 'xs' ? 'h-5' : size === 'lg' ? 'h-16' : size === 'md' ? 'h-10' : 'h-8',
      color || 'text-primary',
    ]"
    aria-hidden="true"
  >
    <span
      v-for="i in (bars ?? 5)"
      :key="i"
      class="audio-bar inline-block rounded-full bg-current opacity-80"
      :class="[
        size === 'xs' ? 'w-0.75' : size === 'lg' ? 'w-2' : 'w-1.5',
        animate !== false ? 'animate-audio-bar' : '',
      ]"
      :style="{ animationDelay: `${(i - 1) * 120}ms`, height: animate !== false ? undefined : `${20 + ((i * 37) % 60)}%` }"
    />
  </div>
</template>

<style scoped>
@keyframes audio-bar {
  0%, 100% { height: 15%; }
  25% { height: 80%; }
  50% { height: 35%; }
  75% { height: 65%; }
}

.animate-audio-bar {
  animation: audio-bar 1.2s ease-in-out infinite;
}
</style>
