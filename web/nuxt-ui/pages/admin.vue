<script setup lang="ts">
definePageMeta({ title: 'Admin Panel', middleware: 'admin' })

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()

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
  { label: 'Content', value: 'content', icon: 'i-lucide-scan' },
  { label: 'Sources', value: 'sources', icon: 'i-lucide-server' },
  { label: 'Discovery', value: 'discovery', icon: 'i-lucide-compass' },
  { label: 'Duplicates', value: 'duplicates', icon: 'i-lucide-copy-x' },
  { label: 'Collections', value: 'collections', icon: 'i-lucide-layers' },
  { label: 'Claude', value: 'claude', icon: 'i-lucide-brain' },
]

const VALID = TABS.map(t => t.value)
// Legacy tab name aliases — redirect old bookmarks to the current tab value.
const TAB_ALIASES: Record<string, string> = { settings: 'system' }
const activeTab = ref(
  VALID.includes(route.query.tab as string)
    ? (route.query.tab as string)
    : TAB_ALIASES[route.query.tab as string] ?? 'dashboard',
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

    <div v-else-if="authStore.isAdmin" class="space-y-4">
      <!-- Header -->
      <div class="flex items-center justify-between flex-wrap gap-3">
        <h1 class="text-2xl font-bold text-highlighted flex items-center gap-2">
          <UIcon name="i-lucide-shield" class="size-6 text-primary" />
          Admin Panel
        </h1>
        <UButton to="/" icon="i-lucide-house" label="Home" variant="ghost" color="neutral" size="sm" />
      </div>

      <!-- Sidebar layout -->
      <div class="flex gap-4 items-start">
        <!-- Mobile: horizontal scrollable pill nav -->
        <div class="md:hidden w-full flex gap-1 overflow-x-auto pb-1">
          <UButton
            v-for="tab in TABS"
            :key="tab.value"
            :icon="tab.icon"
            :label="tab.label"
            size="xs"
            :variant="activeTab === tab.value ? 'solid' : 'ghost'"
            :color="activeTab === tab.value ? 'primary' : 'neutral'"
            class="shrink-0"
            @click="activeTab = tab.value"
          />
        </div>

        <!-- Desktop: vertical sidebar -->
        <nav class="hidden md:flex flex-col gap-0.5 w-44 shrink-0 sticky top-4">
          <button
            v-for="tab in TABS"
            :key="tab.value"
            class="flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm font-medium w-full text-left transition-colors"
            :class="activeTab === tab.value
              ? 'bg-primary text-white'
              : 'text-muted hover:bg-elevated hover:text-highlighted'"
            @click="activeTab = tab.value"
          >
            <UIcon :name="tab.icon" class="size-4 shrink-0" />
            <span>{{ tab.label }}</span>
          </button>
        </nav>

        <!-- Content panel -->
        <div class="min-w-0 flex-1 min-h-64">
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
          <AdminContentTab v-else-if="activeTab === 'content'" />
          <AdminSourcesTab v-else-if="activeTab === 'sources'" />
          <AdminDiscoveryTab v-else-if="activeTab === 'discovery'" />
          <AdminDuplicatesTab v-else-if="activeTab === 'duplicates'" />
          <AdminCollectionsTab v-else-if="activeTab === 'collections'" />
          <AdminClaudeTab v-else-if="activeTab === 'claude'" />
        </div>
      </div>
    </div>

    <!-- Non-admin fallback -->
    <div v-else class="flex flex-col items-center justify-center py-20 space-y-4">
      <UIcon name="i-lucide-shield-x" class="size-12 text-muted" />
      <p class="text-lg text-muted">You do not have admin access.</p>
      <UButton to="/" icon="i-lucide-house" label="Go Home" variant="outline" />
    </div>
  </UContainer>
</template>
