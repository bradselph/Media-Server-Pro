<script setup lang="ts">
import type { MetricTimelineEntry } from '~/types/api'

// Reusable SVG line chart for any per-day metric. Renders one or more
// gap-filled MetricTimelineEntry[] series so the dashboard can overlay
// related metrics (views vs streams vs bandwidth, etc.) without pulling in
// a chart library.
//
// Decisions:
//   - SVG, not canvas — DOM-introspectable for tests and crawlers.
//   - viewBox-driven so the chart fills its container at any size.
//   - One axis on the left (largest series), labels rendered as tspans so
//     theme color tokens flow through.
//   - No tooltips library — a simple <title> on each circle gives native
//     hover behavior without keyboard-trap or focus issues.

type Series = {
  label: string
  color: string // tailwind class on the path/circle, e.g. "stroke-primary"
  values: MetricTimelineEntry[]
  format?: (v: number) => string
}

const props = defineProps<{
  series: Series[]
  height?: number
  showAxis?: boolean
}>()

// Emit click events when the user clicks a data point so parents can
// drill the daily breakdown to that date.
const emit = defineEmits<{
  pointClick: [date: string, seriesIndex: number]
}>()

const W = 600
const H = 200

const allValues = computed(() => props.series.flatMap(s => s.values.map(v => v.value)))
const maxValue = computed(() => {
  const m = Math.max(0, ...allValues.value)
  // Round up to a nice number so axis ticks read cleanly.
  if (m === 0) return 1
  const exp = Math.pow(10, Math.floor(Math.log10(m)))
  return Math.ceil(m / exp) * exp
})

// All series should have the same length (gap-filled by backend), but
// guard anyway so a misaligned series doesn't blow up the path.
const xCount = computed(() => Math.max(1, ...props.series.map(s => s.values.length)))

function pointsFor(values: MetricTimelineEntry[]): string {
  const n = Math.max(1, xCount.value - 1)
  const stride = (W - 40) / n
  return values
    .map((entry, i) => {
      const x = 30 + i * stride
      const y = H - 20 - (entry.value / maxValue.value) * (H - 40)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

function circlesFor(values: MetricTimelineEntry[], color: string, fmt?: (v: number) => string) {
  const n = Math.max(1, xCount.value - 1)
  const stride = (W - 40) / n
  return values.map((entry, i) => ({
    cx: 30 + i * stride,
    cy: H - 20 - (entry.value / maxValue.value) * (H - 40),
    title: `${entry.date}: ${fmt ? fmt(entry.value) : entry.value.toLocaleString()}`,
    color,
    date: entry.date,
  }))
}

const yTicks = computed(() => {
  const max = maxValue.value
  return [0, max / 4, max / 2, (max * 3) / 4, max].map(v => ({
    value: v,
    y: H - 20 - (v / max) * (H - 40),
  }))
})

// Show every Nth date so the X axis isn't crowded on long ranges.
const xLabels = computed(() => {
  const first = props.series[0]?.values ?? []
  const stride = (W - 40) / Math.max(1, first.length - 1)
  // Aim for ~6 labels regardless of range.
  const skip = Math.max(1, Math.floor(first.length / 6))
  return first
    .map((entry, i) => ({ entry, i }))
    .filter(({ i }) => i % skip === 0 || i === first.length - 1)
    .map(({ entry, i }) => ({
      x: 30 + i * stride,
      // "MM-DD" — keeps the X axis readable.
      label: entry.date.slice(5),
    }))
})
</script>

<template>
  <div class="w-full" :style="{ height: `${height ?? H}px` }">
    <svg :viewBox="`0 0 ${W} ${H}`" class="w-full h-full" preserveAspectRatio="none">
      <!-- Y grid lines + ticks -->
      <g v-if="showAxis !== false">
        <line v-for="(t, i) in yTicks" :key="i"
              :x1="30" :x2="W" :y1="t.y" :y2="t.y"
              class="stroke-default" stroke-width="0.5" stroke-dasharray="2 3" />
        <text v-for="(t, i) in yTicks" :key="`tx-${i}`"
              :x="0" :y="t.y + 3" class="fill-muted text-[9px]">
          {{ series[0]?.format ? series[0].format!(t.value) : Math.round(t.value).toLocaleString() }}
        </text>
      </g>

      <!-- Series lines -->
      <g v-for="(s, idx) in series" :key="idx">
        <polyline
          :points="pointsFor(s.values)"
          fill="none"
          :class="s.color"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
        <circle
          v-for="(c, ci) in circlesFor(s.values, s.color, s.format)"
          :key="`c-${idx}-${ci}`"
          :cx="c.cx"
          :cy="c.cy"
          r="3"
          :class="[c.color, 'fill-current cursor-pointer hover:r-4']"
          @click="emit('pointClick', c.date, idx)"
        >
          <title>{{ c.title }} (click to drill)</title>
        </circle>
      </g>

      <!-- X axis labels -->
      <g v-if="showAxis !== false">
        <text v-for="(l, i) in xLabels" :key="`x-${i}`"
              :x="l.x" :y="H - 4" text-anchor="middle"
              class="fill-muted text-[9px]">
          {{ l.label }}
        </text>
      </g>
    </svg>

    <!-- Legend -->
    <div v-if="series.length > 1" class="flex flex-wrap gap-3 mt-1 text-xs">
      <div v-for="(s, idx) in series" :key="`leg-${idx}`" class="flex items-center gap-1">
        <span :class="['w-3 h-0.5', s.color.replace('stroke-', 'bg-')]" />
        <span class="text-muted">{{ s.label }}</span>
      </div>
    </div>
  </div>
</template>
