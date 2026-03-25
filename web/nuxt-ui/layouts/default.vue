<script setup lang="ts">
const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()
const colorMode = useColorMode()

useHead({
  title: computed(() => {
    const pageTitle = route.meta.title as string | undefined
    return pageTitle ? `${pageTitle} — Media Server Pro` : 'Media Server Pro'
  }),
})

async function handleLogout() {
  await authStore.logout()
  router.push('/login')
}

const navLinks = computed(() => {
  const links = [
    { label: 'Home', to: '/', icon: 'i-lucide-house' },
  ]
  if (authStore.isLoggedIn) {
    links.push({ label: 'Playlists', to: '/playlists', icon: 'i-lucide-list-music' })
    if (authStore.user?.permissions?.can_upload) {
      links.push({ label: 'Upload', to: '/upload', icon: 'i-lucide-upload' })
    }
    links.push({ label: 'Profile', to: '/profile', icon: 'i-lucide-user' })
    if (authStore.isAdmin) {
      links.push({ label: 'Admin', to: '/admin', icon: 'i-lucide-shield' })
    }
  }
  return links
})
</script>

<template>
  <div class="min-h-screen bg-default text-default">
    <!-- Nav -->
    <header class="border-b border-default bg-elevated sticky top-0 z-40">
      <UContainer class="flex items-center justify-between h-14 gap-4">
        <NuxtLink to="/" class="font-bold text-lg text-highlighted flex items-center gap-2">
          <UIcon name="i-lucide-film" class="size-5 text-primary" />
          Media Server Pro
        </NuxtLink>

        <nav class="hidden md:flex items-center gap-1">
          <NuxtLink
            v-for="link in navLinks"
            :key="link.to"
            :to="link.to"
            class="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm text-muted hover:text-default hover:bg-muted transition-colors"
            active-class="text-default bg-muted"
          >
            <UIcon :name="link.icon" class="size-4" />
            {{ link.label }}
          </NuxtLink>
        </nav>

        <div class="flex items-center gap-2">
          <UButton
            :icon="colorMode.value === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
            :aria-label="colorMode.value === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
            variant="ghost"
            color="neutral"
            size="sm"
            @click="colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'"
          />

          <template v-if="authStore.isLoggedIn">
            <UButton
              variant="ghost"
              color="neutral"
              size="sm"
              icon="i-lucide-log-out"
              aria-label="Log out"
              @click="handleLogout"
            />
          </template>
          <template v-else>
            <UButton to="/login" variant="ghost" color="neutral" size="sm" label="Login" />
          </template>
        </div>
      </UContainer>
    </header>

    <!-- Page content -->
    <main>
      <slot />
    </main>
  </div>
</template>
