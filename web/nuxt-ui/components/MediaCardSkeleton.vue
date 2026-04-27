<script setup lang="ts">
// Standard skeleton card used by every list page during the initial fetch.
// Matches the real MediaCard's aspect ratio and stack so the grid doesn't
// reflow when data arrives — only the colours fade in.
withDefaults(defineProps<{ count?: number }>(), { count: 10 })
</script>

<template>
  <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
    <div
      v-for="i in count"
      :key="i"
      class="space-y-1.5"
      aria-hidden="true"
    >
      <div class="aspect-video rounded-lg bg-elevated/70 animate-pulse" />
      <div class="h-3 w-3/4 rounded bg-elevated/70 animate-pulse" />
      <div class="h-2.5 w-1/2 rounded bg-elevated/50 animate-pulse" />
    </div>
  </div>
</template>
