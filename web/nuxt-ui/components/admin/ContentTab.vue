<script setup lang="ts">
import type { ReviewQueueItem, ScannerStats, ValidatorStats, AutoTagRule } from '~/types/api'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const toast = useToast()

const subTab = ref('scanner')
let scanRefreshTimer: ReturnType<typeof setTimeout> | null = null
const subTabs = [
  { label: 'Mature Scanner', value: 'scanner', icon: 'i-lucide-scan' },
  { label: 'Validator', value: 'validator', icon: 'i-lucide-shield-check' },
  { label: 'Auto-Tags', value: 'autotags', icon: 'i-lucide-tag' },
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
  const path = scanPath.value.trim()
  if (path && (path.includes('..') || /^[a-zA-Z]:/.test(path) || path.startsWith('/'))) {
    toast.add({ title: 'Invalid scan path', color: 'error', icon: 'i-lucide-x' })
    return
  }
  scanning.value = true
  try {
    await adminApi.runScan(path || undefined)
    toast.add({ title: 'Scan started', color: 'success', icon: 'i-lucide-check' })
    if (scanRefreshTimer) clearTimeout(scanRefreshTimer)
    scanRefreshTimer = setTimeout(loadScanner, 2000)
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
    await loadScanner()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

const reviewingId = ref<string | null>(null)

async function quickReview(id: string, action: 'approve' | 'reject') {
  if (reviewingId.value) return
  reviewingId.value = id
  try {
    if (action === 'approve') await adminApi.approveContent(id)
    else await adminApi.rejectContent(id)
    await loadScanner()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : `Failed to ${action}`, color: 'error', icon: 'i-lucide-x' })
  } finally {
    reviewingId.value = null
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

// ── Validator ──────────────────────────────────────────────────────────────────
const validatorStats = ref<ValidatorStats | null>(null)
const validatorLoading = ref(false)
const validateId = ref('')
const validating = ref(false)
const validateResult = ref<unknown>(null)

async function loadValidator() {
  validatorLoading.value = true
  try { validatorStats.value = await adminApi.getValidatorStats() }
  catch (e: unknown) { toast.add({ title: e instanceof Error ? e.message : 'Failed to load validator stats', color: 'error', icon: 'i-lucide-x' }) }
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

// ── Auto-tag rules ─────────────────────────────────────────────────────────────
const autoTagRules = ref<AutoTagRule[]>([])
const autoTagLoading = ref(false)
const autoTagApplying = ref(false)
const autoTagRuleForm = reactive({ name: '', pattern: '', tags: '', priority: 0, enabled: true })
const autoTagFormOpen = ref(false)
const autoTagEditTarget = ref<AutoTagRule | null>(null)
const autoTagSaving = ref(false)
const autoTagDeletingId = ref<string | null>(null)

async function loadAutoTagRules() {
  autoTagLoading.value = true
  try {
    autoTagRules.value = (await adminApi.listAutoTagRules()) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load rules', color: 'error', icon: 'i-lucide-x' })
  } finally {
    autoTagLoading.value = false
  }
}

function openCreateRule() {
  autoTagEditTarget.value = null
  autoTagRuleForm.name = ''
  autoTagRuleForm.pattern = ''
  autoTagRuleForm.tags = ''
  autoTagRuleForm.priority = 0
  autoTagRuleForm.enabled = true
  autoTagFormOpen.value = true
}

function openEditRule(rule: AutoTagRule) {
  autoTagEditTarget.value = rule
  autoTagRuleForm.name = rule.name
  autoTagRuleForm.pattern = rule.pattern
  autoTagRuleForm.tags = rule.tags
  autoTagRuleForm.priority = rule.priority
  autoTagRuleForm.enabled = rule.enabled
  autoTagFormOpen.value = true
}

async function saveAutoTagRule() {
  if (!autoTagRuleForm.name || !autoTagRuleForm.pattern || !autoTagRuleForm.tags) {
    toast.add({ title: 'Name, pattern, and tags are required', color: 'error', icon: 'i-lucide-x' })
    return
  }
  autoTagSaving.value = true
  try {
    if (autoTagEditTarget.value) {
      await adminApi.updateAutoTagRule(autoTagEditTarget.value.id, { ...autoTagRuleForm })
      toast.add({ title: 'Rule updated', color: 'success', icon: 'i-lucide-check' })
    } else {
      await adminApi.createAutoTagRule({ ...autoTagRuleForm })
      toast.add({ title: 'Rule created', color: 'success', icon: 'i-lucide-check' })
    }
    autoTagFormOpen.value = false
    await loadAutoTagRules()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Save failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    autoTagSaving.value = false
  }
}

async function deleteAutoTagRule(id: string) {
  if (autoTagDeletingId.value) return
  autoTagDeletingId.value = id
  try {
    await adminApi.deleteAutoTagRule(id)
    autoTagRules.value = autoTagRules.value.filter(r => r.id !== id)
    toast.add({ title: 'Rule deleted', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Delete failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    autoTagDeletingId.value = null
  }
}

async function applyAutoTagRules() {
  autoTagApplying.value = true
  try {
    const result = await adminApi.applyAutoTagRules()
    toast.add({ title: `Applied ${result.applied} rule(s) to ${result.items_affected} item(s)`, color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Apply failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    autoTagApplying.value = false
  }
}

watch(subTab, (v) => {
  if (v === 'scanner') { loadScanner(); loadScannerConfig() }
  else if (v === 'validator') loadValidator()
  else if (v === 'autotags') loadAutoTagRules()
}, { immediate: true })

onUnmounted(() => {
  if (scanRefreshTimer) { clearTimeout(scanRefreshTimer); scanRefreshTimer = null }
})
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">

    <!-- Scanner -->
    <div v-if="item.value === 'scanner'" class="space-y-4">
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
              <UButton icon="i-lucide-check" aria-label="Approve" size="xs" variant="ghost" color="success" :loading="reviewingId === item.id" @click="quickReview(item.id, 'approve')" />
              <UButton icon="i-lucide-x" aria-label="Reject" size="xs" variant="ghost" color="error" :loading="reviewingId === item.id" @click="quickReview(item.id, 'reject')" />
            </div>
          </div>
        </div>
      </UCard>
    </div>


    <!-- Validator -->
    <div v-if="item.value === 'validator'" class="space-y-4">
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

    <!-- Auto-Tags -->
    <div v-if="item.value === 'autotags'" class="space-y-4">
      <!-- Header actions -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p class="text-sm text-muted">
            Rules are matched against the media file path (case-insensitive substring). Tags are merged into existing tags.
            Higher priority rules are applied first; only the first matching rule runs per item.
          </p>
        </div>
        <div class="flex gap-2">
          <UButton
            icon="i-lucide-play"
            label="Apply All Rules"
            size="sm"
            color="primary"
            variant="outline"
            :loading="autoTagApplying"
            @click="applyAutoTagRules"
          />
          <UButton
            icon="i-lucide-plus"
            label="New Rule"
            size="sm"
            color="primary"
            @click="openCreateRule"
          />
          <UButton icon="i-lucide-refresh-cw" aria-label="Refresh rules" size="sm" variant="ghost" color="neutral" :loading="autoTagLoading" @click="loadAutoTagRules" />
        </div>
      </div>

      <!-- Loading -->
      <div v-if="autoTagLoading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5 text-primary" />
      </div>

      <!-- Empty -->
      <div v-else-if="autoTagRules.length === 0" class="text-center py-10 text-muted text-sm">
        <UIcon name="i-lucide-tag" class="size-8 mb-2" />
        <p>No auto-tag rules yet.</p>
        <p class="text-xs mt-1">Create a rule to automatically tag media based on their file path.</p>
      </div>

      <!-- Rules list -->
      <UCard v-else>
        <div class="divide-y divide-default">
          <div
            v-for="rule in autoTagRules"
            :key="rule.id"
            class="flex items-start gap-3 py-3"
          >
            <div class="flex-1 min-w-0 space-y-1">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="font-medium text-sm text-highlighted">{{ rule.name }}</span>
                <UBadge
                  :label="rule.enabled ? 'Enabled' : 'Disabled'"
                  :color="rule.enabled ? 'success' : 'neutral'"
                  variant="subtle"
                  size="xs"
                />
                <UBadge v-if="rule.priority !== 0" :label="`Priority ${rule.priority}`" color="info" variant="subtle" size="xs" />
              </div>
              <div class="flex items-center gap-1 text-xs text-muted">
                <UIcon name="i-lucide-folder-search" class="size-3.5 shrink-0" />
                <span class="font-mono truncate">{{ rule.pattern }}</span>
              </div>
              <div class="flex flex-wrap gap-1 mt-0.5">
                <UBadge
                  v-for="tag in rule.tags.split(',').map(t => t.trim()).filter(Boolean)"
                  :key="tag"
                  :label="tag"
                  color="primary"
                  variant="subtle"
                  size="xs"
                />
              </div>
            </div>
            <div class="flex gap-1 shrink-0">
              <UButton icon="i-lucide-pencil" size="xs" variant="ghost" color="neutral" @click="openEditRule(rule)" />
              <UButton
                icon="i-lucide-trash-2"
                size="xs"
                variant="ghost"
                color="error"
                :loading="autoTagDeletingId === rule.id"
                @click="deleteAutoTagRule(rule.id)"
              />
            </div>
          </div>
        </div>
      </UCard>

      <!-- Create / Edit modal -->
      <UModal
        v-model:open="autoTagFormOpen"
        :title="autoTagEditTarget ? 'Edit Auto-Tag Rule' : 'New Auto-Tag Rule'"
      >
        <template #body>
          <div class="space-y-4">
            <UFormField label="Name" required>
              <UInput v-model="autoTagRuleForm.name" placeholder="e.g. Jazz Music" class="w-full" />
            </UFormField>
            <UFormField label="Path pattern" hint="Case-insensitive substring of the file path" required>
              <UInput v-model="autoTagRuleForm.pattern" placeholder="e.g. /music/jazz" class="w-full font-mono" />
            </UFormField>
            <UFormField label="Tags" hint="Comma-separated — merged into existing tags" required>
              <UInput v-model="autoTagRuleForm.tags" placeholder="e.g. jazz, instrumental, relaxing" class="w-full" />
            </UFormField>
            <UFormField label="Priority" hint="Higher = applied first (0 = default)">
              <UInput v-model.number="autoTagRuleForm.priority" type="number" class="w-28" />
            </UFormField>
            <UFormField label="Enabled">
              <UCheckbox v-model="autoTagRuleForm.enabled" label="Rule is active" />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <UButton :label="autoTagEditTarget ? 'Save' : 'Create'" color="primary" :loading="autoTagSaving" @click="saveAutoTagRule" />
          <UButton label="Cancel" variant="ghost" color="neutral" @click="autoTagFormOpen = false" />
        </template>
      </UModal>
    </div>

        </div>
      </template>
    </UTabs>
  </div>
</template>
