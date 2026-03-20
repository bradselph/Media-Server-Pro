<script setup lang="ts">
import type { UserPreferences, WatchHistoryEntry, StorageUsage, PermissionsInfo } from '~/types/api'
import { ApiError } from '~/composables/useApi'

definePageMeta({
  title: 'Profile',
})

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()
const endpoints = useApiEndpoints()
const watchHistoryApi = useWatchHistoryApi()
const storageApiComposable = useStorageApi()

// ── Redirect to login if not authenticated ──
watch(() => authStore.isAuthenticated, (authenticated) => {
  if (!authenticated && !authStore.isLoading) {
    router.push('/login')
  }
}, { immediate: true })

// ── Toast state ──
const toast = useToast()

// ── Mature redirect handling ──
const matureRedirect = computed(() => {
  const raw = (route.query.mature_redirect as string) || ''
  return raw.startsWith('/') && !raw.startsWith('//') ? raw : ''
})

// ── Preferences ──
const preferences = ref<UserPreferences | null>(null)
const prefsLoading = ref(true)
const prefsError = ref(false)
const prefsSubmitting = ref(false)

// ── Storage & Permissions ──
const storageUsage = ref<StorageUsage | null>(null)
const permissions = ref<PermissionsInfo | null>(null)

// ── Watch history ──
const watchHistory = ref<WatchHistoryEntry[]>([])
const watchHistoryError = ref(false)
const historySearch = ref('')
const historySortBy = ref<'watched_at' | 'name' | 'duration' | 'progress'>('watched_at')
const historySortDesc = ref(true)

// ── Password change ──
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordError = ref('')
const passwordSubmitting = ref(false)

// ── Delete account ──
const showDeleteConfirm = ref(false)
const deletePassword = ref('')
const deleteError = ref('')
const deleteSubmitting = ref(false)

// ── Theme options ──
const themeOptions = [
  { label: 'Auto (System)', value: 'auto' },
  { label: 'Light', value: 'light' },
  { label: 'Dark', value: 'dark' },
  { label: 'Midnight', value: 'midnight' },
  { label: 'Nord', value: 'nord' },
  { label: 'Dracula', value: 'dracula' },
  { label: 'Solarized Light', value: 'solarized-light' },
  { label: 'Forest', value: 'forest' },
  { label: 'Sunset', value: 'sunset' },
]

const qualityOptions = [
  { label: 'Auto', value: 'auto' },
  { label: 'Low (360p)', value: 'low' },
  { label: 'Medium (480p)', value: 'medium' },
  { label: 'High (720p)', value: 'high' },
  { label: 'Ultra (1080p)', value: 'ultra' },
]

const speedOptions = [
  { label: '0.5x', value: 0.5 },
  { label: '0.75x', value: 0.75 },
  { label: '1x (normal)', value: 1 },
  { label: '1.25x', value: 1.25 },
  { label: '1.5x', value: 1.5 },
  { label: '2x', value: 2 },
]

const itemsPerPageOptions = [
  { label: '12', value: 12 },
  { label: '24 (default)', value: 24 },
  { label: '48', value: 48 },
  { label: '96', value: 96 },
]

const viewModeOptions = [
  { label: 'Grid', value: 'grid', icon: 'i-lucide-grid-3x3' },
  { label: 'List', value: 'list', icon: 'i-lucide-list' },
  { label: 'Compact', value: 'compact', icon: 'i-lucide-layout-list' },
]

// ── Computed ──
const sortedHistory = computed(() => {
  let items = [...watchHistory.value]
  if (historySearch.value) {
    const q = historySearch.value.toLowerCase()
    items = items.filter(e => displayMediaName(e).toLowerCase().includes(q))
  }
  items.sort((a, b) => {
    let cmp = 0
    switch (historySortBy.value) {
      case 'watched_at':
        cmp = new Date(a.watched_at).getTime() - new Date(b.watched_at).getTime()
        break
      case 'name':
        cmp = displayMediaName(a).localeCompare(displayMediaName(b))
        break
      case 'duration':
        cmp = (a.duration || 0) - (b.duration || 0)
        break
      case 'progress':
        cmp = (a.progress || 0) - (b.progress || 0)
        break
    }
    return historySortDesc.value ? -cmp : cmp
  })
  return items
})

const storageBarColor = computed(() => {
  if (!storageUsage.value) return 'bg-primary'
  if (storageUsage.value.percentage > 90) return 'bg-red-500'
  if (storageUsage.value.percentage > 70) return 'bg-amber-500'
  return 'bg-primary'
})

// ── Helpers ──
function formatDate(timestamp: string | undefined): string {
  if (!timestamp) return 'N/A'
  const date = new Date(timestamp)
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
}

function displayMediaName(entry: WatchHistoryEntry): string {
  if (entry.media_name) {
    const base = entry.media_name.split('/').pop()?.split('\\').pop() || entry.media_name
    return base.replace(/\.[^/.]+$/, '').replace(/[._-]/g, ' ')
  }
  return 'Unknown title'
}

function formatDurationHuman(seconds: number): string {
  if (!seconds || seconds < 0) return '0s'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  const parts: string[] = []
  if (h > 0) parts.push(`${h}h`)
  if (m > 0) parts.push(`${m}m`)
  if (s > 0 || parts.length === 0) parts.push(`${s}s`)
  return parts.join(' ')
}

function updatePref<K extends keyof UserPreferences>(key: K, value: UserPreferences[K]) {
  if (preferences.value) {
    preferences.value = { ...preferences.value, [key]: value }
  }
}

// ── Data loading ──
async function loadPreferences() {
  try {
    const prefs = await endpoints.getPreferences()
    preferences.value = prefs
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) {
      prefsError.value = true
    }
  } finally {
    prefsLoading.value = false
  }
}

async function loadWatchHistory() {
  try {
    const history = await watchHistoryApi.list()
    watchHistory.value = Array.isArray(history) ? history : []
  } catch {
    watchHistoryError.value = true
  }
}

async function loadStorageAndPermissions() {
  try {
    const [storage, perms] = await Promise.all([
      storageApiComposable.getUsage(),
      storageApiComposable.getPermissions(),
    ])
    storageUsage.value = storage
    permissions.value = perms
  } catch {
    // Non-critical
  }
}

// ── Form handlers ──
async function handlePasswordSubmit() {
  passwordError.value = ''
  if (newPassword.value.length < 8) {
    passwordError.value = 'Password must be at least 8 characters'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = 'Passwords do not match'
    return
  }
  passwordSubmitting.value = true
  try {
    await endpoints.changePassword(currentPassword.value, newPassword.value)
    toast.add({ title: 'Password changed successfully', color: 'success' })
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
  } catch (err: unknown) {
    if (err instanceof ApiError) {
      passwordError.value = err.message
    } else {
      passwordError.value = 'Failed to change password'
    }
  } finally {
    passwordSubmitting.value = false
  }
}

async function handlePreferencesSubmit() {
  if (!preferences.value) return
  prefsSubmitting.value = true
  try {
    await endpoints.updatePreferences(preferences.value)
    await authStore.checkSession()
    toast.add({ title: 'Preferences saved', color: 'success' })
    if (matureRedirect.value) {
      if (preferences.value.show_mature) {
        router.replace(matureRedirect.value)
      } else {
        router.replace('/')
      }
      return
    }
  } catch {
    toast.add({ title: 'Failed to save preferences', color: 'error' })
  } finally {
    prefsSubmitting.value = false
  }
}

async function handleDeleteHistoryItem(mediaId: string) {
  try {
    await watchHistoryApi.remove(mediaId)
    watchHistory.value = watchHistory.value.filter(e => e.media_id !== mediaId)
    toast.add({ title: 'History entry removed', color: 'success' })
  } catch {
    toast.add({ title: 'Failed to remove history entry', color: 'error' })
  }
}

async function handleClearHistory() {
  try {
    await watchHistoryApi.clear()
    watchHistory.value = []
    toast.add({ title: 'Watch history cleared', color: 'success' })
  } catch {
    toast.add({ title: 'Failed to clear watch history', color: 'error' })
  }
}

async function handleDeleteAccount() {
  deleteError.value = ''
  deleteSubmitting.value = true
  try {
    await endpoints.deleteAccount(deletePassword.value)
    toast.add({ title: 'Account deleted', color: 'success' })
    window.location.href = '/'
  } catch (err: unknown) {
    if (err instanceof ApiError) {
      deleteError.value = err.message
    } else {
      deleteError.value = 'Failed to delete account'
    }
  } finally {
    deleteSubmitting.value = false
  }
}

// ── Initialize ──
onMounted(() => {
  loadPreferences()
  loadWatchHistory()
  loadStorageAndPermissions()
})
</script>

<template>
  <UContainer class="py-8">
    <!-- Loading -->
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin text-2xl text-(--ui-text-dimmed)" />
    </div>

    <div v-else-if="authStore.user" class="max-w-3xl mx-auto space-y-6">
      <!-- Page header -->
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-2xl font-bold text-(--ui-text-highlighted)">User Profile</h1>
          <p class="text-sm text-(--ui-text-muted)">Manage your account settings and preferences</p>
        </div>
        <UButton to="/" variant="ghost" icon="i-lucide-arrow-left" label="Back to Library" />
      </div>

      <!-- Mature redirect banner -->
      <div
        v-if="matureRedirect"
        class="flex items-center justify-between gap-3 px-4 py-3 rounded-lg bg-primary/10 border border-primary/20 flex-wrap"
      >
        <span class="text-sm flex items-center gap-2">
          <UIcon name="i-lucide-shield" class="text-primary" />
          Enable mature content below to view the requested media.
        </span>
        <UButton size="sm" variant="ghost" label="Skip" @click="router.replace('/')" />
      </div>

      <!-- Account Information -->
      <UCard>
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">Account Information</h2>
        </template>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="flex justify-between sm:flex-col sm:gap-1">
            <span class="text-sm text-(--ui-text-muted)">Username</span>
            <span class="font-medium">{{ authStore.user.username }}</span>
          </div>
          <div class="flex justify-between sm:flex-col sm:gap-1">
            <span class="text-sm text-(--ui-text-muted)">Account Type</span>
            <span>
              <UBadge :color="authStore.isAdmin ? 'primary' : 'neutral'" variant="subtle">
                {{ authStore.user.role }}
              </UBadge>
            </span>
          </div>
          <div class="flex justify-between sm:flex-col sm:gap-1">
            <span class="text-sm text-(--ui-text-muted)">Member Since</span>
            <span class="font-medium">{{ formatDate(authStore.user.created_at) }}</span>
          </div>
          <div class="flex justify-between sm:flex-col sm:gap-1">
            <span class="text-sm text-(--ui-text-muted)">Last Login</span>
            <span class="font-medium">{{ formatDate(authStore.user.last_login) }}</span>
          </div>
        </div>
      </UCard>

      <!-- Storage Usage -->
      <UCard v-if="storageUsage">
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">Storage Usage</h2>
        </template>
        <div class="space-y-3">
          <div class="grid grid-cols-2 gap-4">
            <div>
              <span class="text-sm text-(--ui-text-muted)">Used</span>
              <p class="font-medium">{{ storageUsage.used_gb.toFixed(2) }} GB</p>
            </div>
            <div>
              <span class="text-sm text-(--ui-text-muted)">Quota</span>
              <p class="font-medium">
                {{ storageUsage.quota_gb > 0 ? `${storageUsage.quota_gb.toFixed(1)} GB` : 'Unlimited' }}
              </p>
            </div>
          </div>
          <div>
            <div class="flex justify-between text-xs text-(--ui-text-muted) mb-1">
              <span>{{ storageUsage.used_bytes.toLocaleString() }} bytes</span>
              <span>{{ storageUsage.percentage.toFixed(1) }}%</span>
            </div>
            <div class="w-full h-2 bg-(--ui-bg) rounded-full overflow-hidden">
              <div
                :class="['h-full rounded-full transition-all duration-300', storageBarColor]"
                :style="{ width: `${Math.min(storageUsage.percentage, 100)}%` }"
              />
            </div>
          </div>
        </div>
      </UCard>

      <!-- Permissions -->
      <UCard v-if="permissions">
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">My Permissions</h2>
        </template>
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
          <div
            v-for="{ label, value } in [
              { label: 'Stream', value: permissions.capabilities.canStream },
              { label: 'Download', value: permissions.capabilities.canDownload },
              { label: 'Upload', value: permissions.capabilities.canUpload },
              { label: 'Create Playlists', value: permissions.capabilities.canCreatePlaylists },
              { label: 'View Mature', value: permissions.capabilities.canViewMature },
              ...(permissions.capabilities.canDelete !== undefined ? [{ label: 'Delete', value: permissions.capabilities.canDelete }] : []),
              ...(permissions.capabilities.canManage !== undefined ? [{ label: 'Manage', value: permissions.capabilities.canManage }] : []),
            ]"
            :key="label"
            class="flex items-center gap-2"
          >
            <UIcon
              :name="value ? 'i-lucide-check-circle' : 'i-lucide-x-circle'"
              :class="value ? 'text-green-500' : 'text-red-400'"
              class="text-sm"
            />
            <span class="text-sm">{{ label }}</span>
          </div>
        </div>
        <div v-if="permissions.limits" class="mt-4 pt-4 border-t border-(--ui-border) grid grid-cols-2 gap-4">
          <div>
            <span class="text-sm text-(--ui-text-muted)">Storage Quota</span>
            <p class="font-medium">
              {{ permissions.limits.storage_quota > 0 ? `${(permissions.limits.storage_quota / 1073741824).toFixed(0)} GB` : 'Unlimited' }}
            </p>
          </div>
          <div>
            <span class="text-sm text-(--ui-text-muted)">Concurrent Streams</span>
            <p class="font-medium">{{ permissions.limits.concurrent_streams }}</p>
          </div>
        </div>
      </UCard>

      <!-- Preferences -->
      <UCard>
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">Preferences</h2>
        </template>

        <div v-if="prefsLoading" class="py-4 text-center">
          <UIcon name="i-lucide-loader-2" class="animate-spin text-(--ui-text-dimmed)" />
          <p class="text-sm text-(--ui-text-muted) mt-2">Loading preferences...</p>
        </div>

        <div v-else-if="prefsError" class="py-4 text-center">
          <p class="text-sm text-red-500">Failed to load preferences. Please refresh the page.</p>
        </div>

        <form v-else class="space-y-5" @submit.prevent="handlePreferencesSubmit">
          <!-- Playback fields -->
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <UFormField label="Default Video Quality">
              <USelect
                :model-value="preferences?.default_quality || 'auto'"
                :items="qualityOptions"
                value-key="value"
                class="w-full"
                @update:model-value="(v: string) => updatePref('default_quality', v)"
              />
            </UFormField>

            <UFormField label="Theme">
              <USelect
                :model-value="preferences?.theme || 'auto'"
                :items="themeOptions"
                value-key="value"
                class="w-full"
                @update:model-value="(v: string) => updatePref('theme', v)"
              />
            </UFormField>

            <UFormField label="Default Playback Speed">
              <USelect
                :model-value="preferences?.playback_speed ?? 1"
                :items="speedOptions"
                value-key="value"
                class="w-full"
                @update:model-value="(v: number) => updatePref('playback_speed', v)"
              />
            </UFormField>

            <UFormField label="Items Per Page">
              <USelect
                :model-value="preferences?.items_per_page ?? 24"
                :items="itemsPerPageOptions"
                value-key="value"
                class="w-full"
                @update:model-value="(v: number) => updatePref('items_per_page', v)"
              />
            </UFormField>
          </div>

          <!-- Volume -->
          <UFormField label="Default Volume">
            <div class="flex items-center gap-3">
              <input
                type="range"
                :value="preferences?.volume ?? 1"
                min="0"
                max="1"
                step="0.05"
                class="flex-1 h-2 accent-primary"
                @input="updatePref('volume', Number(($event.target as HTMLInputElement).value))"
              />
              <span class="text-sm font-mono text-(--ui-text-muted) w-10 text-right">
                {{ Math.round((preferences?.volume ?? 1) * 100) }}%
              </span>
            </div>
          </UFormField>

          <!-- View mode -->
          <UFormField label="Default View Mode">
            <div class="flex gap-1">
              <UButton
                v-for="mode in viewModeOptions"
                :key="mode.value"
                :variant="(preferences?.view_mode || 'grid') === mode.value ? 'solid' : 'outline'"
                size="sm"
                :icon="mode.icon"
                :label="mode.label"
                @click="updatePref('view_mode', mode.value)"
              />
            </div>
          </UFormField>

          <!-- Toggle switches -->
          <div class="space-y-3">
            <div class="flex items-center justify-between">
              <span class="text-sm">Autoplay next track</span>
              <USwitch
                :model-value="preferences?.auto_play ?? false"
                @update:model-value="(v: boolean) => updatePref('auto_play', v)"
              />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Resume playback position</span>
              <USwitch
                :model-value="preferences?.resume_playback ?? true"
                @update:model-value="(v: boolean) => updatePref('resume_playback', v)"
              />
            </div>
            <div class="flex items-center justify-between">
              <span class="text-sm">Show analytics bar</span>
              <USwitch
                :model-value="preferences?.show_analytics ?? false"
                @update:model-value="(v: boolean) => updatePref('show_analytics', v)"
              />
            </div>
          </div>

          <!-- Content settings -->
          <div class="pt-4 border-t border-(--ui-border)">
            <h3 class="text-sm font-semibold text-(--ui-text-highlighted) mb-3">Content Settings</h3>
            <div class="flex items-center justify-between">
              <span class="text-sm text-amber-600">Allow mature content (18+)</span>
              <USwitch
                :model-value="preferences?.show_mature ?? false"
                @update:model-value="(v: boolean) => updatePref('show_mature', v)"
              />
            </div>
          </div>

          <!-- Home page sections -->
          <div class="pt-4 border-t border-(--ui-border)">
            <h3 class="text-sm font-semibold text-(--ui-text-highlighted) mb-1">Home Page Sections</h3>
            <p class="text-xs text-(--ui-text-muted) mb-3">Choose which sections appear on your home page.</p>
            <div class="space-y-3">
              <div class="flex items-center justify-between">
                <span class="text-sm">Continue Watching</span>
                <USwitch
                  :model-value="preferences?.show_continue_watching ?? true"
                  @update:model-value="(v: boolean) => updatePref('show_continue_watching', v)"
                />
              </div>
              <div class="flex items-center justify-between">
                <span class="text-sm">Recommended For You</span>
                <USwitch
                  :model-value="preferences?.show_recommended ?? true"
                  @update:model-value="(v: boolean) => updatePref('show_recommended', v)"
                />
              </div>
              <div class="flex items-center justify-between">
                <span class="text-sm">Trending</span>
                <USwitch
                  :model-value="preferences?.show_trending ?? true"
                  @update:model-value="(v: boolean) => updatePref('show_trending', v)"
                />
              </div>
            </div>
          </div>

          <UButton type="submit" :loading="prefsSubmitting" label="Save Preferences" icon="i-lucide-save" />
        </form>
      </UCard>

      <!-- Watch History -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h2 class="font-semibold text-(--ui-text-highlighted)">Watch History</h2>
            <UButton
              v-if="watchHistory.length > 0"
              size="xs"
              variant="ghost"
              color="error"
              icon="i-lucide-trash-2"
              label="Clear All"
              @click="handleClearHistory"
            />
          </div>
        </template>

        <div v-if="watchHistoryError" class="py-4 text-center">
          <p class="text-sm text-red-500">Failed to load watch history</p>
        </div>

        <div v-else-if="watchHistory.length === 0" class="py-8 text-center">
          <UIcon name="i-lucide-clock" class="text-3xl text-(--ui-text-dimmed) mb-2" />
          <p class="text-sm text-(--ui-text-muted)">No watch history yet</p>
        </div>

        <div v-else class="space-y-3">
          <!-- Search and sort controls -->
          <div class="flex gap-2 flex-wrap items-center">
            <UInput
              v-model="historySearch"
              placeholder="Search history..."
              icon="i-lucide-search"
              size="sm"
              class="flex-1 min-w-[150px]"
            />
            <USelect
              v-model="historySortBy"
              :items="[
                { label: 'Date Watched', value: 'watched_at' },
                { label: 'Title', value: 'name' },
                { label: 'Duration', value: 'duration' },
                { label: 'Progress', value: 'progress' },
              ]"
              value-key="value"
              size="sm"
              class="w-36"
            />
            <UButton
              size="sm"
              variant="outline"
              :icon="historySortDesc ? 'i-lucide-arrow-down-wide-narrow' : 'i-lucide-arrow-up-narrow-wide'"
              @click="historySortDesc = !historySortDesc"
            />
          </div>

          <!-- History list -->
          <div v-if="sortedHistory.length === 0" class="py-4 text-center">
            <p class="text-sm text-(--ui-text-muted)">No matching history entries</p>
          </div>

          <div v-else class="space-y-1">
            <div
              v-for="(entry, i) in sortedHistory"
              :key="`${entry.media_id}-${i}`"
              class="flex items-center gap-3 p-2.5 rounded-lg hover:bg-(--ui-bg-elevated) transition-colors group"
            >
              <div class="min-w-0 flex-1">
                <NuxtLink
                  :to="`/player?id=${encodeURIComponent(entry.media_id)}`"
                  class="text-sm font-medium text-(--ui-text-highlighted) hover:text-primary truncate block"
                >
                  {{ displayMediaName(entry) }}
                </NuxtLink>
                <div class="flex items-center gap-2 text-xs text-(--ui-text-muted)">
                  <span>{{ formatDurationHuman(entry.duration) }}</span>
                  <span>{{ Math.round(entry.progress * 100) }}% watched</span>
                </div>
              </div>
              <span class="text-xs text-(--ui-text-muted) whitespace-nowrap hidden sm:block">
                {{ formatDate(entry.watched_at) }}
              </span>
              <UButton
                size="xs"
                variant="ghost"
                color="error"
                icon="i-lucide-x"
                class="opacity-0 group-hover:opacity-100 transition-opacity"
                @click="handleDeleteHistoryItem(entry.media_id)"
              />
            </div>
          </div>
        </div>
      </UCard>

      <!-- Change Password -->
      <UCard>
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">Change Password</h2>
        </template>

        <form class="space-y-4" @submit.prevent="handlePasswordSubmit">
          <div v-if="passwordError" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-sm text-red-500">
            {{ passwordError }}
          </div>

          <UFormField label="Current Password">
            <UInput
              v-model="currentPassword"
              type="password"
              autocomplete="current-password"
              required
            />
          </UFormField>

          <UFormField label="New Password" hint="Must be at least 8 characters">
            <UInput
              v-model="newPassword"
              type="password"
              autocomplete="new-password"
              :minlength="8"
              required
            />
          </UFormField>

          <UFormField label="Confirm New Password">
            <UInput
              v-model="confirmPassword"
              type="password"
              autocomplete="new-password"
              required
            />
          </UFormField>

          <UButton type="submit" :loading="passwordSubmitting" label="Change Password" icon="i-lucide-lock" />
        </form>
      </UCard>

      <!-- Danger Zone -->
      <UCard v-if="authStore.user.role !== 'admin'" class="border-red-500/30">
        <template #header>
          <h2 class="font-semibold text-red-500">Danger Zone</h2>
        </template>

        <div v-if="!showDeleteConfirm">
          <p class="text-sm text-(--ui-text-muted) mb-3">
            Permanently delete your account and all associated data.
          </p>
          <UButton color="error" variant="soft" label="Delete Account" icon="i-lucide-trash-2" @click="showDeleteConfirm = true" />
        </div>

        <form v-else class="space-y-4" @submit.prevent="handleDeleteAccount">
          <p class="text-sm text-(--ui-text-muted)">
            Enter your password to confirm account deletion. This cannot be undone.
          </p>

          <div v-if="deleteError" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-sm text-red-500">
            {{ deleteError }}
          </div>

          <UFormField label="Confirm Password">
            <UInput
              v-model="deletePassword"
              type="password"
              autocomplete="current-password"
              required
            />
          </UFormField>

          <div class="flex gap-2">
            <UButton type="submit" color="error" :loading="deleteSubmitting" label="Confirm Delete" icon="i-lucide-trash-2" />
            <UButton
              variant="ghost"
              label="Cancel"
              @click="showDeleteConfirm = false; deletePassword = ''; deleteError = ''"
            />
          </div>
        </form>
      </UCard>
    </div>
  </UContainer>
</template>
