<script setup lang="ts">
import { useSuggestionsApi } from '~/composables/useApiEndpoints'

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()
const colorMode = useColorMode()
const ageGateApi = useAgeGateApi()
const versionApi = useVersionApi()
const serverVersion = ref('')
const suggestionsApi = useSuggestionsApi()
const newCount = ref(0)

async function fetchNewCount() {
  if (!authStore.isLoggedIn) return
  try {
    const resp = await suggestionsApi.getNewSinceLastVisit(1)
    newCount.value = resp.total
  } catch { /* non-critical */ }
}

const ageGateOpen = ref(false)
const ageGateChecked = ref(false)
const ageGateVerifying = ref(false)
const ageGateTermsAccepted = ref(false)

async function checkAgeGate() {
  try {
    const status = await ageGateApi.getStatus()
    if (status.enabled && !status.verified) {
      ageGateOpen.value = true
    }
  } catch { /* non-critical */ }
  finally { ageGateChecked.value = true }
}

async function verifyAge() {
  if (!ageGateTermsAccepted.value) return
  ageGateVerifying.value = true
  try {
    await ageGateApi.verify()
    ageGateOpen.value = false
  } catch { /* if verify fails, keep modal open */ }
  finally { ageGateVerifying.value = false }
}

onMounted(checkAgeGate)
onMounted(() => { versionApi.get().then(r => { serverVersion.value = r.version }).catch(() => {}) })
onMounted(fetchNewCount)

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

const mobileMenuOpen = ref(false)
const shortcutsModal = ref<{ open: boolean } | null>(null)

const navLinks = computed(() => {
  const links = [
    { label: 'Home', to: '/', icon: 'i-lucide-house' },
  ]
  if (authStore.isLoggedIn) {
    links.push(
      { label: 'Categories', to: '/categories', icon: 'i-lucide-layers' },
      { label: 'Playlists', to: '/playlists', icon: 'i-lucide-list-music' },
      { label: 'Favorites', to: '/favorites', icon: 'i-lucide-heart' },
      { label: 'History', to: '/history', icon: 'i-lucide-history' },
    )
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

// Close the mobile menu when the route changes (user tapped a link)
watch(() => route.path, () => { mobileMenuOpen.value = false })
</script>

<template>
  <div class="min-h-screen bg-default text-default">
    <a href="#main-content" class="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:top-2 focus:left-2 focus:px-4 focus:py-2 focus:bg-primary focus:text-white focus:rounded-md focus:text-sm focus:font-medium">
      Skip to main content
    </a>
    <!-- Full-page gate: nothing is rendered until the age-gate check resolves.
         Once resolved, if the gate is open the modal covers everything.
         Content only appears after the gate is cleared. -->
    <div v-if="!ageGateChecked || ageGateOpen" class="fixed inset-0 z-40 bg-default" />

    <!-- Nav -->
    <header v-if="ageGateChecked && !ageGateOpen" class="border-b border-default bg-elevated sticky top-0 z-40">
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
            class="relative flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm text-muted hover:text-default hover:bg-muted transition-colors"
            active-class="text-default bg-muted"
          >
            <UIcon :name="link.icon" class="size-4" />
            {{ link.label }}
            <span
              v-if="link.to === '/' && newCount > 0"
              class="absolute -top-0.5 -right-0.5 min-w-4 h-4 px-0.5 rounded-full bg-primary text-white text-[10px] font-bold flex items-center justify-center"
            >{{ newCount > 99 ? '99+' : newCount }}</span>
          </NuxtLink>
        </nav>

        <div class="flex items-center gap-2">
          <UButton
            icon="i-lucide-keyboard"
            aria-label="Keyboard shortcuts"
            variant="ghost"
            color="neutral"
            size="sm"
            class="hidden md:flex"
            @click="() => { if (shortcutsModal) shortcutsModal.open = !shortcutsModal.open }"
          />
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
              class="hidden md:flex"
              @click="handleLogout"
            />
          </template>
          <template v-else>
            <UButton to="/login" variant="ghost" color="neutral" size="sm" label="Login" class="hidden md:flex" />
          </template>

          <!-- Hamburger — mobile only -->
          <UButton
            :icon="mobileMenuOpen ? 'i-lucide-x' : 'i-lucide-menu'"
            :aria-label="mobileMenuOpen ? 'Close menu' : 'Open menu'"
            variant="ghost"
            color="neutral"
            size="sm"
            class="md:hidden"
            @click="mobileMenuOpen = !mobileMenuOpen"
          />
        </div>
      </UContainer>

      <!-- Mobile nav dropdown -->
      <div v-if="mobileMenuOpen" class="md:hidden border-t border-default bg-elevated">
        <UContainer class="py-2 flex flex-col gap-1">
          <NuxtLink
            v-for="link in navLinks"
            :key="link.to"
            :to="link.to"
            class="flex items-center gap-2 px-3 py-2.5 rounded-md text-sm text-muted hover:text-default hover:bg-muted transition-colors"
            active-class="text-default bg-muted"
          >
            <UIcon :name="link.icon" class="size-4 shrink-0" />
            {{ link.label }}
          </NuxtLink>
          <div class="border-t border-default mt-1 pt-1">
            <template v-if="authStore.isLoggedIn">
              <button
                class="flex w-full items-center gap-2 px-3 py-2.5 rounded-md text-sm text-muted hover:text-default hover:bg-muted transition-colors"
                @click="handleLogout"
              >
                <UIcon name="i-lucide-log-out" class="size-4 shrink-0" />
                Log out
              </button>
            </template>
            <template v-else>
              <NuxtLink
                to="/login"
                class="flex items-center gap-2 px-3 py-2.5 rounded-md text-sm text-muted hover:text-default hover:bg-muted transition-colors"
              >
                <UIcon name="i-lucide-log-in" class="size-4 shrink-0" />
                Login
              </NuxtLink>
            </template>
          </div>
        </UContainer>
      </div>
    </header>

    <!-- Page content -->
    <main id="main-content" v-if="ageGateChecked && !ageGateOpen">
      <slot />
    </main>

    <!-- Footer -->
    <footer v-if="ageGateChecked && !ageGateOpen" class="border-t border-default py-3">
      <UContainer>
        <div class="flex flex-col items-center gap-1">
          <p v-if="serverVersion" class="text-xs text-muted">Media Server Pro v{{ serverVersion }}</p>
          <div class="flex items-center gap-3 text-xs text-muted">
            <NuxtLink to="/privacy" class="hover:text-default underline">Privacy Policy</NuxtLink>
            <span aria-hidden="true">·</span>
            <NuxtLink to="/terms" class="hover:text-default underline">Terms of Service</NuxtLink>
          </div>
        </div>
      </UContainer>
    </footer>

    <!-- Mini player (appears when navigating away from player) -->
    <MiniPlayer />

    <!-- Global keyboard shortcuts reference (press ? anywhere) -->
    <KeyboardShortcutsModal ref="shortcutsModal" />

    <!-- Cookie consent banner -->
    <CookieConsentBanner />

    <!-- Age gate modal -->
    <UModal
      :open="ageGateOpen"
      :dismissible="false"
      title="Age Verification Required"
      description="This site contains mature content. You must be 18 or older to continue."
    >
      <template #body>
        <div class="px-4 pb-2 space-y-4">
          <p class="text-sm text-muted">
            By continuing you confirm that you are at least 18 years old, that viewing adult content
            is permitted in your jurisdiction, and that you agree to our
            <NuxtLink to="/terms" class="underline hover:text-default" target="_blank">Terms of Service</NuxtLink>
            and
            <NuxtLink to="/privacy" class="underline hover:text-default" target="_blank">Privacy Policy</NuxtLink>.
          </p>
          <label class="flex items-start gap-3 cursor-pointer select-none">
            <UCheckbox
              v-model="ageGateTermsAccepted"
              aria-label="I confirm I am 18 or older and agree to the Terms of Service and Privacy Policy"
            />
            <span class="text-sm text-default leading-snug">
              I confirm I am 18 or older and agree to the Terms of Service and Privacy Policy.
            </span>
          </label>
        </div>
      </template>
      <template #footer>
        <UButton
          :loading="ageGateVerifying"
          :disabled="!ageGateTermsAccepted"
          icon="i-lucide-check"
          label="I confirm I am 18 or older"
          color="primary"
          @click="verifyAge"
        />
      </template>
    </UModal>
  </div>
</template>
