<script setup lang="ts">
import type { NuxtError } from '#app'

defineProps<{ error: NuxtError }>()

function handleBack() {
  clearError({ redirect: '/' })
}
</script>

<template>
  <div class="min-h-screen bg-default text-default flex items-center justify-center px-4">
    <div class="text-center space-y-4 max-w-md">
      <p class="text-6xl font-bold text-primary">{{ error.statusCode || 500 }}</p>
      <h1 class="text-xl font-semibold text-highlighted">
        {{ error.statusCode === 404 ? 'Page Not Found' : 'Something Went Wrong' }}
      </h1>
      <p class="text-muted text-sm">
        {{ error.statusCode === 404
          ? 'The page you were looking for doesn\'t exist or has been moved.'
          : (error.message || 'An unexpected error occurred.') }}
      </p>
      <UButton icon="i-lucide-house" label="Back to Home" @click="handleBack" />
    </div>
  </div>
</template>
