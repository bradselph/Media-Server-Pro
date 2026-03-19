<script setup lang="ts">
definePageMeta({
  title: 'Profile',
})

const authStore = useAuthStore()
const router = useRouter()

// Redirect to login if not authenticated
watch(() => authStore.isAuthenticated, (authenticated) => {
  if (!authenticated && !authStore.isLoading) {
    router.push('/login')
  }
}, { immediate: true })
</script>

<template>
  <UContainer class="py-8">
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin text-2xl" />
    </div>

    <div v-else-if="authStore.user" class="max-w-2xl mx-auto space-y-6">
      <h1 class="text-2xl font-bold text-(--ui-text-highlighted)">
        Profile
      </h1>

      <!-- User info card -->
      <UCard>
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">
            Account Information
          </h2>
        </template>

        <div class="space-y-3">
          <div class="flex justify-between">
            <span class="text-(--ui-text-muted)">Username</span>
            <span class="font-medium">{{ authStore.user.username }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-(--ui-text-muted)">Role</span>
            <UBadge :color="authStore.isAdmin ? 'primary' : 'neutral'" variant="subtle">
              {{ authStore.user.role }}
            </UBadge>
          </div>
          <div class="flex justify-between">
            <span class="text-(--ui-text-muted)">Member since</span>
            <span class="font-medium">{{ new Date(authStore.user.created_at).toLocaleDateString() }}</span>
          </div>
        </div>
      </UCard>

      <!-- Permissions card -->
      <UCard>
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">
            Permissions
          </h2>
        </template>

        <div class="grid grid-cols-2 gap-3">
          <div
            v-for="(value, key) in authStore.user.permissions"
            :key="key"
            class="flex items-center gap-2"
          >
            <UIcon
              :name="value ? 'i-lucide-check-circle' : 'i-lucide-x-circle'"
              :class="value ? 'text-green-500' : 'text-(--ui-text-dimmed)'"
            />
            <span class="text-sm capitalize">{{ String(key).replace('can_', '').replace('_', ' ') }}</span>
          </div>
        </div>
      </UCard>

      <!-- Preferences placeholder -->
      <UCard>
        <template #header>
          <h2 class="font-semibold text-(--ui-text-highlighted)">
            Preferences
          </h2>
        </template>

        <p class="text-(--ui-text-muted)">
          Preference controls will be implemented here (theme, playback quality, auto-play, etc.)
        </p>
      </UCard>
    </div>
  </UContainer>
</template>
