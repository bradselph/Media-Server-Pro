<script setup lang="ts">
import { formatDuration } from '~/utils/format'

const route = useRoute()
const playbackStore = usePlaybackStore()

// Only show when there's a known media and we're NOT on the player page
const visible = computed(() =>
  !!playbackStore.mediaInfo &&
  !!playbackStore.currentMediaId &&
  route.path !== '/player'
)

const dismissed = ref(false)
watch(() => playbackStore.currentMediaId, () => { dismissed.value = false })

const show = computed(() => visible.value && !dismissed.value)

const progress = computed(() => {
  const dur = playbackStore.mediaInfo?.duration ?? playbackStore.duration
  if (!dur) return 0
  return Math.min(1, playbackStore.position / dur) * 100
})

const resumeUrl = computed(() => {
  const id = playbackStore.currentMediaId
  if (!id) return '/'
  const t = Math.floor(playbackStore.position)
  return t > 5 ? `/player?id=${encodeURIComponent(id)}&t=${t}` : `/player?id=${encodeURIComponent(id)}`
})
</script>

<template>
  <Transition name="slide-up">
    <div
      v-if="show"
      class="fixed bottom-0 left-0 right-0 z-50 bg-elevated border-t border-default shadow-lg"
      role="complementary"
      aria-label="Mini player"
    >
      <!-- Progress bar at top edge -->
      <div class="h-0.5 bg-muted/40">
        <div class="h-full bg-primary transition-all duration-500" :style="{ width: `${progress}%` }" />
      </div>

      <div class="flex items-center gap-3 px-4 py-2">
        <!-- Thumbnail -->
        <NuxtLink :to="resumeUrl" class="shrink-0">
          <div class="w-12 h-8 rounded overflow-hidden bg-muted">
            <img
              v-if="playbackStore.mediaInfo?.thumbnail_url"
              :src="playbackStore.mediaInfo.thumbnail_url"
              :alt="playbackStore.mediaInfo.name"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center">
              <UIcon
                :name="playbackStore.mediaInfo?.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
                class="size-3.5 text-muted"
              />
            </div>
          </div>
        </NuxtLink>

        <!-- Title + position -->
        <div class="flex-1 min-w-0">
          <p class="text-sm font-medium truncate text-highlighted" :title="playbackStore.mediaInfo?.name">
            {{ playbackStore.mediaInfo?.name ?? 'Media' }}
          </p>
          <p class="text-xs text-muted font-mono">
            {{ formatDuration(Math.floor(playbackStore.position)) }}
            <span v-if="playbackStore.mediaInfo?.duration"> / {{ formatDuration(playbackStore.mediaInfo.duration) }}</span>
          </p>
        </div>

        <!-- Resume button -->
        <NuxtLink :to="resumeUrl">
          <UButton
            icon="i-lucide-play"
            label="Resume"
            size="xs"
            color="primary"
            variant="solid"
            aria-label="Resume playback"
          />
        </NuxtLink>

        <!-- Dismiss -->
        <button
          class="p-1 rounded hover:bg-muted transition-colors"
          aria-label="Dismiss mini player"
          @click="dismissed = true"
        >
          <UIcon name="i-lucide-x" class="size-4 text-muted" />
        </button>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.slide-up-enter-active,
.slide-up-leave-active {
  transition: transform 0.25s ease, opacity 0.25s ease;
}
.slide-up-enter-from,
.slide-up-leave-to {
  transform: translateY(100%);
  opacity: 0;
}
</style>
