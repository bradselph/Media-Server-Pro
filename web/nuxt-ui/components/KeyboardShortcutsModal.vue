<script setup lang="ts">
const open = ref(false)

function onKeyDown(e: KeyboardEvent) {
  const tag = (e.target as HTMLElement)?.tagName?.toLowerCase()
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return
  if (e.key === '?' && !e.ctrlKey && !e.altKey && !e.metaKey) {
    e.preventDefault()
    open.value = !open.value
  }
}

onMounted(() => document.addEventListener('keydown', onKeyDown))
onUnmounted(() => document.removeEventListener('keydown', onKeyDown))

defineExpose({ open })

const PLAYER_SHORTCUTS = [
  { key: 'Space / K', desc: 'Play / Pause' },
  { key: 'J / L', desc: 'Skip ±10s (configurable)' },
  { key: '← →', desc: 'Skip ±5s' },
  { key: '↑ ↓', desc: 'Volume ±5%' },
  { key: '0–9', desc: 'Seek to 0–90%' },
  { key: 'Home / End', desc: 'Jump to start / end' },
  { key: ', / .', desc: 'Frame step (paused)' },
  { key: '< / >', desc: 'Decrease / increase speed' },
  { key: 'F', desc: 'Toggle fullscreen' },
  { key: 'T', desc: 'Toggle theater mode' },
  { key: 'M', desc: 'Mute / Unmute' },
  { key: 'I', desc: 'Media info overlay' },
]

const GLOBAL_SHORTCUTS = [
  { key: '?', desc: 'Show this shortcuts reference' },
]
</script>

<template>
  <UModal
    v-model:open="open"
    title="Keyboard Shortcuts"
    :ui="{ content: 'max-w-lg' }"
  >
    <template #body>
      <div class="space-y-5">
        <!-- Player shortcuts -->
        <div>
          <p class="text-xs font-semibold text-muted uppercase tracking-wider mb-2">Player</p>
          <div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
            <template v-for="s in PLAYER_SHORTCUTS" :key="s.key">
              <kbd class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center whitespace-nowrap self-center">{{ s.key }}</kbd>
              <span class="text-sm text-muted self-center">{{ s.desc }}</span>
            </template>
          </div>
        </div>

        <!-- Global shortcuts -->
        <div>
          <p class="text-xs font-semibold text-muted uppercase tracking-wider mb-2">Global</p>
          <div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
            <template v-for="s in GLOBAL_SHORTCUTS" :key="s.key">
              <kbd class="font-mono bg-muted rounded px-1.5 py-0.5 text-xs text-center whitespace-nowrap self-center">{{ s.key }}</kbd>
              <span class="text-sm text-muted self-center">{{ s.desc }}</span>
            </template>
          </div>
        </div>
      </div>
    </template>
    <template #footer>
      <p class="text-xs text-muted">Press <kbd class="font-mono bg-muted rounded px-1 py-0.5 text-xs">?</kbd> anywhere to toggle this modal.</p>
      <UButton label="Close" variant="ghost" color="neutral" class="ml-auto" @click="open = false" />
    </template>
  </UModal>
</template>
