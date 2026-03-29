<script setup lang="ts">
import type { ReviewQueueItem, HLSJob, HLSValidationResult, ScannerStats, HLSStats, ValidatorStats, HLSCapabilities } from '~/types/api'
import { formatBytes } from '~/utils/format'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const hlsApi = useHlsApi()
const toast = useToast()

const subTab = ref('scanner')
const subTabs = [
  { label: 'Scanner', value: 'scanner', icon: 'i-lucide-scan' },
  { label: 'HLS Jobs', value: 'hls', icon: 'i-lucide-video' },
  { label: 'Validator', value: 'validator', icon: 'i-lucide-shield-check' },
]

// ── Scanner ────────────────────────────────────────────────────────────────────
const scannerStats = ref<ScannerStats | null>(null)
const reviewQueue = ref<ReviewQueueItem[]>([])
const scannerLoading = ref(false)
const scanPath = ref('')
const scanning = ref(false)
const selected = ref<string[]>([])

async function loadScanner() {
  scannerLoading.value = true
  try {
    const [stats, queue] = await Promise.all([
      adminApi.getScannerStats(),
      adminApi.getReviewQueue(),
    ])
    scannerStats.value = stats
    reviewQueue.value = queue ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load scanner', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { scannerLoading.value = false }
}

async function startScan() {
  scanning.value = true
  try {
    await adminApi.runScan(scanPath.value || undefined)
    toast.add({ title: 'Scan started', color: 'success', icon: 'i-lucide-check' })
    setTimeout(loadScanner, 2000)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Scan failed', color: 'error', icon: 'i-lucide-x' })
  } finally { scanning.value = false }
}

async function batchAction(action: 'approve' | 'reject') {
  if (selected.value.length === 0) return
  try {
    const res = await adminApi.batchReview(action, selected.value)
    toast.add({ title: `${action === 'approve' ? 'Approved' : 'Rejected'} ${res.updated} item(s)`, color: 'success', icon: 'i-lucide-check' })
    selected.value = []
    loadScanner()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function clearQueue() {
  try {
    await adminApi.clearReviewQueue()
    reviewQueue.value = []
    toast.add({ title: 'Review queue cleared', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

function toggleSelect(id: string) {
  const i = selected.value.indexOf(id)
  if (i === -1) selected.value.push(id)
  else selected.value.splice(i, 1)
}

function toggleAll() {
  if (selected.value.length === reviewQueue.value.length) selected.value = []
  else selected.value = reviewQueue.value.map(r => r.id)
}

// ── HLS ────────────────────────────────────────────────────────────────────────
const hlsStats = ref<HLSStats | null>(null)
const hlsJobs = ref<HLSJob[]>([])
const hlsCaps = ref<HLSCapabilities | null>(null)
const hlsLoading = ref(false)

async function loadHLS() {
  hlsLoading.value = true
  try {
    const [stats, jobs, caps] = await Promise.allSettled([
      adminApi.getHLSStats(),
      adminApi.listHLSJobs(),
      hlsApi.getCapabilities(),
    ])
    if (stats.status === 'fulfilled') hlsStats.value = stats.value
    if (jobs.status === 'fulfilled') hlsJobs.value = jobs.value ?? []
    if (caps.status === 'fulfilled') hlsCaps.value = caps.value
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load HLS', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { hlsLoading.value = false }
}

async function deleteHLSJob(id: string) {
  try {
    await adminApi.deleteHLSJob(id)
    hlsJobs.value = hlsJobs.value.filter(j => j.id !== id)
    toast.add({ title: 'HLS job deleted', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

const hlsValidating = ref<string | null>(null)
const hlsValidationResult = ref<HLSValidationResult | null>(null)
const hlsRefreshing = ref<string | null>(null)

async function refreshJobStatus(id: string) {
  hlsRefreshing.value = id
  try {
    const updated = await hlsApi.getStatus(id)
    const idx = hlsJobs.value.findIndex(j => j.id === id)
    if (idx !== -1) hlsJobs.value[idx] = updated
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to refresh status', color: 'error', icon: 'i-lucide-x' })
  } finally { hlsRefreshing.value = null }
}

async function validateHLSJob(id: string) {
  hlsValidating.value = id
  hlsValidationResult.value = null
  try {
    hlsValidationResult.value = await adminApi.validateHLS(id)
    const ok = hlsValidationResult.value.valid
    toast.add({ title: ok ? 'HLS output is valid' : 'HLS validation failed', color: ok ? 'success' : 'warning', icon: ok ? 'i-lucide-check' : 'i-lucide-alert-triangle' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Validation failed', color: 'error', icon: 'i-lucide-x' })
  } finally { hlsValidating.value = null }
}

async function cleanInactiveLocks() {
  try {
    await adminApi.cleanHLSStaleLocks()
    toast.add({ title: 'Stale locks cleaned', color: 'success', icon: 'i-lucide-check' })
    loadHLS()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function cleanInactiveJobs() {
  try {
    await adminApi.cleanHLSInactive()
    toast.add({ title: 'Inactive HLS jobs cleaned', color: 'success', icon: 'i-lucide-check' })
    loadHLS()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}


// ── Validator ──────────────────────────────────────────────────────────────────
const validatorStats = ref<ValidatorStats | null>(null)
const validatorLoading = ref(false)
const validateId = ref('')
const validating = ref(false)
const validateResult = ref<unknown>(null)

async function loadValidator() {
  validatorLoading.value = true
  try { validatorStats.value = await adminApi.getValidatorStats() }
  catch {}
  finally { validatorLoading.value = false }
}

async function runValidate() {
  if (!validateId.value.trim()) return
  validating.value = true
  try {
    validateResult.value = await adminApi.validateMedia(validateId.value.trim())
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Validation failed', color: 'error', icon: 'i-lucide-x' })
  } finally { validating.value = false }
}

async function runFix() {
  if (!validateId.value.trim()) return
  validating.value = true
  try {
    validateResult.value = await adminApi.fixMedia(validateId.value.trim())
    toast.add({ title: 'Media fixed', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Fix failed', color: 'error', icon: 'i-lucide-x' })
  } finally { validating.value = false }
}

// ── Scanner confidence thresholds ──────────────────────────────────────────────
const scannerFullConfig = ref<Record<string, unknown>>({})
const highConfidenceThreshold = ref(0.85)
const mediumConfidenceThreshold = ref(0.65)
const scannerConfigSaving = ref(false)

async function loadScannerConfig() {
  try {
    const cfg = await adminApi.getConfig()
    if (cfg) {
      scannerFullConfig.value = cfg
      const ms = asRecord(cfg.mature_scanner)
      highConfidenceThreshold.value = typeof ms?.high_confidence_threshold === 'number' ? ms.high_confidence_threshold : 0.85
      mediumConfidenceThreshold.value = typeof ms?.medium_confidence_threshold === 'number' ? ms.medium_confidence_threshold : 0.65
    }
  } catch { /* non-critical */ }
}

async function saveScannerThresholds() {
  scannerConfigSaving.value = true
  try {
    const updated = {
      ...scannerFullConfig.value,
      mature_scanner: {
        ...asRecord(scannerFullConfig.value.mature_scanner),
        high_confidence_threshold: highConfidenceThreshold.value,
        medium_confidence_threshold: mediumConfidenceThreshold.value,
      },
    }
    await adminApi.updateConfig(updated)
    scannerFullConfig.value = updated
    toast.add({ title: 'Scanner thresholds saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
  } finally {
    scannerConfigSaving.value = false
  }
}

watch(subTab, (v) => {
  if (v === 'scanner') { loadScanner(); loadScannerConfig() }
  else if (v === 'hls') loadHLS()
  else if (v === 'validator') loadValidator()
}, { immediate: true })
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm" />

    <!-- Scanner -->
    <div v-if="subTab === 'scanner'" class="space-y-4">
      <!-- Confidence thresholds config -->
      <UCard :ui="{ body: 'p-4' }">
        <p class="text-xs font-semibold text-muted mb-3 uppercase tracking-wide">Content Scanner Thresholds</p>
        <div class="flex flex-wrap items-end gap-4">
          <UFormField label="High confidence" description="Score ≥ this value → auto-flag as mature">
            <UInput
              v-model.number="highConfidenceThreshold"
              type="number"
              min="0"
              max="1"
              step="0.01"
              class="w-24"
            />
          </UFormField>
          <UFormField label="Medium confidence" description="Score ≥ this value → add to review queue">
            <UInput
              v-model.number="mediumConfidenceThreshold"
              type="number"
              min="0"
              max="1"
              step="0.01"
              class="w-24"
            />
          </UFormField>
          <UButton
            :loading="scannerConfigSaving"
            icon="i-lucide-save"
            label="Save"
            size="sm"
            @click="saveScannerThresholds"
          />
        </div>
      </UCard>

      <!-- Stats -->
      <div v-if="scannerStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <UCard v-for="item in [
          { label: 'Total Scanned', value: scannerStats.total_scanned },
          { label: 'Mature', value: scannerStats.mature_count },
          { label: 'Auto-Flagged', value: scannerStats.auto_flagged },
          { label: 'Pending Review', value: scannerStats.pending_review },
        ]" :key="item.label" :ui="{ body: 'p-3' }">
          <p class="text-xl font-bold text-highlighted">{{ (item.value ?? 0).toLocaleString() }}</p>
          <p class="text-xs text-muted">{{ item.label }}</p>
        </UCard>
      </div>

      <!-- Scan controls -->
      <UCard>
        <template #header><div class="font-semibold">Run Scan</div></template>
        <div class="flex flex-wrap gap-2">
          <UInput v-model="scanPath" placeholder="Path (optional, leave blank for full scan)" class="flex-1 min-w-48" />
          <UButton :loading="scanning" icon="i-lucide-scan" label="Scan" @click="startScan" />
        </div>
      </UCard>

      <!-- Review queue -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between flex-wrap gap-2">
            <div class="font-semibold">Review Queue ({{ reviewQueue.length }})</div>
            <div class="flex gap-2">
              <UButton v-if="selected.length > 0" icon="i-lucide-check" label="Approve Selected" size="xs" color="success" variant="outline" @click="batchAction('approve')" />
              <UButton v-if="selected.length > 0" icon="i-lucide-x" label="Reject Selected" size="xs" color="error" variant="outline" @click="batchAction('reject')" />
              <UButton v-if="reviewQueue.length > 0" icon="i-lucide-trash-2" aria-label="Clear review queue" size="xs" variant="ghost" color="error" @click="clearQueue" />
              <UButton icon="i-lucide-refresh-cw" aria-label="Refresh scanner" size="xs" variant="ghost" color="neutral" @click="loadScanner" />
            </div>
          </div>
        </template>
        <div v-if="scannerLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="reviewQueue.length === 0" class="text-center py-6 text-muted text-sm">No items pending review.</div>
        <div v-else class="divide-y divide-default text-sm">
          <div class="flex items-center gap-2 py-2 font-medium text-muted text-xs">
            <UCheckbox :model-value="selected.length === reviewQueue.length && reviewQueue.length > 0" @update:model-value="toggleAll" />
            <span class="flex-1">Path</span>
            <span class="w-20 text-right">Confidence</span>
            <span class="w-24 text-right">Actions</span>
          </div>
          <div v-for="item in reviewQueue" :key="item.id" class="flex items-center gap-2 py-2">
            <UCheckbox :model-value="selected.includes(item.id)" @update:model-value="toggleSelect(item.id)" />
            <div class="flex-1 min-w-0">
              <p class="truncate text-xs font-medium" :title="item.name">{{ item.name }}</p>
              <div class="flex gap-1 mt-0.5 flex-wrap">
                <UBadge v-for="r in (item.reasons ?? [])" :key="r" :label="r" color="neutral" variant="subtle" size="xs" />
              </div>
              <p v-if="item.detected_at" class="text-xs text-muted mt-0.5">Detected: {{ new Date(item.detected_at).toLocaleDateString() }}</p>
            </div>
            <span class="w-20 text-right text-muted">{{ item.confidence != null ? `${(item.confidence * 100).toFixed(0)}%` : '—' }}</span>
            <div class="w-24 flex justify-end gap-1">
              <UButton icon="i-lucide-check" aria-label="Approve" size="xs" variant="ghost" color="success" @click="adminApi.approveContent(item.id).then(loadScanner)" />
              <UButton icon="i-lucide-x" aria-label="Reject" size="xs" variant="ghost" color="error" @click="adminApi.rejectContent(item.id).then(loadScanner)" />
            </div>
          </div>
        </div>
      </UCard>
    </div>

    <!-- HLS Jobs -->
    <div v-if="subTab === 'hls'" class="space-y-4">
      <!-- Capabilities -->
      <UCard v-if="hlsCaps">
        <div class="flex flex-wrap items-center gap-4 text-sm">
          <div class="flex items-center gap-1.5">
            <UIcon
              :name="hlsCaps.healthy ? 'i-lucide-check-circle' : 'i-lucide-alert-triangle'"
              :class="hlsCaps.healthy ? 'text-success' : 'text-warning'"
              class="size-4"
            />
            <span class="font-medium">{{ hlsCaps.healthy ? 'HLS Ready' : 'HLS Unavailable' }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <UBadge :label="hlsCaps.ffmpeg_found ? 'ffmpeg ✓' : 'ffmpeg ✗'" :color="hlsCaps.ffmpeg_found ? 'success' : 'error'" variant="subtle" size="xs" />
            <UBadge :label="hlsCaps.ffprobe_found ? 'ffprobe ✓' : 'ffprobe ✗'" :color="hlsCaps.ffprobe_found ? 'success' : 'error'" variant="subtle" size="xs" />
          </div>
          <div v-if="hlsCaps.qualities.length" class="flex items-center gap-1 flex-wrap">
            <span class="text-muted">Qualities:</span>
            <UBadge v-for="q in hlsCaps.qualities" :key="q" :label="q" color="neutral" variant="subtle" size="xs" />
          </div>
          <span class="text-muted text-xs">Max concurrent: {{ hlsCaps.max_concurrent }}</span>
          <span v-if="hlsCaps.message" class="text-muted text-xs ml-auto">{{ hlsCaps.message }}</span>
        </div>
      </UCard>

      <!-- Stats -->
      <div v-if="hlsStats" class="grid grid-cols-2 sm:grid-cols-3 gap-3">
        <UCard v-for="item in [
          { label: 'Total Jobs', value: hlsStats.total_jobs },
          { label: 'Running', value: hlsStats.running_jobs },
          { label: 'Completed', value: hlsStats.completed_jobs },
          { label: 'Failed', value: hlsStats.failed_jobs },
          { label: 'Pending', value: hlsStats.pending_jobs },
          { label: 'Cache Size', value: formatBytes(hlsStats.cache_size_bytes) },
        ]" :key="item.label" :ui="{ body: 'p-3' }">
          <p class="text-xl font-bold text-highlighted">{{ item.value }}</p>
          <p class="text-xs text-muted">{{ item.label }}</p>
        </UCard>
      </div>

      <!-- Actions -->
      <div class="flex gap-2 flex-wrap">
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" size="sm" @click="loadHLS" />
        <UButton icon="i-lucide-lock-open" label="Clean Stale Locks" variant="outline" color="warning" size="sm" @click="cleanInactiveLocks" />
        <UButton icon="i-lucide-trash-2" label="Clean Inactive" variant="outline" color="error" size="sm" @click="cleanInactiveJobs" />
      </div>

      <!-- Jobs table -->
      <UCard>
        <div v-if="hlsLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="hlsJobs.length === 0" class="text-center py-6 text-muted text-sm">No HLS jobs.</div>
        <UTable
          v-else
          :data="hlsJobs"
          :columns="[
            { accessorKey: 'id', header: 'Media ID' },
            { accessorKey: 'status', header: 'Status' },
            { accessorKey: 'progress', header: 'Progress' },
            { accessorKey: 'started_at', header: 'Started' },
            { accessorKey: 'actions', header: '' },
          ]"
        >
          <template #id-cell="{ row }">
            <span class="font-mono text-xs">{{ row.original.id.slice(0, 8) }}…</span>
          </template>
          <template #status-cell="{ row }">
            <UBadge
              :label="row.original.status"
              :color="(({ completed: 'success', running: 'info', failed: 'error', pending: 'neutral', cancelled: 'neutral' } as Record<string, 'success'|'info'|'error'|'neutral'>)[row.original.status] ?? 'neutral')"
              variant="subtle"
              size="xs"
            />
          </template>
          <template #progress-cell="{ row }">
            <div class="flex items-center gap-2 w-24">
              <UProgress :value="row.original.progress" size="xs" class="flex-1" />
              <span class="text-xs text-muted w-8">{{ row.original.progress }}%</span>
            </div>
          </template>
          <template #started_at-cell="{ row }">
            <span class="text-xs text-muted">{{ row.original.started_at ? new Date(row.original.started_at).toLocaleString() : '—' }}</span>
          </template>
          <template #actions-cell="{ row }">
            <div class="flex gap-1">
              <UButton
                v-if="row.original.status === 'running' || row.original.status === 'pending'"
                icon="i-lucide-refresh-cw"
                aria-label="Refresh job status"
                size="xs"
                variant="ghost"
                color="neutral"
                :loading="hlsRefreshing === row.original.id"
                @click="refreshJobStatus(row.original.id)"
              />
              <UButton
                v-if="row.original.status === 'completed'"
                icon="i-lucide-shield-check"
                aria-label="Validate HLS output"
                size="xs"
                variant="ghost"
                color="neutral"
                :loading="hlsValidating === row.original.id"
                @click="validateHLSJob(row.original.id)"
              />
              <UButton icon="i-lucide-trash-2" aria-label="Delete HLS job" size="xs" variant="ghost" color="error" @click="deleteHLSJob(row.original.id)" />
            </div>
          </template>
        </UTable>
      </UCard>
      <!-- Validation result -->
      <UCard v-if="hlsValidationResult">
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon
              :name="hlsValidationResult.valid ? 'i-lucide-check-circle' : 'i-lucide-alert-triangle'"
              :class="hlsValidationResult.valid ? 'text-success' : 'text-warning'"
              class="size-4"
            />
            HLS Validation — {{ hlsValidationResult.valid ? 'Valid' : 'Invalid' }}
            <span class="text-muted font-normal text-xs ml-2">{{ hlsValidationResult.variant_count }} variant(s)</span>
            <UButton icon="i-lucide-x" size="xs" variant="ghost" color="neutral" class="ml-auto" @click="hlsValidationResult = null" />
          </div>
        </template>
        <p class="font-mono text-xs text-muted">{{ hlsValidationResult.job_id }}</p>
      </UCard>
    </div>

    <!-- Validator -->
    <div v-if="subTab === 'validator'" class="space-y-4">
      <!-- Stats -->
      <div v-if="validatorStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <UCard v-for="item in [
          { label: 'Total', value: validatorStats.total },
          { label: 'Validated', value: validatorStats.validated },
          { label: 'Needs Fix', value: validatorStats.needs_fix },
          { label: 'Fixed', value: validatorStats.fixed },
        ]" :key="item.label" :ui="{ body: 'p-3' }">
          <p class="text-xl font-bold text-highlighted">{{ (item.value ?? 0).toLocaleString() }}</p>
          <p class="text-xs text-muted">{{ item.label }}</p>
        </UCard>
      </div>

      <!-- Validate / fix -->
      <UCard>
        <template #header><div class="font-semibold">Validate or Fix Media</div></template>
        <div class="flex flex-wrap gap-2 mb-4">
          <UInput v-model="validateId" placeholder="Media ID" class="flex-1 min-w-48" />
          <UButton :loading="validating" icon="i-lucide-shield-check" label="Validate" variant="outline" color="neutral" @click="runValidate" />
          <UButton :loading="validating" icon="i-lucide-wrench" label="Fix" variant="outline" color="warning" @click="runFix" />
        </div>
        <div v-if="validateResult">
          <pre class="text-xs bg-muted rounded p-3 overflow-auto max-h-64">{{ JSON.stringify(validateResult, null, 2) }}</pre>
        </div>
      </UCard>
    </div>
  </div>
</template>
