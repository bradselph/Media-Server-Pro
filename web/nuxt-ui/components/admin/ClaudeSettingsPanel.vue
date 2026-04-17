<script setup lang="ts">
import type { ClaudePublicConfig, ClaudeConfigUpdate } from '~/composables/useApiEndpoints'

const emit = defineEmits<{ (e: 'config-changed'): void }>()

const adminApi = useAdminApi()
const toast = useToast()

const config = ref<ClaudePublicConfig | null>(null)
const loading = ref(false)
const saving = ref(false)
const killSwitchBusy = ref(false)

// Draft fields bound to form inputs
const draft = ref<ClaudeConfigUpdate & { api_key?: string; web_login_token?: string }>({})

const MODELS = [
  { label: 'Claude Sonnet 4.6 (recommended)', value: 'claude-sonnet-4-6' },
  { label: 'Claude Opus 4.7', value: 'claude-opus-4-7' },
  { label: 'Claude Haiku 4.5', value: 'claude-haiku-4-5-20251001' },
]

const MODES = [
  { label: 'Advisory — suggests only, no execution', value: 'advisory' },
  { label: 'Interactive — proposes, admin confirms writes', value: 'interactive' },
  { label: 'Autonomous — executes within scope and rate limit', value: 'autonomous' },
]

async function load() {
  loading.value = true
  try {
    config.value = await adminApi.getClaudeConfig()
    if (config.value) {
      draft.value = {
        enabled: config.value.enabled,
        model: config.value.model || 'claude-sonnet-4-6',
        mode: config.value.mode || 'advisory',
        max_tokens: config.value.max_tokens || 4096,
        system_prompt: config.value.system_prompt || '',
        allowed_tools: [...(config.value.allowed_tools ?? [])],
        allowed_shell_commands: [...(config.value.allowed_shell_commands ?? [])],
        allowed_paths: [...(config.value.allowed_paths ?? [])],
        allowed_services: [...(config.value.allowed_services ?? [])],
        require_confirm_for_writes: config.value.require_confirm_for_writes,
        max_tool_calls_per_turn: config.value.max_tool_calls_per_turn || 16,
        rate_limit_per_minute: config.value.rate_limit_per_minute || 30,
        kill_switch: config.value.kill_switch,
        history_retention_days: config.value.history_retention_days || 30,
      }
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load Claude config', color: 'error', icon: 'i-lucide-x' })
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const update: ClaudeConfigUpdate & { api_key?: string; web_login_token?: string } = { ...draft.value }
    // Only send credentials if the user typed something
    if (!update.api_key) delete update.api_key
    if (!update.web_login_token) delete update.web_login_token
    config.value = await adminApi.updateClaudeConfig(update)
    toast.add({ title: 'Claude settings saved', color: 'success', icon: 'i-lucide-check' })
    draft.value.api_key = ''
    draft.value.web_login_token = ''
    emit('config-changed')
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
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
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    killSwitchBusy.value = false
  }
}

// Tag-list helpers (comma-separated textarea ↔ string[])
function listToText(arr: string[] | undefined): string {
  return (arr ?? []).join('\n')
}
function textToList(text: string): string[] {
  return text.split(/[\n,]/).map(s => s.trim()).filter(Boolean)
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div v-if="loading && !config" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <template v-else-if="config">
      <!-- Kill-switch banner -->
      <UAlert
        v-if="config.kill_switch"
        icon="i-lucide-shield-off"
        color="warning"
        title="Kill-switch is active"
        description="All Claude activity is blocked. Clear the kill-switch to resume."
      />

      <!-- Module status -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2 font-semibold">
              <UIcon name="i-lucide-brain" class="size-4" />
              Claude Admin Assistant
            </div>
            <div class="flex items-center gap-2">
              <UBadge v-if="config.api_key_set" color="success" variant="subtle" icon="i-lucide-key">API Key Set</UBadge>
              <UBadge v-else-if="config.web_login_token_set" color="success" variant="subtle" icon="i-lucide-key">Web Token Set</UBadge>
              <UBadge v-else color="warning" variant="subtle" icon="i-lucide-key">No Credentials</UBadge>
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
          <!-- Enable / Disable -->
          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium text-sm">Enable Claude Assistant</p>
              <p class="text-xs text-muted">Module must be enabled before use. Requires a valid API key.</p>
            </div>
            <UToggle v-model="draft.enabled" />
          </div>

          <!-- Credentials: API Key or Web Login Token -->
          <UFormField label="Anthropic API Key" help="Write-only. Leave blank to keep the existing value. Takes precedence over web login token.">
            <UInput
              v-model="draft.api_key"
              type="password"
              :placeholder="config.api_key_set ? '••••••••••••  (key already set)' : 'sk-ant-...'"
              class="font-mono"
            />
          </UFormField>
          <UFormField label="Web Login Token" help="OAuth / user-access token from claude.ai. Write-only. Used when no API key is set.">
            <UInput
              v-model="draft.web_login_token"
              type="password"
              :placeholder="config.web_login_token_set ? '••••••••••••  (token already set)' : 'Paste token...'"
              class="font-mono"
            />
          </UFormField>

          <!-- Model -->
          <UFormField label="Model">
            <USelect v-model="draft.model" :items="MODELS" value-key="value" label-key="label" />
          </UFormField>

          <!-- Mode -->
          <UFormField label="Operational Mode" help="Controls whether Claude can execute actions or only suggest them.">
            <USelect v-model="draft.mode" :items="MODES" value-key="value" label-key="label" />
          </UFormField>
        </div>
      </UCard>

      <!-- Scope & Safety -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-shield" class="size-4" />
            Scope & Safety
          </div>
        </template>

        <div class="space-y-4">
          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium text-sm">Require Confirmation for Writes</p>
              <p class="text-xs text-muted">Always gate file writes and shell commands, even in Autonomous mode.</p>
            </div>
            <UToggle v-model="draft.require_confirm_for_writes" />
          </div>

          <!-- Allowed Tools -->
          <UFormField
            label="Allowed Tools"
            :help="config.available_tools.length
              ? `Available: ${config.available_tools.join(', ')}. Leave empty to allow all.`
              : 'Leave empty to allow all registered tools.'"
          >
            <div class="space-y-1">
              <div
                v-for="tool in config.available_tools"
                :key="tool"
                class="flex items-center gap-2"
              >
                <UCheckbox
                  :model-value="!draft.allowed_tools?.length || draft.allowed_tools.includes(tool)"
                  :label="tool"
                  @update:model-value="(v: boolean | 'indeterminate') => {
                    const checked = v === true
                    if (!draft.allowed_tools?.length) {
                      draft.allowed_tools = checked
                        ? config!.available_tools.filter(t => t !== tool)
                        : [...config!.available_tools]
                    } else {
                      draft.allowed_tools = checked
                        ? [...(draft.allowed_tools ?? []), tool]
                        : (draft.allowed_tools ?? []).filter(t => t !== tool)
                    }
                  }"
                />
              </div>
              <p v-if="!config.available_tools.length" class="text-xs text-muted">No tools registered (module not loaded yet).</p>
            </div>
          </UFormField>

          <!-- Allowed Shell Commands -->
          <UFormField
            label="Allowed Shell Commands"
            help="One command name per line (e.g. ls, df, systemctl). Empty = deny all."
          >
            <UTextarea
              :model-value="listToText(draft.allowed_shell_commands)"
              :rows="3"
              placeholder="ls&#10;df&#10;systemctl"
              class="font-mono text-sm"
              @update:model-value="(v: string) => draft.allowed_shell_commands = textToList(v)"
            />
          </UFormField>

          <!-- Allowed Paths -->
          <UFormField
            label="Allowed File Paths"
            help="One path prefix per line. Empty = deny all file read/write."
          >
            <UTextarea
              :model-value="listToText(draft.allowed_paths)"
              :rows="3"
              placeholder="/opt/media-server/logs&#10;/etc/media-server"
              class="font-mono text-sm"
              @update:model-value="(v: string) => draft.allowed_paths = textToList(v)"
            />
          </UFormField>

          <!-- Allowed Services -->
          <UFormField
            label="Allowed systemd Services"
            help="One service name per line (e.g. media-server). Empty = deny all restarts."
          >
            <UTextarea
              :model-value="listToText(draft.allowed_services)"
              :rows="2"
              placeholder="media-server"
              class="font-mono text-sm"
              @update:model-value="(v: string) => draft.allowed_services = textToList(v)"
            />
          </UFormField>
        </div>
      </UCard>

      <!-- Limits -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-sliders-horizontal" class="size-4" />
            Limits & Tuning
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
          <UFormField label="Max Tool Calls per Turn">
            <UInput
              :model-value="String(draft.max_tool_calls_per_turn ?? 16)"
              type="number" min="1" max="64"
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

      <!-- System Prompt -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-file-text" class="size-4" />
            System Prompt Override
          </div>
        </template>
        <UFormField help="Leave blank to use the built-in system prompt.">
          <UTextarea
            v-model="draft.system_prompt"
            :rows="5"
            placeholder="You are an admin assistant for Media Server Pro 4..."
            class="font-mono text-sm"
          />
        </UFormField>
      </UCard>

      <!-- Save -->
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
