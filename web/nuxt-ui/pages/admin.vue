<script setup lang="ts">
definePageMeta({title: 'Admin Panel', middleware: 'admin'})

// Async-load each tab so an admin landing on /admin?tab=dashboard pays for
// only the dashboard chunk, not all tabs. Previously every tab component
// was eagerly bundled into the admin route (~420 kB minified) because Vue's
// template compiler treats `<AdminFooTab>` references as static imports.
// The shell tabs (Media/Library/Ingest/Moderation/System) render their absorbed
// sub-panels via auto-import, so those land in the shell's own async chunk.
const AdminDashboardTab = defineAsyncComponent(() => import('~/components/admin/DashboardTab.vue'))
const AdminUsersTab = defineAsyncComponent(() => import('~/components/admin/UsersTab.vue'))
const AdminMediaTab = defineAsyncComponent(() => import('~/components/admin/MediaTab.vue'))
const AdminLibraryTab = defineAsyncComponent(() => import('~/components/admin/LibraryTab.vue'))
const AdminIngestTab = defineAsyncComponent(() => import('~/components/admin/IngestTab.vue'))
const AdminModerationTab = defineAsyncComponent(() => import('~/components/admin/ModerationTab.vue'))
const AdminAnalyticsTab = defineAsyncComponent(() => import('~/components/admin/AnalyticsTab.vue'))
const AdminSecurityTab = defineAsyncComponent(() => import('~/components/admin/SecurityTab.vue'))
const AdminSystemTab = defineAsyncComponent(() => import('~/components/admin/SystemTab.vue'))

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()

const TABS = [
  {label: 'Dashboard', value: 'dashboard', icon: 'i-lucide-layout-dashboard'},
  {label: 'Users', value: 'users', icon: 'i-lucide-users'},
  {label: 'Media', value: 'media', icon: 'i-lucide-film'},
  {label: 'Library', value: 'library', icon: 'i-lucide-library'},
  {label: 'Ingest', value: 'ingest', icon: 'i-lucide-import'},
  {label: 'Moderation', value: 'moderation', icon: 'i-lucide-shield-check'},
  {label: 'Analytics', value: 'analytics', icon: 'i-lucide-bar-chart-2'},
  {label: 'Security', value: 'security', icon: 'i-lucide-shield'},
  {label: 'System', value: 'system', icon: 'i-lucide-settings'},
]

const VALID = TABS.map(t => t.value)
// Legacy tab name aliases — redirect old bookmarks/links to the tab that now
// owns that surface. The target shell reads ?tab= to open the matching sub-tab.
const TAB_ALIASES: Record<string, string> = {
  settings: 'system',
  streaming: 'system',
  updates: 'system',
  categories: 'library',
  collections: 'library',
  playlists: 'library',
  duplicates: 'media',
  downloader: 'ingest',
  sources: 'ingest',
  content: 'moderation',
  discovery: 'moderation',
  reports: 'moderation',
}
const activeTab = ref(
    VALID.includes(route.query.tab as string)
        ? (route.query.tab as string)
        : TAB_ALIASES[route.query.tab as string] ?? 'dashboard',
)

watch(activeTab, tab => {
  router.replace({query: {tab}})
})
</script>

<template>
  <UContainer class="py-6">
    <!-- Loading -->
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary"/>
    </div>

    <div v-else-if="authStore.isAdmin" class="space-y-4">
      <!-- Header -->
      <div class="flex items-center justify-between flex-wrap gap-3">
        <h1 class="text-2xl font-bold text-highlighted flex items-center gap-2">
          <UIcon name="i-lucide-shield" class="size-6 text-primary"/>
          Admin Panel
        </h1>
        <UButton to="/" icon="i-lucide-house" label="Home" variant="ghost" color="neutral" size="sm"/>
      </div>

      <!-- Sidebar layout -->
      <div class="flex gap-4 items-start">
        <!-- Mobile: horizontal scrollable pill nav -->
        <div class="md:hidden w-full flex gap-1 overflow-x-auto pb-1 scrollbar-hide">
          <UButton
              v-for="tab in TABS"
              :key="tab.value"
              :icon="tab.icon"
              :label="tab.label"
              size="xs"
              :variant="activeTab === tab.value ? 'solid' : 'ghost'"
              :color="activeTab === tab.value ? 'primary' : 'neutral'"
              class="shrink-0"
              @click="() => { activeTab = tab.value }"
          />
        </div>

        <!-- Desktop: vertical sidebar — 196px width per handoff §6.7. -->
        <nav class="hidden md:flex flex-col gap-0.5 w-[196px] shrink-0 sticky top-4">
          <button
              v-for="tab in TABS"
              :key="tab.value"
              class="flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm font-medium w-full text-left transition-colors"
              :class="activeTab === tab.value
              ? 'bg-primary text-white'
              : 'text-muted hover:bg-elevated hover:text-highlighted'"
              @click="activeTab = tab.value"
          >
            <UIcon :name="tab.icon" class="size-4 shrink-0"/>
            <span>{{ tab.label }}</span>
          </button>
        </nav>

        <!-- Content panel -->
        <div class="min-w-0 flex-1 min-h-64">
          <AdminDashboardTab v-if="activeTab === 'dashboard'"/>
          <AdminUsersTab v-else-if="activeTab === 'users'"/>
          <AdminMediaTab v-else-if="activeTab === 'media'"/>
          <AdminLibraryTab v-else-if="activeTab === 'library'"/>
          <AdminIngestTab v-else-if="activeTab === 'ingest'"/>
          <AdminModerationTab v-else-if="activeTab === 'moderation'"/>
          <AdminAnalyticsTab v-else-if="activeTab === 'analytics'"/>
          <AdminSecurityTab v-else-if="activeTab === 'security'"/>
          <AdminSystemTab v-else-if="activeTab === 'system'"/>
        </div>
      </div>
    </div>

    <!-- Non-admin fallback -->
    <div v-else class="flex flex-col items-center justify-center py-20 space-y-4">
      <UIcon name="i-lucide-shield-x" class="size-12 text-muted"/>
      <p class="text-lg text-muted">You do not have admin access.</p>
      <UButton to="/" icon="i-lucide-house" label="Go Home" variant="outline"/>
    </div>
  </UContainer>
</template>
