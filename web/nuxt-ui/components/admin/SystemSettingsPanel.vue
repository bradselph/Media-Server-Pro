<script setup lang="ts">
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const toast = useToast()

// Full config state
const fullConfig = ref<Record<string, unknown>>({})
const configLoading = ref(false)
const configSaving = ref(false)

// Section tab
const activeSection = ref('streaming')
const sections = [
  { label: 'Streaming', value: 'streaming', icon: 'i-lucide-play' },
  { label: 'Downloads', value: 'download', icon: 'i-lucide-download' },
  { label: 'Uploads', value: 'uploads', icon: 'i-lucide-upload' },
  { label: 'Thumbnails', value: 'thumbnails', icon: 'i-lucide-image' },
  { label: 'Scanner', value: 'mature_scanner', icon: 'i-lucide-scan-eye' },
  { label: 'Features', value: 'features', icon: 'i-lucide-toggle-right' },
  { label: 'Advanced', value: 'advanced', icon: 'i-lucide-code' },
]

// Raw JSON fallback
const configText = ref('')

async function loadConfig() {
  configLoading.value = true
  try {
    const cfg = await adminApi.getConfig()
    if (cfg) {
      fullConfig.value = cfg
      configText.value = JSON.stringify(cfg, null, 2)
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load config', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { configLoading.value = false }
}

// Generic section helper
function getSection(key: string): Record<string, unknown> {
  return asRecord(fullConfig.value[key]) ?? {}
}

function getSectionValue<T>(section: string, key: string, fallback: T): T {
  const sec = getSection(section)
  return (sec[key] as T) ?? fallback
}

function setSectionValue(section: string, key: string, value: unknown) {
  const sec = { ...getSection(section), [key]: value }
  fullConfig.value = { ...fullConfig.value, [section]: sec }
}

async function saveSection(section: string) {
  configSaving.value = true
  try {
    await adminApi.updateConfig(fullConfig.value)
    configText.value = JSON.stringify(fullConfig.value, null, 2)
    toast.add({ title: 'Settings saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
  } finally { configSaving.value = false }
}

async function saveRawConfig() {
  configSaving.value = true
  try {
    const parsed = JSON.parse(configText.value)
    await adminApi.updateConfig(parsed)
    fullConfig.value = parsed
    toast.add({ title: 'Config saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { configSaving.value = false }
}

// Password
const pwCurrent = ref('')
const pwNew = ref('')
const pwConfirm = ref('')
const pwLoading = ref(false)

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

// Features section helpers
const featureFlags = computed(() => {
  const sec = getSection('features')
  return [
    { key: 'enable_hls', label: 'HLS Streaming', desc: 'Adaptive bitrate streaming via HLS' },
    { key: 'enable_analytics', label: 'Analytics', desc: 'Track views, playback, and user activity' },
    { key: 'enable_playlists', label: 'Playlists', desc: 'User playlist creation and management' },
    { key: 'enable_uploads', label: 'Uploads', desc: 'Allow users to upload media files' },
    { key: 'enable_thumbnails', label: 'Thumbnails', desc: 'Automatic thumbnail generation' },
    { key: 'enable_mature_scanner', label: 'Mature Scanner', desc: 'Content maturity scanning and auto-flagging' },
    { key: 'enable_remote_media', label: 'Remote Media', desc: 'Connect to remote media sources' },
    { key: 'enable_user_auth', label: 'User Auth', desc: 'User registration and authentication' },
    { key: 'enable_admin_panel', label: 'Admin Panel', desc: 'Administrative dashboard' },
    { key: 'enable_suggestions', label: 'Suggestions', desc: 'Personalized content recommendations' },
    { key: 'enable_auto_discovery', label: 'Auto Discovery', desc: 'Automatic media file discovery' },
    { key: 'enable_receiver', label: 'Receiver', desc: 'Master-slave media receiver protocol' },
    { key: 'enable_extractor', label: 'Extractor', desc: 'Stream extraction and proxy' },
    { key: 'enable_crawler', label: 'Crawler', desc: 'Web crawling for media streams' },
    { key: 'enable_duplicate_detection', label: 'Duplicate Detection', desc: 'Fingerprint-based duplicate media detection' },
    { key: 'enable_huggingface', label: 'HuggingFace AI', desc: 'Visual content classification via HuggingFace' },
    { key: 'enable_downloader', label: 'Downloader', desc: 'External media downloader integration' },
  ].map(f => ({ ...f, value: sec[f.key] === true }))
})

onMounted(loadConfig)
</script>

<template>
  <div class="space-y-6">
    <UCard>
      <template #header><div class="font-semibold">Server Configuration</div></template>
      <div v-if="configLoading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
      </div>
      <template v-else>
        <UTabs v-model="activeSection" :items="sections" size="sm">
          <template #content="{ item }">
            <div class="pt-4">

              <!-- Streaming -->
              <div v-if="item.value === 'streaming'" class="space-y-4">
                <div class="divide-y divide-default">
                  <div class="flex items-center justify-between gap-4 py-3 first:pt-0">
                    <div>
                      <p class="font-medium text-sm">Require Authentication</p>
                      <p class="text-xs text-muted mt-0.5">Reject unauthenticated streaming requests</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('streaming', 'require_auth', false)"
                      @update:model-value="v => setSectionValue('streaming', 'require_auth', v)"
                    />
                  </div>
                  <div class="py-3">
                    <UFormField label="Unauth Stream Limit" description="Max concurrent streams per IP when unauthenticated (0 = unlimited)">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('streaming', 'unauth_stream_limit', 0)"
                        @update:model-value="v => setSectionValue('streaming', 'unauth_stream_limit', Number(v))"
                        class="w-32"
                      />
                    </UFormField>
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Adaptive Streaming</p>
                      <p class="text-xs text-muted mt-0.5">Adjust chunk sizes based on client bandwidth</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('streaming', 'adaptive', false)"
                      @update:model-value="v => setSectionValue('streaming', 'adaptive', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Mobile Optimization</p>
                      <p class="text-xs text-muted mt-0.5">Use smaller chunk sizes for mobile devices</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('streaming', 'mobile_optimization', false)"
                      @update:model-value="v => setSectionValue('streaming', 'mobile_optimization', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3 last:pb-0">
                    <div>
                      <p class="font-medium text-sm">Keep-Alive</p>
                      <p class="text-xs text-muted mt-0.5">Maintain persistent connections for streaming</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('streaming', 'keep_alive_enabled', true)"
                      @update:model-value="v => setSectionValue('streaming', 'keep_alive_enabled', v)"
                    />
                  </div>
                </div>
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Streaming Settings" @click="saveSection('streaming')" />
              </div>

              <!-- Downloads -->
              <div v-if="item.value === 'download'" class="space-y-4">
                <div class="divide-y divide-default">
                  <div class="flex items-center justify-between gap-4 py-3 first:pt-0">
                    <div>
                      <p class="font-medium text-sm">Enable Downloads</p>
                      <p class="text-xs text-muted mt-0.5">Allow users to download media files</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('download', 'enabled', true)"
                      @update:model-value="v => setSectionValue('download', 'enabled', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Require Authentication</p>
                      <p class="text-xs text-muted mt-0.5">Only authenticated users can download files</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('download', 'require_auth', false)"
                      @update:model-value="v => setSectionValue('download', 'require_auth', v)"
                    />
                  </div>
                  <div class="py-3 last:pb-0">
                    <UFormField label="Chunk Size (KB)" description="Download chunk size in kilobytes">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('download', 'chunk_size_kb', 1024)"
                        @update:model-value="v => setSectionValue('download', 'chunk_size_kb', Number(v))"
                        class="w-32"
                      />
                    </UFormField>
                  </div>
                </div>
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Download Settings" @click="saveSection('download')" />
              </div>

              <!-- Uploads -->
              <div v-if="item.value === 'uploads'" class="space-y-4">
                <div class="divide-y divide-default">
                  <div class="flex items-center justify-between gap-4 py-3 first:pt-0">
                    <div>
                      <p class="font-medium text-sm">Enable Uploads</p>
                      <p class="text-xs text-muted mt-0.5">Allow media file uploads</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('uploads', 'enabled', false)"
                      @update:model-value="v => setSectionValue('uploads', 'enabled', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Require Authentication</p>
                      <p class="text-xs text-muted mt-0.5">Only authenticated users can upload</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('uploads', 'require_auth', true)"
                      @update:model-value="v => setSectionValue('uploads', 'require_auth', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Scan for Mature Content</p>
                      <p class="text-xs text-muted mt-0.5">Automatically scan uploads for mature content</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('uploads', 'scan_for_mature', false)"
                      @update:model-value="v => setSectionValue('uploads', 'scan_for_mature', v)"
                    />
                  </div>
                  <div class="py-3 last:pb-0">
                    <UFormField label="Max File Size (bytes)" description="Maximum allowed upload file size">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('uploads', 'max_file_size', 0)"
                        @update:model-value="v => setSectionValue('uploads', 'max_file_size', Number(v))"
                        class="w-48"
                      />
                    </UFormField>
                  </div>
                </div>
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Upload Settings" @click="saveSection('uploads')" />
              </div>

              <!-- Thumbnails -->
              <div v-if="item.value === 'thumbnails'" class="space-y-4">
                <div class="divide-y divide-default">
                  <div class="flex items-center justify-between gap-4 py-3 first:pt-0">
                    <div>
                      <p class="font-medium text-sm">Enable Thumbnails</p>
                      <p class="text-xs text-muted mt-0.5">Enable thumbnail generation system</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('thumbnails', 'enabled', true)"
                      @update:model-value="v => setSectionValue('thumbnails', 'enabled', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Auto Generate</p>
                      <p class="text-xs text-muted mt-0.5">Automatically generate thumbnails for new media</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('thumbnails', 'auto_generate', true)"
                      @update:model-value="v => setSectionValue('thumbnails', 'auto_generate', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Generate on Access</p>
                      <p class="text-xs text-muted mt-0.5">Generate thumbnails when first accessed by a user</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('thumbnails', 'generate_on_access', false)"
                      @update:model-value="v => setSectionValue('thumbnails', 'generate_on_access', v)"
                    />
                  </div>
                  <div class="grid grid-cols-2 gap-4 py-3">
                    <UFormField label="Width">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('thumbnails', 'width', 320)"
                        @update:model-value="v => setSectionValue('thumbnails', 'width', Number(v))"
                      />
                    </UFormField>
                    <UFormField label="Height">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('thumbnails', 'height', 180)"
                        @update:model-value="v => setSectionValue('thumbnails', 'height', Number(v))"
                      />
                    </UFormField>
                  </div>
                  <div class="grid grid-cols-2 gap-4 py-3 last:pb-0">
                    <UFormField label="Quality" description="JPEG quality (1-100)">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('thumbnails', 'quality', 80)"
                        @update:model-value="v => setSectionValue('thumbnails', 'quality', Number(v))"
                      />
                    </UFormField>
                    <UFormField label="Worker Count" description="Concurrent generation workers">
                      <UInput
                        type="number"
                        :model-value="getSectionValue('thumbnails', 'worker_count', 2)"
                        @update:model-value="v => setSectionValue('thumbnails', 'worker_count', Number(v))"
                      />
                    </UFormField>
                  </div>
                </div>
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Thumbnail Settings" @click="saveSection('thumbnails')" />
              </div>

              <!-- Scanner -->
              <div v-if="item.value === 'mature_scanner'" class="space-y-4">
                <div class="divide-y divide-default">
                  <div class="flex items-center justify-between gap-4 py-3 first:pt-0">
                    <div>
                      <p class="font-medium text-sm">Enable Scanner</p>
                      <p class="text-xs text-muted mt-0.5">Enable mature content scanning</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('mature_scanner', 'enabled', false)"
                      @update:model-value="v => setSectionValue('mature_scanner', 'enabled', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Auto Flag</p>
                      <p class="text-xs text-muted mt-0.5">Automatically flag content above confidence thresholds</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('mature_scanner', 'auto_flag', false)"
                      @update:model-value="v => setSectionValue('mature_scanner', 'auto_flag', v)"
                    />
                  </div>
                  <div class="flex items-center justify-between gap-4 py-3">
                    <div>
                      <p class="font-medium text-sm">Require Review</p>
                      <p class="text-xs text-muted mt-0.5">Flagged content must be reviewed by an admin before being hidden</p>
                    </div>
                    <USwitch
                      :model-value="getSectionValue('mature_scanner', 'require_review', true)"
                      @update:model-value="v => setSectionValue('mature_scanner', 'require_review', v)"
                    />
                  </div>
                  <div class="grid grid-cols-2 gap-4 py-3 last:pb-0">
                    <UFormField label="High Confidence Threshold" description="Score above this is flagged as mature (0.0–1.0)">
                      <UInput
                        type="number"
                        step="0.05"
                        min="0"
                        max="1"
                        :model-value="getSectionValue('mature_scanner', 'high_confidence_threshold', 0.8)"
                        @update:model-value="v => setSectionValue('mature_scanner', 'high_confidence_threshold', Number(v))"
                      />
                    </UFormField>
                    <UFormField label="Medium Confidence Threshold" description="Score above this triggers review (0.0–1.0)">
                      <UInput
                        type="number"
                        step="0.05"
                        min="0"
                        max="1"
                        :model-value="getSectionValue('mature_scanner', 'medium_confidence_threshold', 0.5)"
                        @update:model-value="v => setSectionValue('mature_scanner', 'medium_confidence_threshold', Number(v))"
                      />
                    </UFormField>
                  </div>
                </div>
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Scanner Settings" @click="saveSection('mature_scanner')" />
              </div>

              <!-- Features -->
              <div v-if="item.value === 'features'" class="space-y-4">
                <p class="text-sm text-muted">Enable or disable major feature modules. Disabling a feature hides its UI and stops its background processes.</p>
                <div class="divide-y divide-default">
                  <div
                    v-for="flag in featureFlags"
                    :key="flag.key"
                    class="flex items-center justify-between gap-4 py-3 first:pt-0 last:pb-0"
                  >
                    <div>
                      <p class="font-medium text-sm">{{ flag.label }}</p>
                      <p class="text-xs text-muted mt-0.5">{{ flag.desc }}</p>
                    </div>
                    <USwitch
                      :model-value="flag.value"
                      @update:model-value="v => setSectionValue('features', flag.key, v)"
                    />
                  </div>
                </div>
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Feature Flags" @click="saveSection('features')" />
              </div>

              <!-- Advanced (raw JSON) -->
              <div v-if="item.value === 'advanced'" class="space-y-3">
                <p class="text-sm text-muted">Edit the full server configuration as JSON. Be careful — invalid values may cause issues.</p>
                <UTextarea v-model="configText" :rows="20" class="font-mono text-xs" />
                <UButton :loading="configSaving" icon="i-lucide-save" label="Save Config" @click="saveRawConfig" />
              </div>

            </div>
          </template>
        </UTabs>
      </template>
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
