<script setup lang="ts">
import type { ClaudePublicConfig } from '~/composables/useApiEndpoints'

const adminApi = useAdminApi()
const toast = useToast()

const subTab = ref('chat')
const subTabs = [
  { label: 'Chat', value: 'chat', icon: 'i-lucide-message-square' },
  { label: 'Settings', value: 'settings', icon: 'i-lucide-settings' },
  { label: 'Audit Log', value: 'audit', icon: 'i-lucide-scroll-text' },
]

const config = ref<ClaudePublicConfig | null>(null)
const loading = ref(true)
const enabling = ref(false)

async function loadConfig() {
  try {
    config.value = await adminApi.getClaudeConfig()
  } catch {
    // non-fatal — tab still renders
  } finally {
    loading.value = false
  }
}

async function enable() {
  enabling.value = true
  try {
    config.value = await adminApi.updateClaudeConfig({ enabled: true })
    toast.add({ title: 'Claude enabled', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to enable', color: 'error', icon: 'i-lucide-x' })
  } finally {
    enabling.value = false
  }
}

async function disable() {
  enabling.value = true
  try {
    config.value = await adminApi.updateClaudeConfig({ enabled: false })
    toast.add({ title: 'Claude disabled', color: 'neutral', icon: 'i-lucide-power-off' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to disable', color: 'error', icon: 'i-lucide-x' })
  } finally {
    enabling.value = false
  }
}

const hasCredentials = computed(() =>
  config.value ? (config.value.api_key_set || config.value.web_login_token_set) : false
)

onMounted(loadConfig)
</script>

<template>
  <div class="space-y-4">
    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-muted" />
    </div>

    <template v-else>
      <!-- ── Enable/Disable banner ───────────────────────────────────────── -->
      <UCard>
        <div class="flex items-center justify-between gap-4">
          <div class="flex items-center gap-3">
            <UIcon name="i-lucide-brain" class="size-6 shrink-0" :class="config?.enabled ? 'text-primary' : 'text-muted'" />
            <div>
              <p class="font-semibold text-sm">Claude Admin Assistant</p>
              <p class="text-xs text-muted">
                <template v-if="config?.enabled">
                  Enabled · {{ config?.model || 'claude-sonnet-4-6' }}
                  <UBadge v-if="config?.kill_switch" color="warning" size="xs" class="ml-1">Kill-switch active</UBadge>
                </template>
                <template v-else>
                  Disabled — add credentials in Settings then enable.
                </template>
              </p>
            </div>
          </div>

          <div class="flex items-center gap-3 shrink-0">
            <!-- Credential status -->
            <UBadge
              v-if="hasCredentials"
              color="success"
              variant="subtle"
              icon="i-lucide-key"
            >Credentials set</UBadge>
            <UBadge
              v-else
              color="warning"
              variant="subtle"
              icon="i-lucide-key"
            >No credentials</UBadge>

            <!-- Enable / Disable toggle -->
            <UButton
              v-if="!config?.enabled"
              icon="i-lucide-power"
              label="Enable"
              color="primary"
              size="sm"
              :loading="enabling"
              :disabled="!hasCredentials"
              @click="enable"
            />
            <UButton
              v-else
              icon="i-lucide-power-off"
              label="Disable"
              color="neutral"
              variant="outline"
              size="sm"
              :loading="enabling"
              @click="disable"
            />
          </div>
        </div>

        <!-- No-credentials nudge -->
        <div v-if="!hasCredentials && !config?.enabled" class="mt-3 text-xs text-muted border-t border-default pt-3">
          Go to <UButton variant="link" size="xs" label="Settings" class="px-0" @click="subTab = 'settings'" /> to add an API key or web login token, then come back here to enable Claude.
        </div>
      </UCard>

      <!-- ── Sub-tabs ────────────────────────────────────────────────────── -->
      <UTabs v-model="subTab" :items="subTabs" size="sm">
        <template #content="{ item }">
          <div class="pt-3">
            <AdminClaudeChatPanel v-if="item.value === 'chat'" />
            <AdminClaudeSettingsPanel v-else-if="item.value === 'settings'" @config-changed="loadConfig" />
            <AdminClaudeAuditPanel v-else-if="item.value === 'audit'" />
          </div>
        </template>
      </UTabs>
    </template>
  </div>
</template>
