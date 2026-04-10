<script setup lang="ts">
definePageMeta({ title: 'API Docs', middleware: 'auth' })

const { data: spec, status } = await useFetch('/api/docs', {
  credentials: 'include',
  server: false,
})

function download() {
  const blob = new Blob([String(spec.value ?? '')], { type: 'application/yaml' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'openapi.yaml'
  a.click()
  URL.revokeObjectURL(a.href)
}
</script>

<template>
  <UContainer class="py-8 max-w-5xl">
    <div class="flex items-center justify-between mb-6">
      <div class="flex items-center gap-3">
        <UIcon name="i-lucide-file-code-2" class="size-6 text-primary" />
        <div>
          <h1 class="text-xl font-bold">API Reference</h1>
          <p class="text-sm text-muted">OpenAPI 3.0 specification</p>
        </div>
      </div>
      <UButton
        v-if="spec"
        icon="i-lucide-download"
        label="Download YAML"
        variant="outline"
        size="sm"
        @click="download"
      />
    </div>

    <div v-if="status === 'pending'" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>
    <div v-else-if="status === 'error'" class="text-center py-16 text-error">
      <UIcon name="i-lucide-alert-circle" class="size-10 mb-2" />
      <p>Failed to load API spec. Make sure you are logged in.</p>
    </div>
    <pre
      v-else
      class="bg-muted rounded-xl p-4 text-xs font-mono overflow-auto max-h-[75vh] whitespace-pre-wrap break-all"
    >{{ spec }}</pre>
  </UContainer>
</template>
