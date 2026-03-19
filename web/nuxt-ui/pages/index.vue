<script setup lang="ts">
definePageMeta({
  title: 'Media Library',
})
</script>

<template>
  <UContainer class="py-8">
    <div class="space-y-6">
      <div class="flex items-center justify-between">
        <h1 class="text-2xl font-bold text-(--ui-text-highlighted)">
          Media Library
        </h1>
        <div class="flex items-center gap-2">
          <UInput
            icon="i-lucide-search"
            placeholder="Search media..."
            class="w-64"
          />
        </div>
      </div>

      <!-- Placeholder media grid -->
      <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
        <UCard v-for="i in 10" :key="i" class="overflow-hidden">
          <template #header>
            <div class="aspect-video bg-(--ui-bg-muted) flex items-center justify-center">
              <UIcon name="i-lucide-film" class="text-4xl text-(--ui-text-dimmed)" />
            </div>
          </template>
          <p class="text-sm font-medium text-(--ui-text-highlighted) truncate">
            Media Item {{ i }}
          </p>
          <p class="text-xs text-(--ui-text-muted)">
            Placeholder
          </p>
        </UCard>
      </div>
    </div>
  </UContainer>
</template>
