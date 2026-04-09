<script setup lang="ts">
import type { Suggestion } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'

defineProps<{
  title: string
  icon: string
  items: Suggestion[]
  failedIds?: Set<string>
}>()

const emit = defineEmits<{
  'thumbnail-error': [id: string]
}>()

const mediaApi = useMediaApi()
const scrollContainer = ref<HTMLElement | null>(null)

function scrollBy(delta: number) {
  scrollContainer.value?.scrollBy({ left: delta, behavior: 'smooth' })
}
</script>

<template>
  <div v-if="items.length > 0" class="space-y-2">
    <div class="flex items-center justify-between">
      <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
        <UIcon :name="icon" class="size-4 text-primary" />
        {{ title }}
        <span class="text-xs font-normal opacity-60">({{ items.length }})</span>
      </h2>
      <div class="flex gap-1">
        <UButton icon="i-lucide-chevron-left" size="xs" variant="ghost" color="neutral" aria-label="Scroll left" @click="scrollBy(-320)" />
        <UButton icon="i-lucide-chevron-right" size="xs" variant="ghost" color="neutral" aria-label="Scroll right" @click="scrollBy(320)" />
      </div>
    </div>
    <div ref="scrollContainer" class="flex gap-3 overflow-x-auto pb-2 scroll-smooth">
      <NuxtLink
        v-for="s in items"
        :key="s.media_id"
        :to="`/player?id=${encodeURIComponent(s.media_id)}`"
        class="group shrink-0 w-40"
      >
        <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
          <!-- Audio items: animated bars -->
          <div v-if="s.media_type === 'audio'" class="w-full h-full flex flex-col items-center justify-center bg-linear-to-br from-primary/10 to-primary/5 gap-1">
            <AudioBars size="sm" :bars="5" class="opacity-70 group-hover:opacity-100 transition-opacity" />
            <span class="text-[9px] font-medium text-muted uppercase tracking-wider">Audio</span>
          </div>
          <!-- Video items: thumbnail with fallback -->
          <img
            v-else-if="!failedIds?.has(s.media_id)"
            :src="mediaApi.getThumbnailUrl(s.media_id)"
            :alt="getDisplayTitle(s)"
            width="320"
            height="180"
            class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
            loading="lazy"
            @error="emit('thumbnail-error', s.media_id)"
          />
          <div v-else class="w-full h-full flex items-center justify-center">
            <UIcon name="i-lucide-film" class="size-6 text-muted" />
          </div>
        </div>
        <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(s)">{{ getDisplayTitle(s) }}</p>
        <p v-if="s.category" class="text-[10px] text-muted truncate">{{ s.category }}</p>
      </NuxtLink>
    </div>
  </div>
</template>
