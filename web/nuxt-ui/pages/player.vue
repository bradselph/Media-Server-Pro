<script setup lang="ts">
definePageMeta({
  title: 'Player',
})

const route = useRoute()

// Media ID comes from query parameter: /player?id=xxx
const mediaId = computed(() => route.query.id as string | undefined)
</script>

<template>
  <UContainer class="py-8">
    <div v-if="!mediaId" class="text-center py-16 space-y-4">
      <UIcon name="i-lucide-film" class="text-6xl text-(--ui-text-dimmed)" />
      <h1 class="text-xl font-semibold text-(--ui-text-highlighted)">
        No Media Selected
      </h1>
      <p class="text-(--ui-text-muted)">
        Select a media item from the library to start playback.
      </p>
      <UButton to="/" variant="soft" label="Browse Library" />
    </div>

    <div v-else class="space-y-6">
      <!-- Video player area -->
      <div class="aspect-video bg-black rounded-lg flex items-center justify-center">
        <div class="text-center text-white space-y-2">
          <UIcon name="i-lucide-play-circle" class="text-6xl opacity-50" />
          <p class="text-sm opacity-75">
            Player for media ID: {{ mediaId }}
          </p>
          <p class="text-xs opacity-50">
            HLS.js integration will be implemented here
          </p>
        </div>
      </div>

      <!-- Media info -->
      <UCard>
        <template #header>
          <h1 class="text-xl font-bold text-(--ui-text-highlighted)">
            Media Details
          </h1>
        </template>
        <p class="text-(--ui-text-muted)">
          Media metadata, playback controls, and sidebar will be implemented here.
        </p>
      </UCard>
    </div>
  </UContainer>
</template>
