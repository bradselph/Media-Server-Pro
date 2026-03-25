<script setup lang="ts">
import type { UserPreferences, WatchHistoryItem } from '~/types/api'
import { THEMES, type ThemeValue } from '~/stores/theme'
import { getDisplayTitle } from '~/utils/mediaTitle'

definePageMeta({ layout: 'default', title: 'Profile', middleware: 'auth' })

const authStore = useAuthStore()
const themeStore = useThemeStore()
const router = useRouter()
const { changePassword, deleteAccount, getPreferences, updatePreferences } = useApiEndpoints()
const { list: listHistory, remove: removeHistory, clear: clearHistory } = useWatchHistoryApi()
const toast = useToast()

// Redirect if not logged in
watchEffect(() => {
  if (!authStore.isLoading && !authStore.isLoggedIn) router.replace('/login')
})

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

async function doClearHistory() {
  try {
    await clearHistory()
    history.value = []
    toast.add({ title: 'Watch history cleared', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

const filteredHistory = computed(() => {
  if (!historySearch.value) return history.value
  const q = historySearch.value.toLowerCase()
  return history.value.filter(h => (h.media_name || h.media_id || '').toLowerCase().includes(q))
})

const historyTotalPages = computed(() => Math.max(1, Math.ceil(filteredHistory.value.length / historyPerPage)))
const pagedHistory = computed(() => {
  const start = (historyPage.value - 1) * historyPerPage
  return filteredHistory.value.slice(start, start + historyPerPage)
})

watch(historySearch, () => { historyPage.value = 1 })

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

// Delete account
const deleteOpen = ref(false)
const deletePassword = ref('')
const deleteLoading = ref(false)

async function handleDeleteAccount() {
  deleteLoading.value = true
  try {
    await deleteAccount(deletePassword.value)
    authStore.clear()
    router.replace('/')
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    deleteLoading.value = false
  }
}

onMounted(() => { loadPrefs(); loadHistory() })
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
          <div>
            <p class="font-semibold text-lg">{{ authStore.username }}</p>
            <div class="flex items-center gap-2 mt-1">
              <UBadge :label="authStore.user.role" :color="authStore.isAdmin ? 'warning' : 'neutral'" variant="subtle" size="xs" />
              <span class="text-sm text-muted">Member since {{ new Date(authStore.user.created_at).toLocaleDateString() }}</span>
            </div>
          </div>
        </div>
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
              :items="[{ label: 'Auto', value: 'auto' }, { label: '1080p', value: '1080p' }, { label: '720p', value: '720p' }, { label: '480p', value: '480p' }, { label: '360p', value: '360p' }]"
            />
          </UFormField>
          <UFormField label="Playback Speed">
            <USelect
              v-model="prefs.playback_speed"
              :items="[{ label: '0.5x', value: 0.5 }, { label: '0.75x', value: 0.75 }, { label: '1x (Normal)', value: 1 }, { label: '1.25x', value: 1.25 }, { label: '1.5x', value: 1.5 }, { label: '2x', value: 2 }]"
            />
          </UFormField>
          <UFormField label="Items per Page">
            <USelect
              v-model="prefs.items_per_page"
              :items="[{ label: '12', value: 12 }, { label: '20', value: 20 }, { label: '24', value: 24 }, { label: '48', value: 48 }, { label: '96', value: 96 }]"
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
              { key: 'show_analytics', label: 'Analytics' },
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
            <UButton
              v-if="history.length > 0"
              icon="i-lucide-trash-2"
              label="Clear All"
              variant="ghost"
              color="error"
              size="xs"
              @click="doClearHistory"
            />
          </div>
        </template>
        <UInput
          v-if="history.length > 5"
          v-model="historySearch"
          icon="i-lucide-search"
          placeholder="Search history…"
          class="mb-3 w-64"
        />
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
            <div class="min-w-0">
              <p class="text-sm font-medium truncate">{{ getDisplayTitle(item) }}</p>
              <p class="text-xs text-muted">{{ item.watched_at ? new Date(item.watched_at).toLocaleString() : '' }}</p>
            </div>
            <UButton
              icon="i-lucide-x"
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

      <!-- Danger zone -->
      <UCard v-if="!authStore.isAdmin" :ui="{ root: 'ring-1 ring-error/30' }">
        <template #header>
          <div class="flex items-center gap-2 font-semibold text-error">
            <UIcon name="i-lucide-triangle-alert" class="size-4" />
            Danger Zone
          </div>
        </template>
        <p class="text-sm text-muted mb-3">Permanently delete your account and all associated data.</p>
        <UButton icon="i-lucide-trash-2" label="Delete Account" color="error" variant="outline" @click="deleteOpen = true" />

        <UModal v-model:open="deleteOpen" title="Delete Account" description="This action cannot be undone. All your data will be permanently removed.">
          <template #body>
            <UFormField label="Enter your password to confirm">
              <UInput v-model="deletePassword" type="password" placeholder="••••••••" />
            </UFormField>
          </template>
          <template #footer>
            <UButton variant="ghost" color="neutral" label="Cancel" @click="deleteOpen = false" />
            <UButton :loading="deleteLoading" color="error" label="Delete My Account" @click="handleDeleteAccount" />
          </template>
        </UModal>
      </UCard>
    </template>
  </UContainer>
</template>
