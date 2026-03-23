<script setup lang="ts">
definePageMeta({ title: 'Admin Panel', middleware: 'admin' })

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()

// Redirect non-admins to the admin login page
watchEffect(() => {
  if (!authStore.isLoading && !authStore.isAdmin) {
    router.replace('/admin-login')
  }
})

const TABS = [
  { label: 'Dashboard', value: 'dashboard', icon: 'i-lucide-layout-dashboard' },
  { label: 'Users', value: 'users', icon: 'i-lucide-users' },
  { label: 'Media', value: 'media', icon: 'i-lucide-film' },
  { label: 'Streaming', value: 'streaming', icon: 'i-lucide-radio' },
  { label: 'Analytics', value: 'analytics', icon: 'i-lucide-bar-chart-2' },
  { label: 'Playlists', value: 'playlists', icon: 'i-lucide-list-music' },
  { label: 'Security', value: 'security', icon: 'i-lucide-shield' },
  { label: 'Downloader', value: 'downloader', icon: 'i-lucide-cloud-download' },
  { label: 'System', value: 'system', icon: 'i-lucide-settings' },
  { label: 'Updates', value: 'updates', icon: 'i-lucide-download' },
]

const VALID = TABS.map(t => t.value)
const activeTab = ref(
  VALID.includes(route.query.tab as string) ? (route.query.tab as string) : 'dashboard',
)

watch(activeTab, tab => {
  router.replace({ query: { tab } })
})
</script>

<template>
  <UContainer class="py-6">
    <!-- Loading -->
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <div v-else-if="authStore.isAdmin" class="space-y-6">
      <!-- Header -->
      <div class="flex items-center justify-between flex-wrap gap-3">
        <h1 class="text-2xl font-bold text-highlighted flex items-center gap-2">
          <UIcon name="i-lucide-shield" class="size-6 text-primary" />
          Admin Panel
        </h1>
        <div class="flex gap-2">
          <UButton to="/" icon="i-lucide-house" label="Home" variant="ghost" color="neutral" size="sm" />
        </div>
      </div>

      <!-- Tab nav -->
      <UTabs
        v-model="activeTab"
        :items="TABS"
        orientation="horizontal"
        class="w-full"
      />

      <!-- Tab content -->
      <div class="min-h-64">
        <AdminDashboardTab v-if="activeTab === 'dashboard'" />
        <AdminUsersTab v-else-if="activeTab === 'users'" />
        <AdminMediaTab v-else-if="activeTab === 'media'" />
        <AdminStreamingTab v-else-if="activeTab === 'streaming'" />
        <AdminAnalyticsTab v-else-if="activeTab === 'analytics'" />
        <AdminPlaylistsTab v-else-if="activeTab === 'playlists'" />
        <AdminSecurityTab v-else-if="activeTab === 'security'" />
        <AdminDownloaderTab v-else-if="activeTab === 'downloader'" />
        <AdminSystemTab v-else-if="activeTab === 'system'" />
        <AdminUpdatesTab v-else-if="activeTab === 'updates'" />
      </div>
    </div>
  </UContainer>
</template>
