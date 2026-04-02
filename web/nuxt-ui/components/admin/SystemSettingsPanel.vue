<script setup lang="ts">
const adminApi = useAdminApi()
const toast = useToast()

// ── State ─────────────────────────────────────────────────────────────────
const config = ref<Record<string, any>>({})
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)

// Password change
const pwCurrent = ref('')
const pwNew = ref('')
const pwConfirm = ref('')
const pwLoading = ref(false)

// Raw JSON fallback
const showRawJson = ref(false)
const rawJsonText = ref('')

// ── Helpers ───────────────────────────────────────────────────────────────
function get(section: string, key: string) {
  return config.value[section]?.[key]
}

function set(section: string, key: string, val: any) {
  if (!config.value[section]) config.value[section] = {}
  config.value[section][key] = val
  dirty.value = true
}

function toggle(section: string, key: string) {
  set(section, key, !get(section, key))
}

function toggleHlsProfile(index: number, enabled: boolean) {
  const profiles = get('hls', 'quality_profiles')
  if (!profiles) return
  // Prevent disabling the last enabled profile
  if (!enabled) {
    const enabledCount = profiles.filter((p: any) => p.enabled).length
    if (enabledCount <= 1) return
  }
  profiles[index] = { ...profiles[index], enabled }
  set('hls', 'quality_profiles', [...profiles])
}

// ── Load / Save ───────────────────────────────────────────────────────────
async function loadConfig() {
  loading.value = true
  try {
    const cfg = await adminApi.getConfig()
    config.value = cfg ?? {}
    rawJsonText.value = JSON.stringify(cfg, null, 2)
    dirty.value = false
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load config', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    const payload = showRawJson.value ? JSON.parse(rawJsonText.value) : config.value
    await adminApi.updateConfig(payload)
    toast.add({ title: 'Configuration saved', color: 'success', icon: 'i-lucide-check' })
    dirty.value = false
    if (showRawJson.value) {
      // Reload structured view from saved data
      await loadConfig()
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Save failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    saving.value = false
  }
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
  } finally {
    pwLoading.value = false
  }
}

// Sync raw JSON when toggling
watch(showRawJson, (v) => {
  if (v) rawJsonText.value = JSON.stringify(config.value, null, 2)
})

onMounted(loadConfig)
</script>

<template>
  <div class="space-y-4">
    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-neutral-400" />
    </div>

    <template v-else>
      <!-- Save bar -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <UButton :loading="saving" :disabled="!dirty && !showRawJson" icon="i-lucide-save" label="Save Changes" @click="saveConfig" />
          <UBadge v-if="dirty" color="warning" variant="subtle" size="sm">Unsaved changes</UBadge>
        </div>
        <UButton
          :icon="showRawJson ? 'i-lucide-layout-grid' : 'i-lucide-code'"
          :label="showRawJson ? 'Structured View' : 'Raw JSON'"
          variant="ghost"
          color="neutral"
          size="sm"
          @click="showRawJson = !showRawJson"
        />
      </div>

      <!-- Raw JSON mode -->
      <template v-if="showRawJson">
        <UCard>
          <template #header><div class="font-semibold text-sm">Raw Configuration (JSON)</div></template>
          <UTextarea v-model="rawJsonText" :rows="24" class="font-mono text-xs" />
        </UCard>
      </template>

      <!-- Structured mode -->
      <template v-else>
        <!-- ── Feature Toggles ─────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-toggle-right" class="text-primary" />
              <span class="font-semibold text-sm">Feature Toggles</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-6 gap-y-3">
            <div class="flex items-center justify-between">
              <span class="text-sm">Thumbnails</span>
              <USwitch :model-value="get('features', 'enable_thumbnails')" @update:model-value="set('features', 'enable_thumbnails', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">HLS Streaming</span>
              <USwitch :model-value="get('features', 'enable_hls')" @update:model-value="set('features', 'enable_hls', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Analytics</span>
              <USwitch :model-value="get('features', 'enable_analytics')" @update:model-value="set('features', 'enable_analytics', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Uploads</span>
              <USwitch :model-value="get('features', 'enable_uploads')" @update:model-value="set('features', 'enable_uploads', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">HuggingFace AI</span>
              <USwitch :model-value="get('features', 'enable_huggingface')" @update:model-value="set('features', 'enable_huggingface', $event)" />
            </div>
          </div>
        </UCard>

        <!-- ── Server ──────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-server" class="text-primary" />
              <span class="font-semibold text-sm">Server</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-2xl">
            <UFormField label="Host">
              <UInput :model-value="get('server', 'host')" @update:model-value="set('server', 'host', $event)" placeholder="0.0.0.0" />
            </UFormField>
            <UFormField label="Port">
              <UInput type="number" :model-value="get('server', 'port')" @update:model-value="set('server', 'port', Number($event))" />
            </UFormField>
            <UFormField label="HTTPS">
              <div class="pt-2">
                <USwitch :model-value="get('server', 'enable_https')" @update:model-value="set('server', 'enable_https', $event)" />
              </div>
            </UFormField>
          </div>
        </UCard>

        <!-- ── Security ────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-shield" class="text-primary" />
              <span class="font-semibold text-sm">Security</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-2xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Rate Limiting</span>
              <USwitch :model-value="get('security', 'rate_limit_enabled')" @update:model-value="set('security', 'rate_limit_enabled', $event)" />
            </div>
            <UFormField label="Rate Limit (req/window)">
              <UInput type="number" :model-value="get('security', 'rate_limit_requests')" @update:model-value="set('security', 'rate_limit_requests', Number($event))" :disabled="!get('security', 'rate_limit_enabled')" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">IP Whitelist</span>
              <USwitch :model-value="get('security', 'enable_ip_whitelist')" @update:model-value="set('security', 'enable_ip_whitelist', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">IP Blacklist</span>
              <USwitch :model-value="get('security', 'enable_ip_blacklist')" @update:model-value="set('security', 'enable_ip_blacklist', $event)" />
            </div>
          </div>
        </UCard>

        <!-- ── Thumbnails ──────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-image" class="text-primary" />
              <span class="font-semibold text-sm">Thumbnails</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Auto Generate</span>
              <USwitch :model-value="get('thumbnails', 'auto_generate')" @update:model-value="set('thumbnails', 'auto_generate', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Generate on Access</span>
              <USwitch :model-value="get('thumbnails', 'generate_on_access')" @update:model-value="set('thumbnails', 'generate_on_access', $event)" />
            </div>
            <UFormField label="Preview Count">
              <UInput type="number" :model-value="get('thumbnails', 'preview_count')" @update:model-value="set('thumbnails', 'preview_count', Number($event))" />
            </UFormField>
            <UFormField label="Width (px)">
              <UInput type="number" :model-value="get('thumbnails', 'width')" @update:model-value="set('thumbnails', 'width', Number($event))" />
            </UFormField>
            <UFormField label="Height (px)">
              <UInput type="number" :model-value="get('thumbnails', 'height')" @update:model-value="set('thumbnails', 'height', Number($event))" />
            </UFormField>
            <UFormField label="Quality (1-100)">
              <UInput type="number" :model-value="get('thumbnails', 'quality')" @update:model-value="set('thumbnails', 'quality', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── HLS ─────────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-play-circle" class="text-primary" />
              <span class="font-semibold text-sm">HLS Streaming</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('hls', 'enabled')" @update:model-value="set('hls', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Auto Generate</span>
              <USwitch :model-value="get('hls', 'auto_generate')" @update:model-value="set('hls', 'auto_generate', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Lazy Transcode</span>
              <USwitch :model-value="get('hls', 'lazy_transcode')" @update:model-value="set('hls', 'lazy_transcode', $event)" />
            </div>
            <UFormField label="Concurrent Limit">
              <UInput type="number" :model-value="get('hls', 'concurrent_limit')" @update:model-value="set('hls', 'concurrent_limit', Number($event))" />
            </UFormField>
            <UFormField label="Segment Duration (s)">
              <UInput type="number" :model-value="get('hls', 'segment_duration')" @update:model-value="set('hls', 'segment_duration', Number($event))" />
            </UFormField>
            <UFormField label="Retention (minutes)">
              <UInput type="number" :model-value="get('hls', 'retention_minutes')" @update:model-value="set('hls', 'retention_minutes', Number($event))" />
            </UFormField>
          </div>
          <!-- Quality profiles -->
          <div v-if="get('hls', 'quality_profiles')?.length" class="mt-4">
            <p class="text-xs text-neutral-400 mb-2">Quality Profiles</p>
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div
                v-for="(p, i) in get('hls', 'quality_profiles')"
                :key="i"
                class="flex items-center gap-3 rounded-lg border border-neutral-700 px-3 py-2"
                :class="p.enabled ? 'bg-neutral-800/50' : 'bg-neutral-900/50 opacity-50'"
              >
                <USwitch
                  :model-value="p.enabled"
                  @update:model-value="toggleHlsProfile(Number(i), $event)"
                  size="sm"
                />
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium" :class="p.enabled ? 'text-white' : 'text-neutral-500'">
                    {{ p.name }}
                  </div>
                  <div class="text-xs text-neutral-500">
                    {{ p.width }}x{{ p.height }} &mdash; {{ Math.round(p.bitrate / 1000) }}k video / {{ Math.round(p.audio_bitrate / 1000) }}k audio
                  </div>
                </div>
              </div>
            </div>
            <p class="text-xs text-neutral-500 mt-2">Toggle profiles to control which quality levels are generated. At least one must be enabled.</p>
          </div>
        </UCard>

        <!-- ── Analytics ───────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-bar-chart-3" class="text-primary" />
              <span class="font-semibold text-sm">Analytics</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-2xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('analytics', 'enabled')" @update:model-value="set('analytics', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Track Playback</span>
              <USwitch :model-value="get('analytics', 'track_playback')" @update:model-value="set('analytics', 'track_playback', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Track Views</span>
              <USwitch :model-value="get('analytics', 'track_views')" @update:model-value="set('analytics', 'track_views', $event)" />
            </div>
          </div>
        </UCard>

        <!-- ── Mature Content Scanner ──────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-eye-off" class="text-primary" />
              <span class="font-semibold text-sm">Content Scanner</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('mature_scanner', 'enabled')" @update:model-value="set('mature_scanner', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Auto Flag</span>
              <USwitch :model-value="get('mature_scanner', 'auto_flag')" @update:model-value="set('mature_scanner', 'auto_flag', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Require Review</span>
              <USwitch :model-value="get('mature_scanner', 'require_review')" @update:model-value="set('mature_scanner', 'require_review', $event)" />
            </div>
            <UFormField label="High Confidence Threshold">
              <UInput type="number" step="0.01" :model-value="get('mature_scanner', 'high_confidence_threshold')" @update:model-value="set('mature_scanner', 'high_confidence_threshold', Number($event))" />
            </UFormField>
            <UFormField label="Medium Confidence Threshold">
              <UInput type="number" step="0.01" :model-value="get('mature_scanner', 'medium_confidence_threshold')" @update:model-value="set('mature_scanner', 'medium_confidence_threshold', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── HuggingFace AI ──────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-brain" class="text-primary" />
              <span class="font-semibold text-sm">HuggingFace AI Classification</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('huggingface', 'enabled')" @update:model-value="set('huggingface', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between col-span-1 sm:col-span-2">
              <span class="text-sm">API Key</span>
              <UBadge :color="get('huggingface', 'api_key_set') ? 'success' : 'error'" variant="subtle" size="sm">
                {{ get('huggingface', 'api_key_set') ? 'Configured' : 'Not set' }}
              </UBadge>
            </div>
            <UFormField label="Model">
              <UInput :model-value="get('huggingface', 'model')" @update:model-value="set('huggingface', 'model', $event)" placeholder="google/vit-base-patch16-224" />
            </UFormField>
            <UFormField label="Rate Limit (req/min)">
              <UInput type="number" :model-value="get('huggingface', 'rate_limit')" @update:model-value="set('huggingface', 'rate_limit', Number($event))" />
            </UFormField>
            <UFormField label="Max Concurrent">
              <UInput type="number" :model-value="get('huggingface', 'max_concurrent')" @update:model-value="set('huggingface', 'max_concurrent', Number($event))" />
            </UFormField>
            <UFormField label="Timeout (seconds)">
              <UInput type="number" :model-value="get('huggingface', 'timeout_secs')" @update:model-value="set('huggingface', 'timeout_secs', Number($event))" />
            </UFormField>
            <UFormField label="Max Frames">
              <UInput type="number" :model-value="get('huggingface', 'max_frames')" @update:model-value="set('huggingface', 'max_frames', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Database (read-only) ────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-database" class="text-primary" />
              <span class="font-semibold text-sm">Database</span>
              <UBadge variant="subtle" color="neutral" size="xs">Read-only</UBadge>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-3xl">
            <UFormField label="Host">
              <UInput :model-value="get('database', 'host')" disabled />
            </UFormField>
            <UFormField label="Port">
              <UInput :model-value="get('database', 'port')" disabled />
            </UFormField>
            <UFormField label="Database">
              <UInput :model-value="get('database', 'name')" disabled />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Database settings can only be changed via environment variables or config file.</p>
        </UCard>
      </template>

      <!-- ── Change Password ─────────────────────────────────────── -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-key-round" class="text-primary" />
            <span class="font-semibold text-sm">Change Admin Password</span>
          </div>
        </template>
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

      <!-- ── Developer Links ─────────────────────────────────────── -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-code-2" class="text-primary" />
            <span class="font-semibold text-sm">Developer Links</span>
          </div>
        </template>
        <div class="flex flex-wrap gap-2">
          <UButton icon="i-lucide-file-code" label="OpenAPI Spec" variant="outline" color="neutral" size="sm" to="/api/docs" target="_blank" external />
          <UButton icon="i-lucide-bar-chart-2" label="Prometheus Metrics" variant="outline" color="neutral" size="sm" to="/metrics" target="_blank" external />
        </div>
      </UCard>
    </template>
  </div>
</template>
