<script setup lang="ts">
/**
 * PasswordStrength — lightweight strength indicator for the signup + change-
 * password flows. Computes a score out of 4 from length + character-class
 * diversity (lowercase, uppercase, digit, symbol) and renders a 4-segment
 * bar plus a short label.
 *
 * This is purely advisory UI; the actual server-side minimum length check
 * still lives in the Go auth handler. The intent is to nudge users toward
 * stronger passwords, not to block them.
 *
 * Hidden when the input is empty so it doesn't draw attention before the
 * user has typed anything.
 */
const props = defineProps<{ value: string }>()

interface Result {
  score: 0 | 1 | 2 | 3 | 4
  label: string
  tone: 'neutral' | 'weak' | 'fair' | 'good' | 'strong'
}

const result = computed<Result>(() => {
  const p = props.value || ''
  if (!p) return { score: 0, label: '', tone: 'neutral' }

  let score = 0
  // Length tiers are deliberately conservative — most users won't pick a
  // 16+ char passphrase, and rewarding length too aggressively turns the
  // meter into a "type more characters" reward loop.
  if (p.length >= 8) score++
  if (p.length >= 12) score++
  // Character-class diversity. Two classes earn one point; four earn two.
  const classes =
    Number(/[a-z]/.test(p)) +
    Number(/[A-Z]/.test(p)) +
    Number(/\d/.test(p)) +
    Number(/[^A-Za-z0-9]/.test(p))
  if (classes >= 2) score++
  if (classes >= 4) score++

  // Clamp 0..4
  const s = Math.min(4, score) as Result['score']
  const labels = ['', 'Weak', 'Fair', 'Good', 'Strong'] as const
  const tones: Result['tone'][] = ['neutral', 'weak', 'fair', 'good', 'strong']
  return { score: s, label: labels[s], tone: tones[s] }
})

const segmentClass = (i: number) => {
  if (i >= result.value.score) return 'bg-[var(--hairline)]'
  switch (result.value.tone) {
    case 'weak': return 'bg-red-500'
    case 'fair': return 'bg-amber-500'
    case 'good': return 'bg-lime-500'
    case 'strong': return 'bg-emerald-500'
    default: return 'bg-[var(--hairline)]'
  }
}
</script>

<template>
  <div v-if="value" class="mt-1.5 space-y-1" aria-live="polite">
    <div class="flex gap-1" role="meter" :aria-valuenow="result.score" aria-valuemin="0" aria-valuemax="4" :aria-label="`Password strength: ${result.label}`">
      <div
        v-for="i in 4"
        :key="i"
        :class="['h-1 flex-1 rounded-sm transition-colors duration-150', segmentClass(i - 1)]"
      />
    </div>
    <p class="text-[10px] text-muted">
      Strength: <span class="font-semibold">{{ result.label || '—' }}</span>
    </p>
  </div>
</template>
