<script setup lang="ts">
import type {
  CategoryStats, CategorizedItem, DiscoverySuggestion,
  SuggestionStats, ClassifyStatus, ClassifyStats,
} from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const subTab = ref('categorizer')
const subTabs = [
  { label: 'Categorizer', value: 'categorizer', icon: 'i-lucide-tag' },
  { label: 'Auto-Discovery', value: 'discovery', icon: 'i-lucide-compass' },
  { label: 'Suggestions', value: 'suggestions', icon: 'i-lucide-sparkles' },
  { label: 'Classification', value: 'classify', icon: 'i-lucide-brain' },
]

// ── Categorizer ────────────────────────────────────────────────────────────────
const categoryStats = ref<CategoryStats | null>(null)
const categorizerLoading = ref(false)
const categorizePath = ref('')
const categorizeCategory = ref('')
const categorizing = ref(false)
const categorizeResult = ref<unknown>(null)

async function loadCategorizer() {
  categorizerLoading.value = true
  try {
    categoryStats.value = await adminApi.getCategoryStats()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load categorizer', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { categorizerLoading.value = false }
}

async function categorizeFile() {
  if (!categorizePath.value.trim()) return
  categorizing.value = true
  try {
    categorizeResult.value = await adminApi.categorizeFile(categorizePath.value.trim())
    toast.add({ title: 'File categorized', color: 'success', icon: 'i-lucide-check' })
    await loadCategorizer()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { categorizing.value = false }
}

async function categorizeDirectory() {
  if (!categorizePath.value.trim()) return
  categorizing.value = true
  try {
    const results = await adminApi.categorizeDirectory(categorizePath.value.trim())
    categorizeResult.value = results
    toast.add({ title: `Categorized ${Array.isArray(results) ? results.length : 0} files`, color: 'success', icon: 'i-lucide-check' })
    await loadCategorizer()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { categorizing.value = false }
}

async function setCategory() {
  if (!categorizePath.value.trim() || !categorizeCategory.value.trim()) return
  categorizing.value = true
  try {
    await adminApi.setMediaCategory(categorizePath.value.trim(), categorizeCategory.value.trim())
    toast.add({ title: 'Category set', color: 'success', icon: 'i-lucide-check' })
    await loadCategorizer()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { categorizing.value = false }
}

const browseCategory = ref('')
const categoryItems = ref<CategorizedItem[]>([])
const categoryItemsLoading = ref(false)

async function browseByCategory() {
  if (!browseCategory.value.trim()) return
  categoryItemsLoading.value = true
  try {
    categoryItems.value = (await adminApi.getByCategory(browseCategory.value.trim())) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { categoryItemsLoading.value = false }
}

async function cleanStaleCategories() {
  try {
    const res = await adminApi.cleanStaleCategories()
    toast.add({ title: `Cleaned ${(res as { removed: number }).removed ?? 0} stale entries`, color: 'success', icon: 'i-lucide-check' })
    await loadCategorizer()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// ── Auto-Discovery ─────────────────────────────────────────────────────────────
const discoverySuggestions = ref<DiscoverySuggestion[]>([])
const discoveryLoading = ref(false)
const scanDirectory = ref('')
const scanning = ref(false)

async function loadDiscovery() {
  discoveryLoading.value = true
  try {
    discoverySuggestions.value = (await adminApi.getDiscoverySuggestions()) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load discovery', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { discoveryLoading.value = false }
}

async function runDiscoveryScan() {
  if (!scanDirectory.value.trim()) return
  scanning.value = true
  try {
    const results = await adminApi.discoveryScan(scanDirectory.value.trim())
    discoverySuggestions.value = results ?? []
    toast.add({ title: `Found ${discoverySuggestions.value.length} suggestions`, color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { scanning.value = false }
}

async function applyDiscovery(path: string) {
  try {
    await adminApi.applyDiscoverySuggestion(path)
    discoverySuggestions.value = discoverySuggestions.value.filter(s => s.original_path !== path)
    toast.add({ title: 'Suggestion applied', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function dismissDiscovery(path: string) {
  try {
    await adminApi.dismissDiscoverySuggestion(path)
    discoverySuggestions.value = discoverySuggestions.value.filter(s => s.original_path !== path)
    toast.add({ title: 'Suggestion dismissed', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// ── Suggestions ────────────────────────────────────────────────────────────────
const suggestionStats = ref<SuggestionStats | null>(null)
const suggestionsLoading = ref(false)

async function loadSuggestions() {
  suggestionsLoading.value = true
  try {
    suggestionStats.value = await adminApi.getSuggestionStats()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load suggestions', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { suggestionsLoading.value = false }
}

function formatTime(seconds: number): string {
  if (!seconds) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

// ── HuggingFace Classification ─────────────────────────────────────────────────
const classifyStatus = ref<ClassifyStatus | null>(null)
const classifyStats = ref<ClassifyStats | null>(null)
const classifyLoading = ref(false)
const classifyPath = ref('')
const classifying = ref(false)
const classifyResult = ref<unknown>(null)
const clearTagsId = ref('')
const clearingTags = ref(false)

async function loadClassify() {
  classifyLoading.value = true
  try {
    const [status, stats] = await Promise.all([
      adminApi.getClassifyStatus(),
      adminApi.getClassifyStats(),
    ])
    classifyStatus.value = status
    classifyStats.value = stats
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load classification', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { classifyLoading.value = false }
}

async function classifyFile() {
  if (!classifyPath.value.trim()) return
  classifying.value = true
  classifyResult.value = null
  try {
    classifyResult.value = await adminApi.classifyFile(classifyPath.value.trim())
    toast.add({ title: 'Classification complete', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { classifying.value = false }
}

async function classifyDirectory() {
  if (!classifyPath.value.trim()) return
  classifying.value = true
  classifyResult.value = null
  try {
    classifyResult.value = await adminApi.classifyDirectory(classifyPath.value.trim())
    toast.add({ title: 'Directory classification queued', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { classifying.value = false }
}

async function classifyAllPending() {
  classifying.value = true
  try {
    const res = await adminApi.classifyAllPending()
    toast.add({ title: `Queued ${(res as { count: number }).count ?? 0} items for classification`, color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { classifying.value = false }
}

async function runClassifyTask() {
  try {
    await adminApi.classifyRunTask()
    toast.add({ title: 'Classification task triggered', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function clearClassificationTags() {
  if (!clearTagsId.value.trim()) return
  clearingTags.value = true
  try {
    await adminApi.classifyClearTags(clearTagsId.value.trim())
    toast.add({ title: 'Tags cleared', color: 'success', icon: 'i-lucide-check' })
    clearTagsId.value = ''
    await loadClassify()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { clearingTags.value = false }
}

// Tab-switching lazy load
watch(subTab, (tab) => {
  if (tab === 'categorizer' && !categoryStats.value) loadCategorizer()
  else if (tab === 'discovery' && discoverySuggestions.value.length === 0) loadDiscovery()
  else if (tab === 'suggestions' && !suggestionStats.value) loadSuggestions()
  else if (tab === 'classify' && !classifyStatus.value) loadClassify()
}, { immediate: true })
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" orientation="horizontal" class="w-full">
      <template #content="{ item }">
        <div class="pt-3 space-y-4">

          <!-- ── Categorizer ─────────────────────────────────────────────── -->
          <template v-if="item.value === 'categorizer'">
            <div v-if="categoryStats" class="space-y-4">
              <!-- Stats -->
              <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
                <UCard>
                  <p class="text-2xl font-bold">{{ categoryStats.total_items }}</p>
                  <p class="text-xs text-muted mt-1">Total Categorized</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold">{{ categoryStats.manual_overrides }}</p>
                  <p class="text-xs text-muted mt-1">Manual Overrides</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold">{{ Object.keys(categoryStats.by_category).length }}</p>
                  <p class="text-xs text-muted mt-1">Categories</p>
                </UCard>
              </div>
              <!-- By-category breakdown -->
              <UCard v-if="Object.keys(categoryStats.by_category).length > 0">
                <template #header><span class="font-semibold">By Category</span></template>
                <div class="grid grid-cols-2 sm:grid-cols-3 gap-2">
                  <div v-for="(count, cat) in categoryStats.by_category" :key="cat" class="flex items-center justify-between p-2 rounded bg-muted/30">
                    <span class="text-sm font-medium capitalize">{{ cat }}</span>
                    <UBadge :label="String(count)" color="neutral" variant="subtle" size="xs" />
                  </div>
                </div>
              </UCard>
            </div>
            <div v-else-if="categorizerLoading" class="flex justify-center py-8">
              <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
            </div>

            <!-- Actions -->
            <UCard>
              <template #header><span class="font-semibold">Categorize / Set Category</span></template>
              <div class="space-y-2">
                <UInput v-model="categorizePath" placeholder="File path" />
                <div class="flex gap-2">
                  <UInput v-model="categorizeCategory" placeholder="Category (for manual set)" class="flex-1" />
                </div>
                <div class="flex gap-2 flex-wrap">
                  <UButton :loading="categorizing" icon="i-lucide-tag" label="Auto-Categorize File" :disabled="!categorizePath.trim()" @click="categorizeFile" />
                  <UButton :loading="categorizing" icon="i-lucide-folder-sync" label="Categorize Directory" :disabled="!categorizePath.trim()" color="neutral" variant="outline" @click="categorizeDirectory" />
                  <UButton :loading="categorizing" icon="i-lucide-pen" label="Set Category" :disabled="!categorizePath.trim() || !categorizeCategory.trim()" color="neutral" @click="setCategory" />
                  <UButton icon="i-lucide-trash-2" label="Clean Stale" color="warning" variant="outline" @click="cleanStaleCategories" />
                  <UButton icon="i-lucide-refresh-cw" aria-label="Refresh stats" variant="ghost" color="neutral" @click="loadCategorizer" />
                </div>
              </div>
              <pre v-if="categorizeResult" class="mt-3 p-2 rounded bg-muted text-xs overflow-x-auto">{{ JSON.stringify(categorizeResult, null, 2) }}</pre>
            </UCard>

            <!-- Browse by category -->
            <UCard>
              <template #header><span class="font-semibold">Browse by Category</span></template>
              <div class="flex gap-2">
                <UInput v-model="browseCategory" placeholder="Category name" class="flex-1"
                  @keyup.enter="browseByCategory"
                />
                <UButton :loading="categoryItemsLoading" icon="i-lucide-search" label="Browse" :disabled="!browseCategory.trim()" @click="browseByCategory" />
              </div>
              <div v-if="categoryItemsLoading" class="flex justify-center py-4 mt-2">
                <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
              </div>
              <div v-else-if="categoryItems.length > 0" class="mt-3 divide-y divide-default max-h-64 overflow-y-auto">
                <div v-for="item in categoryItems" :key="item.id" class="py-2 text-sm">
                  <p class="font-medium truncate">{{ item.name }}</p>
                  <p class="text-xs text-muted truncate">{{ item.category }}</p>
                </div>
              </div>
              <p v-else-if="!categoryItemsLoading && browseCategory && categoryItems.length === 0" class="text-center py-4 text-muted text-sm mt-2">No items in this category.</p>
            </UCard>
          </template>

          <!-- ── Auto-Discovery ──────────────────────────────────────────── -->
          <template v-else-if="item.value === 'discovery'">
            <UCard>
              <template #header><span class="font-semibold">Scan Directory</span></template>
              <div class="flex gap-2">
                <UInput v-model="scanDirectory" placeholder="Directory path to scan" class="flex-1" />
                <UButton :loading="scanning" icon="i-lucide-compass" label="Scan" :disabled="!scanDirectory.trim()" @click="runDiscoveryScan" />
                <UButton icon="i-lucide-refresh-cw" aria-label="Reload suggestions" variant="ghost" color="neutral" @click="loadDiscovery" />
              </div>
            </UCard>

            <UCard>
              <template #header>
                <span class="font-semibold">Suggestions ({{ discoverySuggestions.length }})</span>
              </template>
              <div v-if="discoveryLoading" class="flex justify-center py-6">
                <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
              </div>
              <div v-else-if="discoverySuggestions.length === 0" class="text-center py-8 text-muted text-sm">
                No discovery suggestions. Run a scan to find media files.
              </div>
              <div v-else class="divide-y divide-default">
                <div v-for="s in discoverySuggestions" :key="s.original_path" class="flex items-center gap-3 py-2 flex-wrap">
                  <div class="flex-1 min-w-0">
                    <p class="text-sm font-medium truncate">{{ s.suggested_name }}</p>
                    <p class="text-xs text-muted truncate">{{ s.original_path }}</p>
                    <div class="flex items-center gap-2 mt-0.5">
                      <UBadge :label="s.type" color="neutral" variant="subtle" size="xs" />
                      <span class="text-xs text-muted">{{ Math.round(s.confidence * 100) }}% confidence</span>
                    </div>
                  </div>
                  <div class="flex gap-1">
                    <UButton icon="i-lucide-check" aria-label="Apply suggestion" size="xs" variant="ghost" color="success" @click="applyDiscovery(s.original_path)" />
                    <UButton icon="i-lucide-x" aria-label="Dismiss suggestion" size="xs" variant="ghost" color="error" @click="dismissDiscovery(s.original_path)" />
                  </div>
                </div>
              </div>
            </UCard>
          </template>

          <!-- ── Suggestions ─────────────────────────────────────────────── -->
          <template v-else-if="item.value === 'suggestions'">
            <div v-if="suggestionsLoading" class="flex justify-center py-8">
              <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
            </div>
            <template v-else-if="suggestionStats">
              <div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
                <UCard>
                  <p class="text-2xl font-bold">{{ suggestionStats.total_profiles }}</p>
                  <p class="text-xs text-muted mt-1">User Profiles</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold">{{ suggestionStats.total_media }}</p>
                  <p class="text-xs text-muted mt-1">Media Tracked</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold">{{ suggestionStats.total_views.toLocaleString() }}</p>
                  <p class="text-xs text-muted mt-1">Total Views</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold">{{ formatTime(suggestionStats.total_watch_time) }}</p>
                  <p class="text-xs text-muted mt-1">Watch Time</p>
                </UCard>
              </div>
              <div class="flex justify-end">
                <UButton icon="i-lucide-refresh-cw" aria-label="Refresh suggestion stats" variant="ghost" color="neutral" @click="loadSuggestions" />
              </div>
            </template>
          </template>

          <!-- ── HuggingFace Classification ──────────────────────────────── -->
          <template v-else-if="item.value === 'classify'">
            <div v-if="classifyLoading" class="flex justify-center py-8">
              <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
            </div>
            <template v-else>
              <!-- Status card -->
              <UCard v-if="classifyStatus">
                <template #header><span class="font-semibold">Classification Status</span></template>
                <div class="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm">
                  <div>
                    <p class="text-muted text-xs">Configured</p>
                    <UBadge :label="classifyStatus.configured ? 'Yes' : 'No'" :color="classifyStatus.configured ? 'success' : 'error'" variant="subtle" size="xs" />
                  </div>
                  <div>
                    <p class="text-muted text-xs">Enabled</p>
                    <UBadge :label="classifyStatus.enabled ? 'Yes' : 'No'" :color="classifyStatus.enabled ? 'success' : 'neutral'" variant="subtle" size="xs" />
                  </div>
                  <div>
                    <p class="text-muted text-xs">Model</p>
                    <p class="font-medium">{{ classifyStatus.model || '—' }}</p>
                  </div>
                  <div>
                    <p class="text-muted text-xs">Rate Limit</p>
                    <p class="font-medium">{{ classifyStatus.rate_limit }}/min</p>
                  </div>
                  <div>
                    <p class="text-muted text-xs">Max Frames</p>
                    <p class="font-medium">{{ classifyStatus.max_frames }}</p>
                  </div>
                  <div>
                    <p class="text-muted text-xs">Concurrency</p>
                    <p class="font-medium">{{ classifyStatus.max_concurrent }}</p>
                  </div>
                  <div v-if="classifyStatus.task_last_run">
                    <p class="text-muted text-xs">Last Run</p>
                    <p class="font-medium">{{ new Date(classifyStatus.task_last_run).toLocaleString() }}</p>
                  </div>
                  <div v-if="classifyStatus.task_next_run">
                    <p class="text-muted text-xs">Next Run</p>
                    <p class="font-medium">{{ new Date(classifyStatus.task_next_run).toLocaleString() }}</p>
                  </div>
                  <div v-if="classifyStatus.task_last_error" class="col-span-full">
                    <p class="text-muted text-xs">Last Error</p>
                    <p class="text-error text-xs">{{ classifyStatus.task_last_error }}</p>
                  </div>
                </div>
              </UCard>

              <!-- Stats -->
              <div v-if="classifyStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
                <UCard>
                  <p class="text-2xl font-bold">{{ classifyStats.total_media }}</p>
                  <p class="text-xs text-muted mt-1">Total Media</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold text-error">{{ classifyStats.mature_total }}</p>
                  <p class="text-xs text-muted mt-1">Mature</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold text-success">{{ classifyStats.mature_classified }}</p>
                  <p class="text-xs text-muted mt-1">Classified</p>
                </UCard>
                <UCard>
                  <p class="text-2xl font-bold text-warning">{{ classifyStats.mature_pending }}</p>
                  <p class="text-xs text-muted mt-1">Pending</p>
                </UCard>
              </div>

              <!-- Actions -->
              <UCard>
                <template #header><span class="font-semibold">Classify</span></template>
                <div class="space-y-3">
                  <div class="flex gap-2">
                    <UInput v-model="classifyPath" placeholder="File path to classify" class="flex-1" />
                    <UButton :loading="classifying" icon="i-lucide-brain" label="Classify File" :disabled="!classifyPath.trim()" @click="classifyFile" />
                  </div>
                  <div class="flex gap-2 flex-wrap">
                    <UButton :loading="classifying" icon="i-lucide-folder-open" label="Classify Directory" :disabled="!classifyPath.trim()" color="neutral" variant="outline" @click="classifyDirectory" />
                    <UButton :loading="classifying" icon="i-lucide-list-checks" label="Classify All Pending" color="warning" variant="outline" @click="classifyAllPending" />
                    <UButton icon="i-lucide-play" label="Run Task Now" color="neutral" variant="outline" @click="runClassifyTask" />
                    <UButton icon="i-lucide-refresh-cw" aria-label="Refresh classification" variant="ghost" color="neutral" @click="loadClassify" />
                  </div>
                  <pre v-if="classifyResult" class="p-2 rounded bg-muted text-xs overflow-x-auto">{{ JSON.stringify(classifyResult, null, 2) }}</pre>
                  <!-- Clear tags -->
                  <div class="border-t border-default pt-3">
                    <p class="text-xs text-muted mb-2">Clear classification tags for a media item by ID:</p>
                    <div class="flex gap-2">
                      <UInput v-model="clearTagsId" placeholder="Media ID" class="flex-1" />
                      <UButton :loading="clearingTags" icon="i-lucide-eraser" label="Clear Tags" color="error" variant="outline" :disabled="!clearTagsId.trim()" @click="clearClassificationTags" />
                    </div>
                  </div>
                </div>
              </UCard>

              <!-- Recent classified items -->
              <UCard v-if="classifyStats && classifyStats.recent_items.length > 0">
                <template #header><span class="font-semibold">Recent Classifications</span></template>
                <div class="divide-y divide-default">
                  <div v-for="it in classifyStats.recent_items" :key="it.id" class="flex items-center gap-3 py-2">
                    <div class="flex-1 min-w-0">
                      <p class="text-sm font-medium truncate">{{ it.name }}</p>
                      <div class="flex flex-wrap gap-1 mt-0.5">
                        <UBadge v-for="tag in it.tags.slice(0, 5)" :key="tag" :label="tag" color="neutral" variant="subtle" size="xs" />
                      </div>
                    </div>
                    <UBadge
                      :label="`${Math.round(it.mature_score * 100)}%`"
                      :color="it.mature_score > 0.7 ? 'error' : it.mature_score > 0.4 ? 'warning' : 'success'"
                      variant="subtle"
                      size="xs"
                    />
                  </div>
                </div>
              </UCard>
            </template>
          </template>

        </div>
      </template>
    </UTabs>
  </div>
</template>
