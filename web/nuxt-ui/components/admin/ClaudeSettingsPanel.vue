<script setup lang="ts">
import type { ClaudePublicConfig, ClaudeConfigUpdate, ClaudeAuthStatus } from '~/composables/useApiEndpoints'

const emit = defineEmits<{ (e: 'config-changed'): void }>()

const adminApi = useAdminApi()
const toast = useToast()
const { notifyError, notifySuccess } = useAdminFeedback()

const config = ref<ClaudePublicConfig | null>(null)
const authStatus = ref<ClaudeAuthStatus | null>(null)
const loading = ref(false)
const authLoading = ref(false)
const saving = ref(false)
const killSwitchBusy = ref(false)

const draft = ref<ClaudeConfigUpdate>({})

const MODELS = [
  { label: 'Claude Sonnet 4.6 (recommended)', value: 'claude-sonnet-4-6' },
  { label: 'Claude Opus 4.7', value: 'claude-opus-4-7' },
  { label: 'Claude Haiku 4.5', value: 'claude-haiku-4-5-20251001' },
]

const MODES = [
  { label: 'Advisory — Claude Code plan mode; no writes', value: 'advisory' },
  { label: 'Interactive — all writes bypass permission prompt', value: 'interactive' },
  { label: 'Autonomous — full tool access end-to-end', value: 'autonomous' },
]

async function load() {
  loading.value = true
  try {
    config.value = await adminApi.getClaudeConfig()
    if (config.value) {
      draft.value = {
        enabled: config.value.enabled,
        binary_path: config.value.binary_path || '',
        workdir: config.value.workdir || '',
        model: config.value.model || 'claude-sonnet-4-6',
        mode: config.value.mode || 'autonomous',
        max_tokens: config.value.max_tokens || 4096,
        system_prompt: config.value.system_prompt || '',
        require_confirm_for_writes: config.value.require_confirm_for_writes,
        max_tool_calls_per_turn: config.value.max_tool_calls_per_turn || 32,
        rate_limit_per_minute: config.value.rate_limit_per_minute || 30,
        kill_switch: config.value.kill_switch,
        history_retention_days: config.value.history_retention_days || 30,
      }
    }
  } catch (e: unknown) {
    notifyError(e, 'Failed to load Claude config')
  } finally {
    loading.value = false
  }
  void refreshAuth()
}

async function refreshAuth() {
  authLoading.value = true
  try {
    authStatus.value = await adminApi.getClaudeAuthStatus()
  } catch (e: unknown) {
    authStatus.value = {
      installed: false,
      authenticated: false,
      message: e instanceof Error ? e.message : 'Failed to probe CLI',
    }
  } finally {
    authLoading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    config.value = await adminApi.updateClaudeConfig(draft.value)
    notifySuccess('Claude settings saved')
    emit('config-changed')
    void refreshAuth()
  } catch (e: unknown) {
    notifyError(e, 'Failed to save')
  } finally {
    saving.value = false
  }
}

async function toggleKillSwitch() {
  if (!config.value) return
  killSwitchBusy.value = true
  try {
    const on = !config.value.kill_switch
    await adminApi.setClaudeKillSwitch(on)
    config.value.kill_switch = on
    draft.value.kill_switch = on
    toast.add({
      title: on ? 'Kill-switch activated — Claude disabled' : 'Kill-switch cleared',
      color: on ? 'warning' : 'success',
      icon: on ? 'i-lucide-shield-off' : 'i-lucide-shield-check',
    })
  } catch (e: unknown) {
    notifyError(e, 'Failed')
  } finally {
    killSwitchBusy.value = false
  }
}

const authBadge = computed(() => {
  if (!authStatus.value) return { color: 'neutral' as const, icon: 'i-lucide-loader-2', label: 'Checking…' }
  if (!authStatus.value.installed) return { color: 'error' as const, icon: 'i-lucide-x-circle', label: 'CLI Not Installed' }
  if (!authStatus.value.authenticated) return { color: 'warning' as const, icon: 'i-lucide-key-round', label: 'Not Authenticated' }
  return { color: 'success' as const, icon: 'i-lucide-check-circle', label: 'Ready' }
})

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div v-if="loading && !config" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <template v-else-if="config">
      <UAlert
        v-if="config.kill_switch"
        icon="i-lucide-shield-off"
        color="warning"
        title="Kill-switch is active"
        description="All Claude activity is blocked. Clear the kill-switch to resume."
      />

      <!-- Module + CLI auth status -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2 font-semibold">
              <UIcon name="i-lucide-brain" class="size-4" />
              Claude Code CLI
            </div>
            <div class="flex items-center gap-2">
              <UBadge :color="authBadge.color" variant="subtle" :icon="authBadge.icon">{{ authBadge.label }}</UBadge>
              <UButton
                size="xs"
                variant="ghost"
                icon="i-lucide-refresh-cw"
                :loading="authLoading"
                @click="refreshAuth"
              />
              <UButton
                size="xs"
                :color="config.kill_switch ? 'success' : 'error'"
                :icon="config.kill_switch ? 'i-lucide-shield-check' : 'i-lucide-shield-off'"
                :label="config.kill_switch ? 'Clear Kill-Switch' : 'Activate Kill-Switch'"
                :loading="killSwitchBusy"
                variant="outline"
                @click="toggleKillSwitch"
              />
            </div>
          </div>
        </template>

        <div class="space-y-4">
          <UAlert
            v-if="authStatus && !authStatus.installed"
            icon="i-lucide-download"
            color="error"
            title="Claude CLI not found on the host"
            :description="authStatus.message || `Install with: curl -fsSL https://claude.ai/install.sh | bash`"
          />
          <UAlert
            v-else-if="authStatus && !authStatus.authenticated"
            icon="i-lucide-key-round"
            color="warning"
            title="CLI is installed but not authenticated"
            :description="authStatus.message || 'SSH to the host and run `claude login` as the deployment user.'"
          />
          <UAlert
            v-else-if="authStatus && authStatus.installed && authStatus.authenticated"
            icon="i-lucide-check-circle"
            color="success"
            title="Ready"
            :description="`CLI ${authStatus.version || 'installed'} at ${authStatus.binary_path || '(PATH)'}.`"
          />

          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium text-sm">Enable Claude Assistant</p>
              <p class="text-xs text-muted">Module must be enabled before use. Requires the CLI installed and authenticated.</p>
            </div>
            <UToggle v-model="draft.enabled" />
          </div>

          <UFormField label="CLI Binary Path" help="Absolute path to the `claude` executable. Leave blank to use PATH.">
            <UInput v-model="draft.binary_path" placeholder="/usr/local/bin/claude" class="font-mono" />
          </UFormField>

          <UFormField label="Working Directory" help="Directory the CLI runs in. Leave blank to inherit the server process working dir.">
            <UInput v-model="draft.workdir" placeholder="/home/deployment/media-server-pro" class="font-mono" />
          </UFormField>

          <UFormField label="Model">
            <USelect v-model="draft.model" :items="MODELS" value-key="value" label-key="label" />
          </UFormField>

          <UFormField label="Operational Mode" help="Advisory maps to Claude Code plan mode; Autonomous bypasses permission prompts.">
            <USelect v-model="draft.mode" :items="MODES" value-key="value" label-key="label" />
          </UFormField>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-shield" class="size-4" />
            Safety
          </div>
        </template>
        <div class="space-y-4">
          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium text-sm">Require Confirmation for Writes</p>
              <p class="text-xs text-muted">Reserved for future interactive-approval bridge. Currently informational.</p>
            </div>
            <UToggle v-model="draft.require_confirm_for_writes" />
          </div>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-sliders-horizontal" class="size-4" />
            Limits &amp; Tuning
          </div>
        </template>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Max Tokens per Response">
            <UInput
              :model-value="String(draft.max_tokens ?? 4096)"
              type="number" min="256" max="16384"
              @update:model-value="(v: string) => draft.max_tokens = Number(v)"
            />
          </UFormField>
          <UFormField label="Max Tool Calls per Turn" help="Passed to the CLI as --max-turns.">
            <UInput
              :model-value="String(draft.max_tool_calls_per_turn ?? 32)"
              type="number" min="1" max="128"
              @update:model-value="(v: string) => draft.max_tool_calls_per_turn = Number(v)"
            />
          </UFormField>
          <UFormField label="Rate Limit (turns/minute)" help="0 = no limit">
            <UInput
              :model-value="String(draft.rate_limit_per_minute ?? 30)"
              type="number" min="0" max="120"
              @update:model-value="(v: string) => draft.rate_limit_per_minute = Number(v)"
            />
          </UFormField>
          <UFormField label="Conversation Retention (days)" help="0 = keep forever">
            <UInput
              :model-value="String(draft.history_retention_days ?? 30)"
              type="number" min="0" max="365"
              @update:model-value="(v: string) => draft.history_retention_days = Number(v)"
            />
          </UFormField>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-file-text" class="size-4" />
            System Prompt Addendum
          </div>
        </template>
        <UFormField help="Appended to Claude Code's built-in system prompt via --append-system-prompt.">
          <UTextarea
            v-model="draft.system_prompt"
            :rows="5"
            placeholder="Operator notes for Claude (e.g. staging-only, skip mature scans until further notice)..."
            class="font-mono text-sm"
          />
        </UFormField>
      </UCard>

      <div class="flex justify-end">
        <UButton
          icon="i-lucide-save"
          label="Save Settings"
          :loading="saving"
          @click="save"
        />
      </div>
    </template>

    <div v-else class="text-center py-12 text-muted">
      <UIcon name="i-lucide-brain" class="size-10 mb-3 mx-auto opacity-40" />
      <p>Failed to load Claude config.</p>
      <UButton class="mt-3" size="sm" variant="outline" icon="i-lucide-refresh-cw" label="Retry" @click="load" />
    </div>
  </div>
</template>
