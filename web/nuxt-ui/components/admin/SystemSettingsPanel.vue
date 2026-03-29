<script setup lang="ts">
const adminApi = useAdminApi()
const toast = useToast()

const configText = ref('')
const configLoading = ref(false)
const configSaving = ref(false)
const pwCurrent = ref('')
const pwNew = ref('')
const pwConfirm = ref('')
const pwLoading = ref(false)

async function loadConfig() {
  configLoading.value = true
  try {
    const cfg = await adminApi.getConfig()
    configText.value = JSON.stringify(cfg, null, 2)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load config', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { configLoading.value = false }
}

async function saveConfig() {
  configSaving.value = true
  try {
    const parsed = JSON.parse(configText.value)
    await adminApi.updateConfig(parsed)
    toast.add({ title: 'Config saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { configSaving.value = false }
}

async function changeAdminPassword() {
  if (pwNew.value !== pwConfirm.value) {
    toast.add({ title: 'Passwords do not match', color: 'error', icon: 'i-lucide-x' })
    return
  }
  pwLoading.value = true
  try {
    await adminApi.changeOwnPassword(pwCurrent.value, pwNew.value)
    toast.add({ title: 'Password changed', color: 'success', icon: 'i-lucide-check' })
    pwCurrent.value = ''; pwNew.value = ''; pwConfirm.value = ''
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { pwLoading.value = false }
}

onMounted(loadConfig)
</script>

<template>
  <div class="space-y-6">
    <UCard>
      <template #header><div class="font-semibold">Server Configuration (JSON)</div></template>
      <div v-if="configLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else class="space-y-3">
        <UTextarea v-model="configText" :rows="20" class="font-mono text-xs" />
        <UButton :loading="configSaving" icon="i-lucide-save" label="Save Config" @click="saveConfig" />
      </div>
    </UCard>

    <UCard>
      <template #header><div class="font-semibold">Change Admin Password</div></template>
      <div class="space-y-3 max-w-sm">
        <UFormField label="Current Password">
          <UInput v-model="pwCurrent" type="password" placeholder="••••••••" />
        </UFormField>
        <UFormField label="New Password">
          <UInput v-model="pwNew" type="password" placeholder="••••••••" />
        </UFormField>
        <UFormField label="Confirm New Password">
          <UInput v-model="pwConfirm" type="password" placeholder="••••••••" />
        </UFormField>
        <UButton :loading="pwLoading" label="Change Password" @click="changeAdminPassword" />
      </div>
    </UCard>

    <UCard>
      <template #header><div class="font-semibold">Developer Links</div></template>
      <div class="flex flex-wrap gap-2">
        <UButton
          icon="i-lucide-file-code"
          label="OpenAPI Spec (/api/docs)"
          variant="outline"
          color="neutral"
          size="sm"
          to="/api/docs"
          target="_blank"
          external
        />
        <UButton
          icon="i-lucide-bar-chart-2"
          label="Prometheus Metrics (/metrics)"
          variant="outline"
          color="neutral"
          size="sm"
          to="/metrics"
          target="_blank"
          external
        />
      </div>
    </UCard>
  </div>
</template>
