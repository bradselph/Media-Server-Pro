<script setup lang="ts">
definePageMeta({
  title: 'Admin Panel',
})

const authStore = useAuthStore()
const router = useRouter()

// Redirect non-admins
watch(() => [authStore.isAdmin, authStore.isLoading], ([isAdmin, isLoading]) => {
  if (!isLoading && !isAdmin) {
    router.push('/login')
  }
}, { immediate: true })

const tabs = [
  { label: 'Dashboard', icon: 'i-lucide-layout-dashboard', value: 'dashboard' },
  { label: 'Users', icon: 'i-lucide-users', value: 'users' },
  { label: 'Media', icon: 'i-lucide-film', value: 'media' },
  { label: 'Streaming', icon: 'i-lucide-radio', value: 'streaming' },
  { label: 'Analytics', icon: 'i-lucide-bar-chart-3', value: 'analytics' },
  { label: 'Content', icon: 'i-lucide-shield', value: 'content' },
  { label: 'Sources', icon: 'i-lucide-server', value: 'sources' },
  { label: 'Playlists', icon: 'i-lucide-list-music', value: 'playlists' },
  { label: 'Security', icon: 'i-lucide-lock', value: 'security' },
  { label: 'Updates', icon: 'i-lucide-download', value: 'updates' },
]

const activeTab = ref('dashboard')
</script>

<template>
  <UContainer class="py-8">
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin text-2xl" />
    </div>

    <div v-else-if="authStore.isAdmin" class="space-y-6">
      <h1 class="text-2xl font-bold text-(--ui-text-highlighted)">
        Admin Panel
      </h1>

      <UTabs
        v-model="activeTab"
        :items="tabs"
        orientation="horizontal"
      />

      <!-- Tab content -->
      <UCard>
        <div v-if="activeTab === 'dashboard'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">
            Dashboard
          </h2>
          <p class="text-(--ui-text-muted)">
            Server statistics, system health, and quick actions will be displayed here.
          </p>
          <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
            <UCard v-for="stat in ['Total Media', 'Active Users', 'Active Streams']" :key="stat">
              <p class="text-sm text-(--ui-text-muted)">{{ stat }}</p>
              <p class="text-2xl font-bold text-(--ui-text-highlighted)">--</p>
            </UCard>
          </div>
        </div>

        <div v-else-if="activeTab === 'users'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">User Management</h2>
          <p class="text-(--ui-text-muted)">User list, roles, and permissions management will be implemented here.</p>
        </div>

        <div v-else-if="activeTab === 'media'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Media Management</h2>
          <p class="text-(--ui-text-muted)">Media scanning, metadata editing, and library management will be implemented here.</p>
        </div>

        <div v-else-if="activeTab === 'streaming'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Streaming</h2>
          <p class="text-(--ui-text-muted)">HLS settings, active streams, and quality profiles will be managed here.</p>
        </div>

        <div v-else-if="activeTab === 'analytics'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Analytics</h2>
          <p class="text-(--ui-text-muted)">Playback analytics, user activity, and usage statistics will be displayed here.</p>
        </div>

        <div v-else-if="activeTab === 'content'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Content Review</h2>
          <p class="text-(--ui-text-muted)">Mature content scanning, categorization, and moderation tools will be implemented here.</p>
        </div>

        <div v-else-if="activeTab === 'sources'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Media Sources</h2>
          <p class="text-(--ui-text-muted)">Remote sources, slave nodes, HLS jobs, and crawlers will be managed here.</p>
        </div>

        <div v-else-if="activeTab === 'playlists'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Playlists</h2>
          <p class="text-(--ui-text-muted)">Global playlist management and statistics will be displayed here.</p>
        </div>

        <div v-else-if="activeTab === 'security'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Security</h2>
          <p class="text-(--ui-text-muted)">IP blocking, rate limiting, and audit logs will be managed here.</p>
        </div>

        <div v-else-if="activeTab === 'updates'" class="space-y-4">
          <h2 class="text-lg font-semibold text-(--ui-text-highlighted)">Updates</h2>
          <p class="text-(--ui-text-muted)">Version info and update management will be displayed here.</p>
        </div>
      </UCard>
    </div>
  </UContainer>
</template>
