<script setup lang="ts">
import type {MediaCollection} from '~/types/api'
import {useCollectionsApi} from '~/composables/useApiEndpoints'

definePageMeta({layout: 'default', title: 'Collections'})

const collectionsApi = useCollectionsApi()
const mediaApi = useMediaApi()
const toast = useToast()

const collections = ref<MediaCollection[]>([])
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    collections.value = (await collectionsApi.list()) ?? []
  } catch (e: unknown) {
    toast.add({
      title: e instanceof Error ? e.message : 'Failed to load collections',
      color: 'error',
      icon: 'i-lucide-alert-circle',
    })
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <UContainer class="py-6 max-w-5xl">
    <div class="flex items-center gap-2 mb-6">
      <UIcon name="i-lucide-library" class="size-5 text-primary"/>
      <h1 class="text-xl font-semibold">Collections</h1>
      <UBadge v-if="collections.length > 0" :label="String(collections.length)" color="neutral" variant="subtle"
              size="xs"/>
    </div>

    <MediaCardSkeleton v-if="loading" :count="8"/>

    <div v-else-if="collections.length === 0" class="text-center py-16 text-muted">
      <UIcon name="i-lucide-library" class="size-10 mb-3 mx-auto opacity-40"/>
      <p class="text-lg font-medium">No collections yet</p>
      <p class="text-sm mt-1">Curated collections appear here once an administrator creates them.</p>
      <UButton class="mt-4" label="Browse Media" to="/" variant="outline"/>
    </div>

    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-4">
      <NuxtLink
          v-for="col in collections"
          :key="col.id"
          :to="`/collections/${encodeURIComponent(col.id)}`"
          class="group block"
      >
        <div class="aspect-video relative rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift">
          <img
              v-if="col.cover_media_id"
              :src="mediaApi.getThumbnailUrl(col.cover_media_id)"
              :alt="col.name"
              class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
              loading="lazy"
              @error="($event.target as HTMLImageElement).style.display='none'"
          />
          <div v-else class="w-full h-full flex items-center justify-center">
            <UIcon name="i-lucide-library" class="size-8 text-muted"/>
          </div>
        </div>
        <p class="text-sm font-semibold truncate" :title="col.name">{{ col.name }}</p>
        <p v-if="col.description" class="text-xs text-muted truncate" :title="col.description">{{ col.description }}</p>
      </NuxtLink>
    </div>
  </UContainer>
</template>
