<script setup lang="ts">
import type { UserPreferences, WatchHistoryItem, StorageUsage, PermissionsInfo, APIToken, APITokenCreated, RatedItem } from '~/types/api'
import { THEMES, type ThemeValue } from '~/stores/theme'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { useAPITokensApi, useRatingsApi } from '~/composables/useApiEndpoints'

const QUALITY_OPTIONS = [
  { label: 'Auto', value: 'auto' },
  { label: '1080p', value: '1080p' },
  { label: '720p', value: '720p' },
  { label: '480p', value: '480p' },
  { label: '360p', value: '360p' },
]

const SPEED_OPTIONS = [
  { label: '0.5x', value: 0.5 },
  { label: '0.75x', value: 0.75 },
  { label: '1x (Normal)', value: 1 },
  { label: '1.25x', value: 1.25 },
  { label: '1.5x', value: 1.5 },
  { label: '2x', value: 2 },
]

const ITEMS_PER_PAGE_OPTIONS = [
  { label: '12', value: 12 },
  { label: '20', value: 20 },
  { label: '24', value: 24 },
  { label: '48', value: 48 },
  { label: '96', value: 96 },
]

definePageMeta({ layout: 'default', title: 'Profile', middleware: 'auth' })

const authStore = useAuthStore()
const themeStore = useThemeStore()
const router = useRouter()
const { changePassword, getPreferences, updatePreferences, requestDataDeletion } = useApiEndpoints()
const { list: listHistory, remove: removeHistory, clear: clearHistory } = useWatchHistoryApi()
const { getUsage, getPermissions } = useStorageApi()

const tokensApi = useAPITokensApi()
const ratingsApi = useRatingsApi()
const toast = useToast()

const storageUsage = ref<StorageUsage | null>(null)
const permissionsInfo = ref<PermissionsInfo | null>(null)
async function loadStorageUsage() {
  try {
    const [u, p] = await Promise.allSettled([getUsage(), getPermissions()])
    if (u.status === 'fulfilled') storageUsage.value = u.value
    if (p.status === 'fulfilled') permissionsInfo.value = p.value
  } catch { /* optional */ }
}

// Preferences
const prefs = ref<Partial<UserPreferences>>({})
const prefsLoading = ref(true)
const prefsSaving = ref(false)

async function loadPrefs() {
  try {
    const p = (await getPreferences()) ?? {}
    if (!p.default_quality) p.default_quality = 'auto'
    prefs.value = p
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load preferences', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { prefsLoading.value = false }
}

async function savePrefs() {
  if (prefsSaving.value) return
  prefsSaving.value = true
  try {
    const toSave = { ...prefs.value }
    if (toSave.default_quality === 'auto') toSave.default_quality = ''
    await updatePreferences(toSave)
    if (prefs.value.theme) themeStore.setTheme(prefs.value.theme as ThemeValue)
    toast.add({ title: 'Preferences saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    prefsSaving.value = false
  }
}

// Watch history
const history = ref<WatchHistoryItem[]>([])
const historyLoading = ref(true)
const historySearch = ref('')
const historyFilter = ref<'all' | 'in-progress' | 'completed'>('all')
const historyPage = ref(1)
const historyPerPage = 20

async function loadHistory() {
  historyLoading.value = true
  try { history.value = (await listHistory(200)) ?? [] }
  catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load watch history', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { historyLoading.value = false }
}

async function removeItem(id: string) {
  if (!id) return
  try {
    await removeHistory(id)
    history.value = history.value.filter(h => h.media_id !== id)
    toast.add({ title: 'Removed from history', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

const clearHistoryConfirmOpen = ref(false)
const exportingCsv = ref(false)

async function exportWatchHistoryCsv() {
  exportingCsv.value = true
  try {
    const res = await fetch('/api/watch-history/export', { credentials: 'include' })
    if (!res.ok) throw new Error(`Export failed: ${res.status}`)
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `watch-history-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Export failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    exportingCsv.value = false
  }
}

async function doClearHistory() {
  clearHistoryConfirmOpen.value = false
  try {
    await clearHistory()
    history.value = []
    toast.add({ title: 'Watch history cleared', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

const filteredHistory = computed(() => {
  let result = history.value
  if (historyFilter.value === 'completed') result = result.filter(h => h.completed)
  else if (historyFilter.value === 'in-progress') result = result.filter(h => !h.completed)
  if (!historySearch.value) return result
  const q = historySearch.value.toLowerCase()
  return result.filter(h => (h.media_name || h.media_id || '').toLowerCase().includes(q))
})

const historyTotalPages = computed(() => Math.max(1, Math.ceil(filteredHistory.value.length / historyPerPage)))
const pagedHistory = computed(() => {
  const start = (historyPage.value - 1) * historyPerPage
  return filteredHistory.value.slice(start, start + historyPerPage)
})

watch([historySearch, historyFilter], () => { historyPage.value = 1 })

// Password
const pw = reactive({ current: '', new: '', confirm: '' })
const pwLoading = ref(false)

async function handleChangePassword() {
  if (pw.new !== pw.confirm) {
    toast.add({ title: 'Passwords do not match', color: 'error', icon: 'i-lucide-x' })
    return
  }
  if (pw.new.length < 8) {
    toast.add({ title: 'Password must be at least 8 characters', color: 'error', icon: 'i-lucide-x' })
    return
  }
  pwLoading.value = true
  try {
    await changePassword(pw.current, pw.new)
    toast.add({ title: 'Password changed', color: 'success', icon: 'i-lucide-check' })
    pw.current = ''; pw.new = ''; pw.confirm = ''
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    pwLoading.value = false
  }
}

// Data deletion request
const deletionRequestOpen = ref(false)
const deletionReason = ref('')
const deletionSubmitting = ref(false)
const deletionSubmitted = ref(false)

async function handleDeletionRequest() {
  deletionSubmitting.value = true
  try {
    await requestDataDeletion(deletionReason.value)
    deletionRequestOpen.value = false
    deletionSubmitted.value = true
    deletionReason.value = ''
    toast.add({ title: 'Request submitted', description: 'An administrator will review your request.', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to submit request', color: 'error', icon: 'i-lucide-x' })
  } finally {
    deletionSubmitting.value = false
  }
}

// My Ratings
const myRatings = ref<RatedItem[]>([])
const ratingsLoading = ref(false)

const ratingsDistribution = computed(() => {
  if (!myRatings.value.length) return []
  const counts: Record<number, number> = { 1: 0, 2: 0, 3: 0, 4: 0, 5: 0 }
  for (const r of myRatings.value) {
    const star = Math.round(r.rating)
    if (star >= 1 && star <= 5) counts[star]++
  }
  const max = Math.max(...Object.values(counts))
  return [5, 4, 3, 2, 1].map(star => ({
    star,
    count: counts[star],
    pct: max > 0 ? Math.round((counts[star] / max) * 100) : 0,
  }))
})

async function loadMyRatings() {
  ratingsLoading.value = true
  try { myRatings.value = (await ratingsApi.getMyRatings()) ?? [] }
  catch { /* non-critical */ }
  finally { ratingsLoading.value = false }
}

// API Tokens
const tokens = ref<APIToken[]>([])
const tokensLoading = ref(false)
const newTokenName = ref('')
const newTokenCreating = ref(false)
const revealedToken = ref<string | null>(null)
let revealedTokenTimer: ReturnType<typeof setTimeout> | null = null

function startTokenAutoDismiss() {
  if (revealedTokenTimer) clearTimeout(revealedTokenTimer)
  revealedTokenTimer = setTimeout(() => { revealedToken.value = null }, 60000)
}

watch(revealedToken, (val) => {
  if (val) startTokenAutoDismiss()
  else if (revealedTokenTimer) { clearTimeout(revealedTokenTimer); revealedTokenTimer = null }
})

onUnmounted(() => {
  if (revealedTokenTimer) clearTimeout(revealedTokenTimer)
  revealedToken.value = null
})

async function copyToken() {
  if (!revealedToken.value) return
  try {
    await navigator.clipboard.writeText(revealedToken.value)
    toast.add({ title: 'Token copied to clipboard', color: 'success', icon: 'i-lucide-check' })
  } catch {
    toast.add({ title: 'Failed to copy token', color: 'error', icon: 'i-lucide-x' })
  }
}

async function loadTokens() {
  tokensLoading.value = true
  try { tokens.value = (await tokensApi.list()) ?? [] }
  catch { /* non-critical */ }
  finally { tokensLoading.value = false }
}

async function createToken() {
  if (!newTokenName.value.trim()) return
  newTokenCreating.value = true
  try {
    const created = await tokensApi.create(newTokenName.value.trim()) as APITokenCreated
    revealedToken.value = created.token
    tokens.value = [{ id: created.id, name: created.name, last_used_at: created.last_used_at, created_at: created.created_at }, ...tokens.value]
    newTokenName.value = ''
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to create token', color: 'error', icon: 'i-lucide-x' })
  } finally {
    newTokenCreating.value = false
  }
}

async function revokeToken(id: string) {
  try {
    await tokensApi.delete(id)
    tokens.value = tokens.value.filter(t => t.id !== id)
    if (revealedToken.value) revealedToken.value = null
    toast.add({ title: 'Token revoked', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

let hasFetched = false
function loadAll() {
  hasFetched = true
  loadPrefs(); loadHistory(); loadStorageUsage(); loadTokens(); loadMyRatings()
}
onMounted(() => { if (!authStore.isLoading && authStore.user) loadAll() })
watch(() => authStore.user, (user) => { if (user && !hasFetched) loadAll() })
</script>

<template>
  <UContainer class="py-6 max-w-4xl space-y-6">
    <!-- Loading -->
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <template v-else-if="authStore.user">
      <!-- Account info -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-user" class="size-4" />
            Account
          </div>
        </template>
        <div class="flex items-center gap-4">
          <div class="w-14 h-14 rounded-full bg-primary/10 flex items-center justify-center text-primary text-xl font-bold">
            {{ authStore.username[0]?.toUpperCase() }}
          </div>
          <div class="flex-1 min-w-0">
            <p class="font-semibold text-lg">{{ authStore.username }}</p>
            <div class="flex items-center gap-2 mt-1 flex-wrap">
              <UBadge :label="authStore.user.role" :color="authStore.isAdmin ? 'warning' : 'neutral'" variant="subtle" size="xs" />
              <span v-if="authStore.user.created_at" class="text-sm text-muted">Member since {{ new Date(authStore.user.created_at).toLocaleDateString() }}</span>
              <span v-if="authStore.user.previous_last_login" class="text-sm text-muted">· Previous session: {{ new Date(authStore.user.previous_last_login).toLocaleDateString() }}</span>
            </div>
            <div v-if="storageUsage" class="mt-2 max-w-xs space-y-1">
              <div class="flex justify-between text-xs text-muted">
                <span>Storage</span>
                <span>{{ storageUsage.used_gb.toFixed(2) }} GB / {{ storageUsage.quota_gb > 0 ? storageUsage.quota_gb + ' GB' : 'Unlimited' }}</span>
              </div>
              <UProgress :value="storageUsage.quota_gb > 0 ? storageUsage.percentage : 0" size="xs" :color="storageUsage.percentage > 90 ? 'error' : storageUsage.percentage > 70 ? 'warning' : 'success'" />
            </div>
            <div v-if="permissionsInfo?.capabilities" class="mt-2 flex flex-wrap gap-1.5">
              <UBadge
                v-for="[cap, allowed] in Object.entries(permissionsInfo.capabilities)"
                :key="cap"
                :label="cap.replace(/^can/, '').replace(/([A-Z])/g, ' $1').trim()"
                :color="allowed ? 'success' : 'neutral'"
                :variant="allowed ? 'subtle' : 'outline'"
                size="xs"
              />
            </div>
          </div>
        </div>
      </UCard>

      <!-- My Ratings -->
      <UCard v-if="myRatings.length > 0 || ratingsLoading">
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-star" class="size-4" />
            My Ratings
            <UBadge v-if="myRatings.length > 0" :label="String(myRatings.length)" color="neutral" variant="subtle" size="xs" />
          </div>
        </template>
        <div v-if="ratingsLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <template v-else>
          <div v-if="ratingsDistribution.length > 0" class="mb-3 pb-3 border-b border-default space-y-0.5">
            <div v-for="row in ratingsDistribution" :key="row.star" class="flex items-center gap-2">
              <span class="text-xs text-muted w-5 shrink-0">{{ row.star }}★</span>
              <div class="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
                <div class="h-full bg-yellow-400 rounded-full" :style="{ width: `${row.pct}%` }" />
              </div>
              <span class="text-xs text-muted w-4 text-right shrink-0">{{ row.count }}</span>
            </div>
          </div>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="item in myRatings"
            :key="item.media_id"
            :to="`/player?id=${encodeURIComponent(item.media_id)}`"
            class="group shrink-0 w-36"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                v-if="item.thumbnail_url"
                :src="item.thumbnail_url"
                :alt="item.name"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-film" class="size-6 text-muted" />
              </div>
              <!-- Rating badge -->
              <div class="absolute bottom-1 right-1 bg-black/70 text-yellow-400 text-xs px-1.5 py-0.5 rounded flex items-center gap-0.5">
                <UIcon name="i-lucide-star" class="size-3" />
                {{ item.rating.toFixed(1) }}
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="item.name">{{ item.name }}</p>
            <p class="text-xs text-muted truncate">{{ item.category || item.media_type }}</p>
          </NuxtLink>
        </div>
        </template>
      </UCard>

      <!-- Preferences -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-sliders-horizontal" class="size-4" />
            Preferences
          </div>
        </template>
        <div v-if="prefsLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Theme">
            <USelect
              :model-value="prefs.theme as ThemeValue"
              :items="THEMES.map(t => ({ label: t.name, value: t.value }))"
              @update:model-value="prefs.theme = $event as string"
            />
          </UFormField>
          <UFormField label="Default Quality">
            <USelect
              v-model="prefs.default_quality"
              :items="QUALITY_OPTIONS"
            />
          </UFormField>
          <UFormField label="Playback Speed">
            <USelect
              v-model="prefs.playback_speed"
              :items="SPEED_OPTIONS"
            />
          </UFormField>
          <UFormField label="Items per Page">
            <USelect
              v-model="prefs.items_per_page"
              :items="ITEMS_PER_PAGE_OPTIONS"
            />
          </UFormField>
          <UFormField label="View Mode">
            <UButtonGroup>
              <UButton
                v-for="m in [{ label: 'Grid', value: 'grid', icon: 'i-lucide-grid-2x2' }, { label: 'List', value: 'list', icon: 'i-lucide-list' }, { label: 'Compact', value: 'compact', icon: 'i-lucide-align-justify' }]"
                :key="m.value"
                :icon="m.icon"
                :label="m.label"
                size="sm"
                :variant="prefs.view_mode === m.value ? 'solid' : 'outline'"
                :color="prefs.view_mode === m.value ? 'primary' : 'neutral'"
                @click="prefs.view_mode = m.value as 'grid' | 'list' | 'compact'"
              />
            </UButtonGroup>
          </UFormField>
          <div class="col-span-full grid grid-cols-2 sm:grid-cols-3 gap-3">
            <div v-for="toggle in [
              { key: 'auto_play', label: 'Auto-play' },
              { key: 'resume_playback', label: 'Resume Playback' },
              { key: 'show_mature', label: 'Show Mature Content' },
              { key: 'show_continue_watching', label: 'Continue Watching' },
              { key: 'show_recommended', label: 'Recommended' },
              { key: 'show_trending', label: 'Trending' },
            ]" :key="toggle.key" class="flex items-center gap-2">
              <USwitch :model-value="!!(prefs as Record<string, unknown>)[toggle.key]" @update:model-value="(prefs as Record<string, unknown>)[toggle.key] = $event" />
              <span class="text-sm">{{ toggle.label }}</span>
            </div>
          </div>
        </div>
        <template #footer>
          <UButton :loading="prefsSaving" icon="i-lucide-save" label="Save Preferences" @click="savePrefs" />
        </template>
      </UCard>

      <!-- Watch history -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2 font-semibold">
              <UIcon name="i-lucide-history" class="size-4" />
              Watch History
            </div>
            <div v-if="history.length > 0" class="flex items-center gap-1">
              <UButton
                icon="i-lucide-download"
                label="Export CSV"
                variant="ghost"
                color="neutral"
                size="xs"
                :loading="exportingCsv"
                @click="exportWatchHistoryCsv"
              />
              <UButton
                icon="i-lucide-trash-2"
                label="Clear All"
                variant="ghost"
                color="error"
                size="xs"
                @click="clearHistoryConfirmOpen = true"
              />
            </div>
          </div>
        </template>
        <div v-if="history.length > 5" class="flex flex-wrap items-center gap-2 mb-3">
          <UInput
            v-model="historySearch"
            icon="i-lucide-search"
            placeholder="Search history…"
            class="w-56"
          />
          <div class="flex gap-1">
            <UButton
              v-for="opt in (['all', 'in-progress', 'completed'] as const)"
              :key="opt"
              size="xs"
              :variant="historyFilter === opt ? 'solid' : 'outline'"
              :color="historyFilter === opt ? 'primary' : 'neutral'"
              :label="opt === 'all' ? 'All' : opt === 'in-progress' ? 'In Progress' : 'Completed'"
              @click="historyFilter = opt"
            />
          </div>
        </div>
        <div v-if="historyLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="filteredHistory.length === 0" class="text-center py-6 text-muted text-sm">
          No watch history.
        </div>
        <div v-else class="divide-y divide-default">
          <div
            v-for="item in pagedHistory"
            :key="item.media_id"
            class="flex items-center justify-between py-2 gap-3"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5">
                <p class="text-sm font-medium truncate">{{ getDisplayTitle(item) }}</p>
                <UBadge v-if="item.completed" label="Completed" color="success" variant="subtle" size="xs" class="shrink-0" />
              </div>
              <div class="flex items-center gap-2 mt-0.5">
                <p class="text-xs text-muted">
                  {{ item.completed ? 'Completed' : `${Math.round(item.progress)}% watched` }}
                  <span v-if="item.watched_at"> · {{ new Date(item.watched_at).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) }}</span>
                </p>
              </div>
            </div>
            <UButton
              icon="i-lucide-x"
              aria-label="Remove from history"
              size="xs"
              variant="ghost"
              color="neutral"
              @click="removeItem(item.media_id)"
            />
          </div>
        </div>
        <div v-if="historyTotalPages > 1" class="flex justify-center pt-3">
          <UPagination v-model:page="historyPage" :total="filteredHistory.length" :items-per-page="historyPerPage" />
        </div>
      </UCard>

      <UModal v-model:open="clearHistoryConfirmOpen" title="Clear Watch History" description="This will permanently delete all your watch history. This action cannot be undone.">
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="clearHistoryConfirmOpen = false" />
          <UButton color="error" label="Clear All" @click="doClearHistory" />
        </template>
      </UModal>

      <!-- API Tokens -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-key-round" class="size-4" />
            API Tokens
          </div>
        </template>
        <p class="text-sm text-muted mb-4">Create tokens to access the API from scripts or tools using <code class="bg-muted/40 px-1 rounded text-xs">Authorization: Bearer &lt;token&gt;</code>.</p>

        <!-- Revealed token banner -->
        <UAlert
          v-if="revealedToken"
          color="warning"
          variant="subtle"
          icon="i-lucide-triangle-alert"
          title="Copy your token now — it won't be shown again."
          class="mb-4"
        >
          <template #description>
            <div class="flex items-center gap-2 mt-1 flex-wrap">
              <code class="text-xs break-all select-all">{{ revealedToken }}</code>
              <UButton size="xs" icon="i-lucide-copy" variant="ghost" color="neutral" aria-label="Copy to clipboard" @click="copyToken" />
              <UButton size="xs" icon="i-lucide-x" variant="ghost" color="neutral" aria-label="Dismiss" @click="revealedToken = null" />
            </div>
          </template>
        </UAlert>

        <!-- Create new token -->
        <div class="flex gap-2 mb-4">
          <UInput v-model="newTokenName" placeholder="Token name (e.g. My Script)" class="flex-1" @keydown.enter="createToken" />
          <UButton :loading="newTokenCreating" icon="i-lucide-plus" label="Create" @click="createToken" />
        </div>

        <!-- Token list -->
        <div v-if="tokensLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="tokens.length === 0" class="text-sm text-muted py-2">No API tokens yet.</div>
        <div v-else class="divide-y divide-default">
          <div v-for="t in tokens" :key="t.id" class="flex items-center justify-between py-2 gap-3">
            <div class="min-w-0">
              <p class="text-sm font-medium truncate">{{ t.name }}</p>
              <p class="text-xs text-muted"><template v-if="t.created_at">Created {{ new Date(t.created_at).toLocaleDateString() }}</template><template v-if="t.last_used_at"> · Last used {{ new Date(t.last_used_at).toLocaleDateString() }}</template></p>
            </div>
            <UButton icon="i-lucide-trash-2" size="xs" variant="ghost" color="error" aria-label="Revoke token" @click="revokeToken(t.id)" />
          </div>
        </div>
      </UCard>

      <!-- Change password -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-key" class="size-4" />
            Change Password
          </div>
        </template>
        <div class="space-y-3 max-w-sm">
          <UFormField label="Current Password">
            <UInput v-model="pw.current" type="password" placeholder="••••••••" />
          </UFormField>
          <UFormField label="New Password">
            <UInput v-model="pw.new" type="password" placeholder="••••••••" />
          </UFormField>
          <UFormField label="Confirm New Password">
            <UInput v-model="pw.confirm" type="password" placeholder="••••••••" />
          </UFormField>
          <UButton :loading="pwLoading" label="Change Password" @click="handleChangePassword" />
        </div>
      </UCard>

      <!-- Data privacy -->
      <UCard v-if="!authStore.isAdmin">
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-shield-check" class="size-4" />
            Data Privacy
          </div>
        </template>

        <div v-if="deletionSubmitted" class="text-sm text-muted space-y-1">
          <p class="font-medium text-default">Request submitted</p>
          <p>Your data deletion request has been submitted. An administrator will review it and take action. You will not be notified by email unless an admin contacts you directly.</p>
        </div>
        <template v-else>
          <p class="text-sm text-muted mb-3">To request deletion of your account and associated data, submit a request below. An administrator will review and process it.</p>
          <UButton icon="i-lucide-file-text" label="Request Data Deletion" variant="outline" color="warning" @click="deletionRequestOpen = true" />
        </template>

        <UModal v-model:open="deletionRequestOpen" title="Request Data Deletion" description="Your request will be reviewed by an administrator before any data is removed.">
          <template #body>
            <UFormField label="Reason (optional)">
              <UTextarea v-model="deletionReason" placeholder="Let us know why you'd like your data deleted…" :rows="3" />
            </UFormField>
          </template>
          <template #footer>
            <UButton variant="ghost" color="neutral" label="Cancel" @click="deletionRequestOpen = false" />
            <UButton :loading="deletionSubmitting" color="warning" label="Submit Request" @click="handleDeletionRequest" />
          </template>
        </UModal>
      </UCard>
    </template>

    <!-- Fallback: prevents blank page if auth resolves without a user (edge case) -->
    <div v-else class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>
  </UContainer>
</template>
