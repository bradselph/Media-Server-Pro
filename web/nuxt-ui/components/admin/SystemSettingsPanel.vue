<script setup lang="ts">
import { useAdminFeedback } from '~/composables/useAdminFeedback'

const adminApi = useAdminApi()
const { notifyError, notifySuccess } = useAdminFeedback()

// Known top-level config sections — matches the Go config struct sections.
// Adding a new section here (and in Go) keeps both sides in sync.
type ConfigSection =
  | 'admin' | 'age_gate' | 'analytics' | 'auth' | 'backup'
  | 'cookie_consent' | 'crawler' | 'database' | 'directories'
  | 'download' | 'downloader' | 'extractor' | 'features' | 'hls'
  | 'huggingface' | 'logging' | 'mature_scanner' | 'receiver'
  | 'remote_media' | 'security' | 'server' | 'storage' | 'streaming'
  | 'thumbnails' | 'ui' | 'updater' | 'uploads'

// ── State ─────────────────────────────────────────────────────────────────
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const config = ref<Partial<Record<ConfigSection, Record<string, any>>>>({})
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)

let mounted = true
onUnmounted(() => { mounted = false })

// Password change
const pwCurrent = ref('')
const pwNew = ref('')
const pwConfirm = ref('')
const pwLoading = ref(false)

// Raw JSON fallback
const showRawJson = ref(false)
const rawJsonText = ref('')

// ── Helpers ───────────────────────────────────────────────────────────────
function get(section: ConfigSection, key: string) {
  return config.value[section]?.[key]
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function set(section: ConfigSection, key: string, val: any) {
  if (!config.value[section]) config.value[section] = {}
  config.value[section]![key] = val
  dirty.value = true
}

function toggle(section: ConfigSection, key: string) {
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
    notifyError(e, 'Failed to load config', 'i-lucide-alert-circle')
  } finally {
    loading.value = false
  }
}

const jsonError = ref('')

async function saveConfig() {
  jsonError.value = ''
  if (showRawJson.value) {
    try { JSON.parse(rawJsonText.value) }
    catch (e) {
      jsonError.value = e instanceof SyntaxError ? e.message : 'Invalid JSON'
      return
    }
  }
  saving.value = true
  try {
    const payload = showRawJson.value ? JSON.parse(rawJsonText.value) : config.value
    await adminApi.updateConfig(payload)
    if (!mounted) return
    notifySuccess('Configuration saved')
    dirty.value = false
    if (showRawJson.value) {
      // Reload structured view from saved data
      await loadConfig()
    }
  } catch (e: unknown) {
    if (!mounted) return
    notifyError(e, 'Save failed')
  } finally {
    if (mounted) saving.value = false
  }
}

async function changeAdminPassword() {
  if (!pwCurrent.value) {
    notifyError('Current password is required')
    return
  }
  if (pwNew.value.length < 8) {
    notifyError('New password must be at least 8 characters')
    return
  }
  if (pwNew.value !== pwConfirm.value) {
    notifyError('Passwords do not match')
    return
  }
  pwLoading.value = true
  try {
    await adminApi.changeOwnPassword(pwCurrent.value, pwNew.value)
    if (!mounted) return
    notifySuccess('Password changed')
    pwCurrent.value = ''; pwNew.value = ''; pwConfirm.value = ''
  } catch (e: unknown) {
    if (!mounted) return
    notifyError(e, 'Failed')
  } finally {
    if (mounted) pwLoading.value = false
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
          <UTextarea v-model="rawJsonText" :rows="24" class="font-mono text-xs" @input="jsonError = ''" />
          <UAlert v-if="jsonError" :title="jsonError" color="error" variant="soft" icon="i-lucide-x-circle" class="mt-2" />
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
            <div class="flex items-center justify-between">
              <span class="text-sm">Playlists</span>
              <USwitch :model-value="get('features', 'enable_playlists')" @update:model-value="set('features', 'enable_playlists', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Suggestions</span>
              <USwitch :model-value="get('features', 'enable_suggestions')" @update:model-value="set('features', 'enable_suggestions', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Auto Discovery</span>
              <USwitch :model-value="get('features', 'enable_auto_discovery')" @update:model-value="set('features', 'enable_auto_discovery', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Content Scanner</span>
              <USwitch :model-value="get('features', 'enable_mature_scanner')" @update:model-value="set('features', 'enable_mature_scanner', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Remote Media</span>
              <USwitch :model-value="get('features', 'enable_remote_media')" @update:model-value="set('features', 'enable_remote_media', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Receiver</span>
              <USwitch :model-value="get('features', 'enable_receiver')" @update:model-value="set('features', 'enable_receiver', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Extractor</span>
              <USwitch :model-value="get('features', 'enable_extractor')" @update:model-value="set('features', 'enable_extractor', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Crawler</span>
              <USwitch :model-value="get('features', 'enable_crawler')" @update:model-value="set('features', 'enable_crawler', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Duplicate Detection</span>
              <USwitch :model-value="get('features', 'enable_duplicate_detection')" @update:model-value="set('features', 'enable_duplicate_detection', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Downloader</span>
              <USwitch :model-value="get('features', 'enable_downloader')" @update:model-value="set('features', 'enable_downloader', $event)" />
            </div>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Some toggles require a server restart to take effect.</p>
        </UCard>

        <!-- ── Server ──────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-server" class="text-primary" />
              <span class="font-semibold text-sm">Server</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-3xl">
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
            <template v-if="get('server', 'enable_https')">
              <UFormField label="Certificate File">
                <UInput :model-value="get('server', 'cert_file')" @update:model-value="set('server', 'cert_file', $event)" placeholder="/path/to/cert.pem" />
              </UFormField>
              <UFormField label="Key File">
                <UInput :model-value="get('server', 'key_file')" @update:model-value="set('server', 'key_file', $event)" placeholder="/path/to/key.pem" />
              </UFormField>
            </template>
            <UFormField label="Max Header Bytes">
              <UInput type="number" :model-value="get('server', 'max_header_bytes')" @update:model-value="set('server', 'max_header_bytes', Number($event))" />
            </UFormField>
            <UFormField label="Memory Limit (% of RAM)" help="Go soft memory limit as a % of total RAM. 0 = auto (75%). Lets a large server use its RAM as GC headroom. Applies live; ignored if GOMEMLIMIT is set.">
              <UInput type="number" min="0" max="95" :model-value="get('server', 'memory_limit_percent')" @update:model-value="set('server', 'memory_limit_percent', Number($event))" />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Server address/port and HTTP timeouts (read/write/idle/shutdown) require a restart and live in raw JSON.</p>
        </UCard>

        <!-- ── Storage Backend ──────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-hard-drive" class="text-primary" />
              <span class="font-semibold text-sm">Storage Backend</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-4xl">
            <UFormField label="Backend">
              <USelect :model-value="get('storage', 'backend') || 'local'" :items="[{label:'Local Filesystem',value:'local'},{label:'S3 Compatible (B2, AWS, MinIO)',value:'s3'}]" @update:model-value="set('storage', 'backend', $event)" />
            </UFormField>
            <template v-if="get('storage', 'backend') === 's3'">
              <UFormField label="S3 Endpoint">
                <UInput :model-value="get('storage', 's3')?.endpoint" @update:model-value="set('storage', 's3', { ...get('storage', 's3'), endpoint: $event })" placeholder="s3.us-west-004.backblazeb2.com" />
              </UFormField>
              <UFormField label="Region">
                <UInput :model-value="get('storage', 's3')?.region" @update:model-value="set('storage', 's3', { ...get('storage', 's3'), region: $event })" placeholder="us-west-004" />
              </UFormField>
              <UFormField label="Access Key ID">
                <UInput :model-value="get('storage', 's3')?.access_key_id" @update:model-value="set('storage', 's3', { ...get('storage', 's3'), access_key_id: $event })" placeholder="Application Key ID" />
              </UFormField>
              <UFormField label="Secret Access Key">
                <UInput type="password" :model-value="get('storage', 's3')?.secret_access_key" @update:model-value="set('storage', 's3', { ...get('storage', 's3'), secret_access_key: $event })" placeholder="••••••••" />
              </UFormField>
              <UFormField label="Bucket">
                <UInput :model-value="get('storage', 's3')?.bucket" @update:model-value="set('storage', 's3', { ...get('storage', 's3'), bucket: $event })" placeholder="my-media-bucket" />
              </UFormField>
              <div class="flex items-center justify-between">
                <span class="text-sm">Path-Style Access</span>
                <USwitch :model-value="get('storage', 's3')?.use_path_style" @update:model-value="set('storage', 's3', { ...get('storage', 's3'), use_path_style: $event })" />
              </div>
            </template>
          </div>
          <!-- Per-role key prefixes: lets a single bucket host multiple roles
               (or multiple deployments) without colliding. Each input is
               optional; empty falls through to "<role>/" in pkg/storage. -->
          <div v-if="get('storage', 'backend') === 's3'" class="mt-4">
            <p class="text-xs font-semibold text-muted uppercase tracking-wide mb-2">Per-Role Key Prefixes <span class="font-normal normal-case opacity-70">(optional)</span></p>
            <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 max-w-4xl">
              <UFormField v-for="role in ['videos', 'music', 'thumbnails', 'uploads', 'hls_cache']" :key="role" :label="role">
                <UInput
                  :model-value="get('storage', 's3')?.prefixes?.[role] || ''"
                  :placeholder="`${role}/`"
                  @update:model-value="set('storage', 's3', { ...get('storage', 's3'), prefixes: { ...(get('storage', 's3')?.prefixes || {}), [role]: $event } })"
                />
              </UFormField>
            </div>
            <p class="text-xs text-neutral-500 mt-2">
              Defaults to <code>&lt;role&gt;/</code>. Useful when sharing a bucket across
              environments (e.g. set <code>prod/videos/</code> and <code>staging/videos/</code>).
            </p>
          </div>
          <p v-if="get('storage', 'backend') === 's3'" class="text-xs text-neutral-500 mt-3">
            S3-compatible storage works with Backblaze B2, AWS S3, MinIO, Cloudflare R2, and Wasabi. Changing storage backend requires a server restart.
          </p>
          <p v-else class="text-xs text-neutral-500 mt-3">
            Files are stored on the local filesystem. Switch to S3 for cloud/CDN-backed storage.
          </p>
        </UCard>

        <!-- ── Security ────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-shield" class="text-primary" />
              <span class="font-semibold text-sm">Security</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-4xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Rate Limiting</span>
              <USwitch :model-value="get('security', 'rate_limit_enabled')" @update:model-value="set('security', 'rate_limit_enabled', $event)" />
            </div>
            <UFormField label="Rate Limit (req/window)">
              <UInput type="number" :model-value="get('security', 'rate_limit_requests')" @update:model-value="set('security', 'rate_limit_requests', Number($event))" :disabled="!get('security', 'rate_limit_enabled')" />
            </UFormField>
            <UFormField label="Burst Limit">
              <UInput type="number" :model-value="get('security', 'burst_limit')" @update:model-value="set('security', 'burst_limit', Number($event))" :disabled="!get('security', 'rate_limit_enabled')" />
            </UFormField>
            <UFormField label="Auth Rate Limit (req/window)">
              <UInput type="number" :model-value="get('security', 'auth_rate_limit')" @update:model-value="set('security', 'auth_rate_limit', Number($event))" />
            </UFormField>
            <UFormField label="Auth Burst Limit">
              <UInput type="number" :model-value="get('security', 'auth_burst_limit')" @update:model-value="set('security', 'auth_burst_limit', Number($event))" />
            </UFormField>
            <UFormField label="Violations for Ban">
              <UInput type="number" :model-value="get('security', 'violations_for_ban')" @update:model-value="set('security', 'violations_for_ban', Number($event))" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">IP Whitelist</span>
              <USwitch :model-value="get('security', 'enable_ip_whitelist')" @update:model-value="set('security', 'enable_ip_whitelist', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">IP Blacklist</span>
              <USwitch :model-value="get('security', 'enable_ip_blacklist')" @update:model-value="set('security', 'enable_ip_blacklist', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Content Security Policy</span>
              <USwitch :model-value="get('security', 'csp_enabled')" @update:model-value="set('security', 'csp_enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">HSTS</span>
              <USwitch :model-value="get('security', 'hsts_enabled')" @update:model-value="set('security', 'hsts_enabled', $event)" />
            </div>
            <UFormField label="HSTS Max Age (s)">
              <UInput type="number" :model-value="get('security', 'hsts_max_age')" @update:model-value="set('security', 'hsts_max_age', Number($event))" :disabled="!get('security', 'hsts_enabled')" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">CORS</span>
              <USwitch :model-value="get('security', 'cors_enabled')" @update:model-value="set('security', 'cors_enabled', $event)" />
            </div>
            <UFormField label="Max File Size (MB, 0=no limit)">
              <UInput type="number" :model-value="get('security', 'max_file_size_mb')" @update:model-value="set('security', 'max_file_size_mb', Number($event))" />
            </UFormField>
          </div>
          <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mt-4 max-w-4xl">
            <UFormField label="IP Whitelist (one per line, CIDR ok)">
              <UTextarea
                :model-value="(get('security', 'ip_whitelist') || []).join('\n')"
                @update:model-value="set('security', 'ip_whitelist', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                placeholder="203.0.113.0/24"
                :rows="3"
                :disabled="!get('security', 'enable_ip_whitelist')"
              />
            </UFormField>
            <UFormField label="IP Blacklist (one per line, CIDR ok)">
              <UTextarea
                :model-value="(get('security', 'ip_blacklist') || []).join('\n')"
                @update:model-value="set('security', 'ip_blacklist', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                placeholder="198.51.100.5"
                :rows="3"
                :disabled="!get('security', 'enable_ip_blacklist')"
              />
            </UFormField>
            <UFormField label="Trusted Proxy CIDRs (X-Forwarded-For source)">
              <UTextarea
                :model-value="(get('security', 'trusted_proxy_cidrs') || []).join('\n')"
                @update:model-value="set('security', 'trusted_proxy_cidrs', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                placeholder="10.0.0.0/8&#10;172.16.0.0/12"
                :rows="3"
              />
            </UFormField>
            <UFormField label="CORS Allowed Origins (one per line)">
              <UTextarea
                :model-value="(get('security', 'cors_origins') || []).join('\n')"
                @update:model-value="set('security', 'cors_origins', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                placeholder="https://app.example.com"
                :rows="3"
                :disabled="!get('security', 'cors_enabled')"
              />
            </UFormField>
            <UFormField label="CSP Policy" class="lg:col-span-2">
              <UTextarea
                :model-value="get('security', 'csp_policy')"
                @update:model-value="set('security', 'csp_policy', $event)"
                placeholder="default-src 'self'; img-src 'self' data: https:; …"
                :rows="3"
                :disabled="!get('security', 'csp_enabled')"
              />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">CSP, HSTS, CORS, IP toggles and rate limits all hot-reload — no restart needed. Rate-limit windows, burst window and ban duration live in raw JSON / config.json.</p>
        </UCard>

        <!-- ── Streaming ──────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-radio" class="text-primary" />
              <span class="font-semibold text-sm">Streaming</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Require Auth</span>
              <USwitch :model-value="get('streaming', 'require_auth')" @update:model-value="set('streaming', 'require_auth', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Mobile Optimization</span>
              <USwitch :model-value="get('streaming', 'mobile_optimization')" @update:model-value="set('streaming', 'mobile_optimization', $event)" />
            </div>
            <UFormField label="Unauth Stream Limit (per IP)">
              <UInput type="number" :model-value="get('streaming', 'unauth_stream_limit')" @update:model-value="set('streaming', 'unauth_stream_limit', Number($event))" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">Keep-Alive</span>
              <USwitch :model-value="get('streaming', 'keep_alive_enabled')" @update:model-value="set('streaming', 'keep_alive_enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Adaptive Bitrate (HLS)</span>
              <USwitch :model-value="get('streaming', 'adaptive')" @update:model-value="set('streaming', 'adaptive', $event)" />
            </div>
            <UFormField label="Default Chunk Size (bytes)">
              <UInput type="number" :model-value="get('streaming', 'default_chunk_size')" @update:model-value="set('streaming', 'default_chunk_size', Number($event))" />
            </UFormField>
            <UFormField label="Max Chunk Size (bytes)">
              <UInput type="number" :model-value="get('streaming', 'max_chunk_size')" @update:model-value="set('streaming', 'max_chunk_size', Number($event))" />
            </UFormField>
            <UFormField label="Mobile Chunk Size (bytes)">
              <UInput type="number" :model-value="get('streaming', 'mobile_chunk_size')" @update:model-value="set('streaming', 'mobile_chunk_size', Number($event))" />
            </UFormField>
            <UFormField label="Buffer Size (bytes)">
              <UInput type="number" :model-value="get('streaming', 'buffer_size')" @update:model-value="set('streaming', 'buffer_size', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Download ──────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-download" class="text-primary" />
              <span class="font-semibold text-sm">Download</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('download', 'enabled')" @update:model-value="set('download', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Require Auth</span>
              <USwitch :model-value="get('download', 'require_auth')" @update:model-value="set('download', 'require_auth', $event)" />
            </div>
            <UFormField label="Chunk Size (KB)">
              <UInput type="number" :model-value="get('download', 'chunk_size_kb')" @update:model-value="set('download', 'chunk_size_kb', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Uploads ────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-upload" class="text-primary" />
              <span class="font-semibold text-sm">Uploads</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('uploads', 'enabled')" @update:model-value="set('uploads', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Require Auth</span>
              <USwitch :model-value="get('uploads', 'require_auth')" @update:model-value="set('uploads', 'require_auth', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Scan for Mature</span>
              <USwitch :model-value="get('uploads', 'scan_for_mature')" @update:model-value="set('uploads', 'scan_for_mature', $event)" />
            </div>
            <UFormField label="Max File Size (bytes)">
              <UInput type="number" :model-value="get('uploads', 'max_file_size')" @update:model-value="set('uploads', 'max_file_size', Number($event))" />
            </UFormField>
            <UFormField label="Allowed Extensions (one per line)" class="sm:col-span-2 lg:col-span-2">
              <UTextarea
                :model-value="(get('uploads', 'allowed_extensions') || []).join('\n')"
                @update:model-value="set('uploads', 'allowed_extensions', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                :rows="3"
                placeholder=".mp4&#10;.mkv&#10;.mp3"
              />
            </UFormField>
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
            <UFormField label="Video Interval (s)">
              <UInput type="number" :model-value="get('thumbnails', 'video_interval')" @update:model-value="set('thumbnails', 'video_interval', Number($event))" />
            </UFormField>
            <UFormField label="Worker Count">
              <UInput type="number" :model-value="get('thumbnails', 'worker_count')" @update:model-value="set('thumbnails', 'worker_count', Number($event))" />
            </UFormField>
            <UFormField label="Queue Size">
              <UInput type="number" :model-value="get('thumbnails', 'queue_size')" @update:model-value="set('thumbnails', 'queue_size', Number($event))" />
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
            <UFormField label="Concurrent Limit" help="How many transcodes run at once. Raising this only helps throughput if the CPU/GPU has spare capacity — a single video's speed is set by the encoder below.">
              <UInput type="number" :model-value="get('hls', 'concurrent_limit')" @update:model-value="set('hls', 'concurrent_limit', Number($event))" />
            </UFormField>
            <UFormField label="Hardware Acceleration" help="auto probes for a GPU encoder (NVENC/QSV/VAAPI) and falls back to software libx264. On a CPU-only server this stays software.">
              <USelect
                :model-value="get('hls', 'hardware_accel') || 'auto'"
                :items="[{label:'Auto (detect GPU, else software)',value:'auto'},{label:'Software only (libx264)',value:'none'},{label:'NVIDIA NVENC',value:'nvenc'},{label:'Intel QuickSync (QSV)',value:'qsv'},{label:'VAAPI (Linux /dev/dri)',value:'vaapi'},{label:'Apple VideoToolbox',value:'videotoolbox'}]"
                @update:model-value="set('hls', 'hardware_accel', $event)"
              />
            </UFormField>
            <UFormField label="Segment Duration (s)">
              <UInput type="number" :model-value="get('hls', 'segment_duration')" @update:model-value="set('hls', 'segment_duration', Number($event))" />
            </UFormField>
            <UFormField label="Retention (minutes)">
              <UInput type="number" :model-value="get('hls', 'retention_minutes')" @update:model-value="set('hls', 'retention_minutes', Number($event))" />
            </UFormField>
            <UFormField label="Cleanup Interval (seconds)" help="How often the hls-inactive-cleanup task runs. Minimum 60s.">
              <UInput
                type="number"
                min="60"
                :model-value="Math.round(Number(get('hls', 'cleanup_interval') ?? 0) / 1_000_000_000)"
                @update:model-value="set('hls', 'cleanup_interval', Number($event) * 1_000_000_000)"
              />
            </UFormField>
            <div class="flex items-start justify-between gap-3">
              <div>
                <span class="text-sm">Cleanup Enabled</span>
                <p class="text-xs text-muted mt-0.5">When on, the <code>hls-inactive-cleanup</code> task evicts cached jobs idle longer than the retention above. Off by default — leaves the cache untouched.</p>
              </div>
              <USwitch :model-value="get('hls', 'cleanup_enabled')" @update:model-value="set('hls', 'cleanup_enabled', $event)" />
            </div>
            <UFormField label="CDN Base URL">
              <UInput :model-value="get('hls', 'cdn_base_url')" @update:model-value="set('hls', 'cdn_base_url', $event)" placeholder="https://cdn.example.com (optional)" />
            </UFormField>
            <UFormField label="Pre-Generate Interval (hours)">
              <UInput type="number" :model-value="get('hls', 'pre_generate_interval_hours')" @update:model-value="set('hls', 'pre_generate_interval_hours', Number($event))" />
            </UFormField>
            <UFormField label="Playlist Length (segments)">
              <UInput type="number" :model-value="get('hls', 'playlist_length')" @update:model-value="set('hls', 'playlist_length', Number($event))" />
            </UFormField>
            <UFormField label="Max Consecutive Failures">
              <UInput type="number" :model-value="get('hls', 'max_consecutive_failures')" @update:model-value="set('hls', 'max_consecutive_failures', Number($event))" />
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
            <UFormField label="Retention (days)">
              <UInput type="number" :model-value="get('analytics', 'retention_days')" @update:model-value="set('analytics', 'retention_days', Number($event))" />
            </UFormField>
            <UFormField label="Max Reconstruct Events (startup)">
              <UInput type="number" :model-value="get('analytics', 'max_reconstruct_events')" @update:model-value="set('analytics', 'max_reconstruct_events', Number($event))" />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Session timeout, view cooldown and cleanup interval are stored as durations (ns) — edit via raw JSON if needed.</p>
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
          <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mt-4 max-w-4xl">
            <UFormField label="High Confidence Keywords (one per line)">
              <UTextarea
                :model-value="(get('mature_scanner', 'high_confidence_keywords') || []).join('\n')"
                @update:model-value="set('mature_scanner', 'high_confidence_keywords', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                :rows="6"
                placeholder="One keyword per line"
              />
            </UFormField>
            <UFormField label="Medium Confidence Keywords (one per line)">
              <UTextarea
                :model-value="(get('mature_scanner', 'medium_confidence_keywords') || []).join('\n')"
                @update:model-value="set('mature_scanner', 'medium_confidence_keywords', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                :rows="6"
                placeholder="One keyword per line"
              />
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
            <UFormField label="Endpoint URL">
              <UInput :model-value="get('huggingface', 'endpoint_url')" @update:model-value="set('huggingface', 'endpoint_url', $event)" placeholder="https://router.huggingface.co (optional)" />
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

        <!-- ── Age Gate ────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-shield-check" class="text-primary" />
              <span class="font-semibold text-sm">Age Gate</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('age_gate', 'enabled')" @update:model-value="set('age_gate', 'enabled', $event)" />
            </div>
            <UFormField label="Cookie Name">
              <UInput :model-value="get('age_gate', 'cookie_name')" @update:model-value="set('age_gate', 'cookie_name', $event)" placeholder="age_verified" />
            </UFormField>
            <UFormField label="Cookie Max Age (s)">
              <UInput type="number" :model-value="get('age_gate', 'cookie_max_age')" @update:model-value="set('age_gate', 'cookie_max_age', Number($event))" />
            </UFormField>
            <UFormField label="Bypass IPs (one per line)" class="sm:col-span-2 lg:col-span-3">
              <UTextarea
                :model-value="(get('age_gate', 'bypass_ips') || []).join('\n')"
                @update:model-value="set('age_gate', 'bypass_ips', String($event).split('\n').map(s => s.trim()).filter(Boolean))"
                placeholder="203.0.113.5&#10;198.51.100.0/24"
                :rows="3"
              />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">IPs listed here bypass the age verification check. CIDR ranges supported. Verification TTL is set in config.json or .env.</p>
        </UCard>

        <!-- ── Cookie Consent (GDPR/CCPA banner) ──────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-cookie" class="text-primary" />
              <span class="font-semibold text-sm">Cookie Consent</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Banner Enabled</span>
              <USwitch :model-value="get('cookie_consent', 'enabled')" @update:model-value="set('cookie_consent', 'enabled', $event)" />
            </div>
            <UFormField label="Cookie Name">
              <UInput :model-value="get('cookie_consent', 'cookie_name')" @update:model-value="set('cookie_consent', 'cookie_name', $event)" placeholder="cookie_consent" />
            </UFormField>
            <UFormField label="Cookie Max Age (s)">
              <UInput type="number" :model-value="get('cookie_consent', 'cookie_max_age')" @update:model-value="set('cookie_consent', 'cookie_max_age', Number($event))" />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Controls the visitor consent banner for analytics cookies. Disable only when running purely server-side without analytics tracking.</p>
        </UCard>

        <!-- ── Remote Media ───────────────────────────────────────── -->
        <UCard v-if="get('features', 'enable_remote_media')">
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-globe" class="text-primary" />
              <span class="font-semibold text-sm">Remote Media</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('remote_media', 'enabled')" @update:model-value="set('remote_media', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Cache Enabled</span>
              <USwitch :model-value="get('remote_media', 'cache_enabled')" @update:model-value="set('remote_media', 'cache_enabled', $event)" />
            </div>
            <UFormField label="Cache Size (bytes)">
              <UInput type="number" :model-value="get('remote_media', 'cache_size')" @update:model-value="set('remote_media', 'cache_size', Number($event))" />
            </UFormField>
            <UFormField label="Max Concurrent Downloads">
              <UInput type="number" :model-value="get('remote_media', 'max_concurrent_downloads')" @update:model-value="set('remote_media', 'max_concurrent_downloads', Number($event))" />
            </UFormField>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Sync interval, cache TTL and HTTP timeout are durations (ns) — edit via raw JSON if needed.</p>
        </UCard>

        <!-- ── Crawler ────────────────────────────────────────────── -->
        <UCard v-if="get('features', 'enable_crawler')">
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-bug" class="text-primary" />
              <span class="font-semibold text-sm">Crawler</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('crawler', 'enabled')" @update:model-value="set('crawler', 'enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Browser Enabled</span>
              <USwitch :model-value="get('crawler', 'browser_enabled')" @update:model-value="set('crawler', 'browser_enabled', $event)" />
            </div>
            <UFormField label="Max Pages">
              <UInput type="number" :model-value="get('crawler', 'max_pages')" @update:model-value="set('crawler', 'max_pages', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Extractor ──────────────────────────────────────────── -->
        <UCard v-if="get('features', 'enable_extractor')">
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-link" class="text-primary" />
              <span class="font-semibold text-sm">Extractor</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('extractor', 'enabled')" @update:model-value="set('extractor', 'enabled', $event)" />
            </div>
            <UFormField label="Max Items">
              <UInput type="number" :model-value="get('extractor', 'max_items')" @update:model-value="set('extractor', 'max_items', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Backup ─────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-archive" class="text-primary" />
              <span class="font-semibold text-sm">Backup</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
            <UFormField label="Config Backup Retention Count">
              <UInput type="number" :model-value="get('backup', 'retention_count')" @update:model-value="set('backup', 'retention_count', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── UI Defaults ────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-layout-grid" class="text-primary" />
              <span class="font-semibold text-sm">UI Defaults</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-3xl">
            <UFormField label="Items per Page (Desktop)">
              <UInput type="number" :model-value="get('ui', 'items_per_page')" @update:model-value="set('ui', 'items_per_page', Number($event))" />
            </UFormField>
            <UFormField label="Items per Page (Mobile)">
              <UInput type="number" :model-value="get('ui', 'mobile_items_per_page')" @update:model-value="set('ui', 'mobile_items_per_page', Number($event))" />
            </UFormField>
            <UFormField label="Mobile Grid Columns">
              <UInput type="number" :model-value="get('ui', 'mobile_grid_columns')" @update:model-value="set('ui', 'mobile_grid_columns', Number($event))" />
            </UFormField>
            <UFormField label="Feed Default Items (RSS/Atom)">
              <UInput type="number" :model-value="get('ui', 'feed_default_items')" @update:model-value="set('ui', 'feed_default_items', Number($event))" />
            </UFormField>
            <UFormField label="Feed Max Items (hard cap)">
              <UInput type="number" :model-value="get('ui', 'feed_max_items')" @update:model-value="set('ui', 'feed_max_items', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Admin Panel ────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-lock" class="text-primary" />
              <span class="font-semibold text-sm">Admin Panel</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
            <UFormField label="Max Query Rows">
              <UInput type="number" :model-value="get('admin', 'max_query_rows')" @update:model-value="set('admin', 'max_query_rows', Number($event))" />
            </UFormField>
            <UFormField label="Audit Log Retention (days)" help="0 disables retention (keep forever). Negative also disables. Default 90.">
              <UInput type="number" :model-value="get('admin', 'audit_log_retention_days')" @update:model-value="set('admin', 'audit_log_retention_days', Number($event))" />
            </UFormField>
          </div>
        </UCard>

        <!-- ── Logging ────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-file-text" class="text-primary" />
              <span class="font-semibold text-sm">Logging</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl">
            <UFormField label="Log Level">
              <USelect :model-value="get('logging', 'level') || 'info'" :items="[{label:'Debug',value:'debug'},{label:'Info',value:'info'},{label:'Warn',value:'warn'},{label:'Error',value:'error'}]" @update:model-value="set('logging', 'level', $event)" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">File Logging</span>
              <USwitch :model-value="get('logging', 'file_enabled')" @update:model-value="set('logging', 'file_enabled', $event)" />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Log Rotation</span>
              <USwitch :model-value="get('logging', 'file_rotation')" @update:model-value="set('logging', 'file_rotation', $event)" />
            </div>
            <UFormField label="Max Backups">
              <UInput type="number" :model-value="get('logging', 'max_backups')" @update:model-value="set('logging', 'max_backups', Number($event))" />
            </UFormField>
            <UFormField label="Format">
              <USelect
                :model-value="get('logging', 'format') || 'text'"
                :items="[{label:'Text',value:'text'},{label:'JSON',value:'json'}]"
                @update:model-value="set('logging', 'format', $event)"
              />
            </UFormField>
            <UFormField label="Max File Size (bytes)">
              <UInput type="number" :model-value="get('logging', 'max_file_size')" @update:model-value="set('logging', 'max_file_size', Number($event))" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">Color Enabled</span>
              <USwitch :model-value="get('logging', 'color_enabled')" @update:model-value="set('logging', 'color_enabled', $event)" />
            </div>
          </div>
        </UCard>

        <!-- ── Downloader ─────────────────────────────────────────── -->
        <UCard v-if="get('features', 'enable_downloader')">
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-download" class="text-primary" />
              <span class="font-semibold text-sm">Downloader</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
            <UFormField label="Service URL">
              <UInput :model-value="get('downloader', 'url')" @update:model-value="set('downloader', 'url', $event)" placeholder="http://localhost:3000" />
            </UFormField>
            <UFormField label="Downloads Directory">
              <UInput :model-value="get('downloader', 'downloads_dir')" @update:model-value="set('downloader', 'downloads_dir', $event)" placeholder="/path/to/downloads" />
            </UFormField>
            <UFormField label="Import Directory">
              <UInput :model-value="get('downloader', 'import_dir')" @update:model-value="set('downloader', 'import_dir', $event)" placeholder="/path/to/import" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">Enabled</span>
              <USwitch :model-value="get('downloader', 'enabled')" @update:model-value="set('downloader', 'enabled', $event)" />
            </div>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Health-check interval and request timeout are durations (ns) — edit via raw JSON if needed.</p>
        </UCard>

        <!-- ── Updater ─────────────────────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-refresh-cw" class="text-primary" />
              <span class="font-semibold text-sm">Updater</span>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
            <UFormField label="Update Method">
              <USelect :model-value="get('updater', 'update_method') || 'source'" :items="[{label:'Source (git pull)',value:'source'},{label:'Binary',value:'binary'}]" @update:model-value="set('updater', 'update_method', $event)" />
            </UFormField>
            <UFormField label="Branch">
              <UInput :model-value="get('updater', 'branch')" @update:model-value="set('updater', 'branch', $event)" placeholder="main" />
            </UFormField>
            <UFormField label="Application Directory">
              <UInput :model-value="get('updater', 'app_dir')" @update:model-value="set('updater', 'app_dir', $event)" placeholder="/opt/media-server-pro" />
            </UFormField>
            <UFormField label="GitHub Username">
              <UInput :model-value="get('updater', 'github_username')" @update:model-value="set('updater', 'github_username', $event)" />
            </UFormField>
            <div class="flex items-center justify-between">
              <span class="text-sm">GitHub Token</span>
              <UBadge :color="get('updater', 'github_token_set') ? 'success' : 'neutral'" variant="subtle" size="sm">
                {{ get('updater', 'github_token_set') ? 'Configured' : 'Not set' }}
              </UBadge>
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Deploy Key</span>
              <UBadge :color="get('updater', 'deploy_key_path_set') ? 'success' : 'neutral'" variant="subtle" size="sm">
                {{ get('updater', 'deploy_key_path_set') ? 'Configured' : 'Not set' }}
              </UBadge>
            </div>
          </div>
          <p class="text-xs text-neutral-500 mt-3">GitHub tokens and deploy-key paths can be set via .env (GITHUB_TOKEN / DEPLOY_KEY_PATH) for security. Their presence is shown here as a status badge.</p>
        </UCard>

        <!-- ── Directories (read-only) ────────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-folder" class="text-primary" />
              <span class="font-semibold text-sm">Directories</span>
              <UBadge variant="subtle" color="neutral" size="xs">Read-only</UBadge>
            </div>
          </template>
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 max-w-4xl">
            <div v-for="[label, key] in [
              ['Videos', 'videos'], ['Music', 'music'], ['Thumbnails', 'thumbnails'],
              ['Playlists', 'playlists'], ['Uploads', 'uploads'], ['HLS Cache', 'hls_cache'],
              ['Data', 'data'], ['Logs', 'logs'], ['Temp', 'temp'],
            ]" :key="key" class="text-sm">
              <span class="text-neutral-400">{{ label }}:</span>
              <span class="font-mono text-xs ml-1">{{ get('directories', key) || '—' }}</span>
            </div>
          </div>
          <p class="text-xs text-neutral-500 mt-3">Directory paths can only be changed via environment variables or config file.</p>
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

        <!-- ── Locked Sections (denylist) ─────────────────────────── -->
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-lock" class="text-warning" />
              <span class="font-semibold text-sm">Environment-Only Settings</span>
            </div>
          </template>
          <p class="text-sm text-neutral-400 mb-3">
            The following settings cannot be changed at runtime for security reasons. They must be configured via environment variables or by editing config.json directly.
          </p>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
            <div>
              <p class="font-medium text-highlighted mb-1">Auth</p>
              <ul class="text-xs text-neutral-400 space-y-0.5 list-disc pl-4">
                <li>Session timeout</li>
                <li>Secure cookies</li>
                <li>Login attempts / lockout</li>
                <li>Registration / guests</li>
                <li>User type definitions</li>
              </ul>
            </div>
            <div>
              <p class="font-medium text-highlighted mb-1">Database</p>
              <ul class="text-xs text-neutral-400 space-y-0.5 list-disc pl-4">
                <li>Host / port / name</li>
                <li>Credentials</li>
                <li>Connection pool settings</li>
                <li>TLS mode</li>
              </ul>
            </div>
            <div>
              <p class="font-medium text-highlighted mb-1">Receiver</p>
              <ul class="text-xs text-neutral-400 space-y-0.5 list-disc pl-4">
                <li>API keys</li>
                <li>Proxy timeout</li>
                <li>Health check interval</li>
                <li>Connection limits</li>
              </ul>
            </div>
          </div>
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
            <PasswordInput v-model="pwCurrent" autocomplete="current-password" />
          </UFormField>
          <UFormField label="New Password">
            <PasswordInput v-model="pwNew" autocomplete="new-password" :minlength="8" />
          </UFormField>
          <UFormField label="Confirm New Password">
            <PasswordInput v-model="pwConfirm" autocomplete="new-password" />
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
