<script setup lang="ts">
import type {MediaCollection, MediaItem} from '~/types/api'
import {getDisplayTitle} from '~/utils/mediaTitle'
import {formatDuration} from '~/utils/format'
import {blurHashBgStyle} from '~/utils/blurhash'
import {useCollectionsApi} from '~/composables/useApiEndpoints'

definePageMeta({layout: 'default', title: 'Collection'})

const route = useRoute()
const collectionsApi = useCollectionsApi()
const mediaApi = useMediaApi()
const toast = useToast()

const collectionId = computed(() => String(route.params.id ?? ''))
const collection = ref<MediaCollection | null>(null)
const mediaMap = ref<Record<string, MediaItem>>({})
const loading = ref(true)
const notFound = ref(false)
const failedThumbnails = reactive(new Set<string>())

const orderedItems = computed(() => collection.value?.items ?? [])

async function load() {
  loading.value = true
  notFound.value = false
  try {
    const col = await collectionsApi.get(collectionId.value)
    collection.value = col
    // Resolve full media items by ID so cards show thumbnails/duration, mirroring
    // the favorites page. The collection only stores media_id + name.
    const ids = (col.items ?? []).map(i => i.media_id)
    if (ids.length > 0) {
      const res = await mediaApi.getBatch(ids)
      mediaMap.value = res?.items ?? {}
    } else {
      mediaMap.value = {}
    }
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'Failed to load collection'
    if (msg.toLowerCase().includes('not found')) {
      notFound.value = true
    } else {
      toast.add({title: msg, color: 'error', icon: 'i-lucide-alert-circle'})
    }
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(collectionId, load)
</script>

<template>
  <UContainer class="py-6 max-w-5xl">
    <UButton to="/collections" variant="ghost" color="neutral" size="sm" icon="i-lucide-arrow-left" label="Collections"
             class="mb-4"/>

    <div v-if="notFound" class="text-center py-16 text-muted">
      <UIcon name="i-lucide-folder-x" class="size-10 mb-3 mx-auto opacity-40"/>
      <p class="text-lg font-medium">Collection not found</p>
      <UButton class="mt-4" label="All Collections" to="/collections" variant="outline"/>
    </div>

    <template v-else>
      <div class="flex items-center gap-2 mb-1">
        <UIcon name="i-lucide-library" class="size-5 text-primary"/>
        <h1 class="text-xl font-semibold">{{ collection?.name || 'Collection' }}</h1>
        <UBadge v-if="orderedItems.length > 0" :label="String(orderedItems.length)" color="neutral" variant="subtle"
                size="xs"/>
      </div>
      <p v-if="collection?.description" class="text-sm text-muted mb-6">{{ collection.description }}</p>
      <div v-else class="mb-6"/>

      <MediaCardSkeleton v-if="loading" :count="10"/>

      <div v-else-if="orderedItems.length === 0" class="text-center py-16 text-muted">
        <UIcon name="i-lucide-film" class="size-10 mb-3 mx-auto opacity-40"/>
        <p class="text-lg font-medium">This collection has no items</p>
      </div>

      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
        <NuxtLink
            v-for="item in orderedItems"
            :key="item.media_id"
            :to="`/player?id=${encodeURIComponent(item.media_id)}`"
            class="group block"
        >
          <div
              class="aspect-video relative rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb"
              :style="mediaMap[item.media_id]?.type !== 'audio' ? blurHashBgStyle(mediaMap[item.media_id]?.blur_hash) : {}"
          >
            <HoverPreviewImg
                v-if="mediaMap[item.media_id] && !failedThumbnails.has(item.media_id)"
                :media-id="item.media_id"
                :src="mediaApi.getThumbnailUrl(item.media_id)"
                :alt="getDisplayTitle(mediaMap[item.media_id])"
                img-class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                @error="failedThumbnails.add(item.media_id)"
            />
            <div v-else-if="mediaMap[item.media_id]?.type === 'audio'"
                 class="w-full h-full flex items-center justify-center bg-linear-to-br from-primary/10 to-primary/5">
              <AudioBars size="sm" :bars="5"/>
            </div>
            <div v-else class="w-full h-full flex items-center justify-center">
              <UIcon name="i-lucide-film" class="size-8 text-muted"/>
            </div>
            <div
                v-if="mediaMap[item.media_id]?.duration"
                class="absolute bottom-1 right-1 bg-black/70 text-white text-xs px-1 rounded font-mono"
            >
              {{ formatDuration(mediaMap[item.media_id]!.duration) }}
            </div>
          </div>
          <p class="text-sm font-semibold truncate"
             :title="mediaMap[item.media_id] ? getDisplayTitle(mediaMap[item.media_id]) : (item.media_name || item.media_id)">
            {{ mediaMap[item.media_id] ? getDisplayTitle(mediaMap[item.media_id]) : (item.media_name || item.media_id) }}
          </p>
        </NuxtLink>
      </div>
    </template>
  </UContainer>
</template>
