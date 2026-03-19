<script setup lang="ts">
const authStore = useAuthStore()

// Check session on app load
onMounted(() => {
  authStore.checkSession()
})
</script>

<template>
  <div class="min-h-screen bg-(--ui-bg)">
    <!-- Top navigation bar -->
    <header class="border-b border-(--ui-border) bg-(--ui-bg-elevated)">
      <UContainer>
        <nav class="flex items-center justify-between h-16">
          <NuxtLink to="/" class="text-xl font-bold text-(--ui-text-highlighted)">
            Media Server Pro
          </NuxtLink>

          <div class="flex items-center gap-2">
            <template v-if="authStore.isAuthenticated">
              <UButton
                to="/profile"
                variant="ghost"
                icon="i-lucide-user"
                label="Profile"
              />
              <UButton
                v-if="authStore.isAdmin"
                to="/admin"
                variant="ghost"
                icon="i-lucide-settings"
                label="Admin"
              />
              <UButton
                variant="soft"
                color="error"
                icon="i-lucide-log-out"
                label="Logout"
                @click="authStore.logout"
              />
            </template>
            <template v-else>
              <UButton
                to="/login"
                variant="ghost"
                label="Login"
              />
              <UButton
                to="/signup"
                variant="soft"
                label="Sign Up"
              />
            </template>
          </div>
        </nav>
      </UContainer>
    </header>

    <!-- Main content -->
    <main>
      <slot />
    </main>
  </div>
</template>
