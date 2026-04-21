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
// Resolves brand config at runtime — prefers window.APP_CONFIG (deploy-time
// injection), falls back to Nuxt app.config, then hard-coded defaults.
const brand = useBrandConfig()

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

// AgeGate "Leave" action per handoff §6.9. Users who aren't 18+ or don't
// want to confirm need an explicit escape route rather than being forced
// to close the browser tab. Prefer history.back() if there's a prior page
// in the session; otherwise redirect to an unauthenticated safe page.
function leaveAgeGate() {
  if (typeof window === 'undefined') return
  // If the user arrived via a link and has history, go back one step.
  // Otherwise, navigate to a generic external placeholder (about:blank is
  // the safest stub that doesn't send the user anywhere unexpected).
  if (window.history.length > 1) {
    window.history.back()
  } else {
    window.location.href = 'about:blank'
  }
}

onMounted(checkAgeGate)
onMounted(() => { versionApi.get().then(r => { serverVersion.value = r.version }).catch(() => {}) })
onMounted(fetchNewCount)
onMounted(() => {
  const saved = localStorage.getItem('msp-accent-hue')
  if (saved) document.documentElement.style.setProperty('--accent-hue', saved)
})

useHead({
  title: computed(() => {
    const pageTitle = route.meta.title as string | undefined
    return pageTitle ? `${pageTitle} — ${brand.value.name}` : brand.value.name
  }),
})

async function handleLogout() {
  await authStore.logout()
  router.push('/login')
}

const mobileMenuOpen = ref(false)
const shortcutsModal = ref<{ open: boolean } | null>(null)

// Main desktop nav — content discovery only. Per handoff §6.1 the main nav
// is deliberately compact ("Home, Browse, Admin" in the spec). Personal items
// (Profile, Favorites, History, Admin) now live in the avatar dropdown to
// reduce nav density and make the library pages the primary tabs.
const navLinks = computed(() => {
  const links = [
    { label: 'Home', to: '/', icon: 'i-lucide-house' },
  ]
  if (authStore.isLoggedIn) {
    links.push(
      { label: 'Categories', to: '/categories', icon: 'i-lucide-layers' },
      { label: 'Playlists', to: '/playlists', icon: 'i-lucide-list-music' },
    )
    if (authStore.user?.permissions?.can_upload) {
      links.push({ label: 'Upload', to: '/upload', icon: 'i-lucide-upload' })
    }
  }
  return links
})

// Mobile menu still shows everything (single-column vertical list is not dense).
const mobileNavLinks = computed(() => {
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

// Avatar dropdown menu items — per handoff §6.1: Profile, Admin (if admin),
// Logout. Nuxt UI's UDropdownMenu expects a 2D array of groups; each group
// renders as a section with a separator between.
const avatarMenuItems = computed(() => {
  const primary: Array<{ label: string; icon: string; to?: string; onSelect?: () => void }> = [
    { label: 'Profile', icon: 'i-lucide-user', to: '/profile' },
    { label: 'Favorites', icon: 'i-lucide-heart', to: '/favorites' },
    { label: 'History', icon: 'i-lucide-history', to: '/history' },
  ]
  if (authStore.isAdmin) {
    primary.push({ label: 'Admin', icon: 'i-lucide-shield', to: '/admin' })
  }
  return [
    primary,
    [{ label: 'Log out', icon: 'i-lucide-log-out', onSelect: handleLogout }],
  ]
})

const navSearch = ref('')

function handleNavSearch() {
  const q = navSearch.value.trim()
  if (!q) return
  router.push({ path: '/', query: { search: q } })
  navSearch.value = ''
  mobileMenuOpen.value = false
}
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
    <header v-if="ageGateChecked && !ageGateOpen" class="border-b border-[var(--hairline)] bg-[var(--surface-page)] sticky top-0 z-40">
      <UContainer class="flex items-center justify-between h-[60px] gap-4">
        <!-- Brand — per handoff §6.1: 28×28 gradient-filled square logo (rounded 6px),
             brand name 17px/800, tagline 10px/500 uppercase muted. Name/tagline/
             gradient are resolved via useBrandConfig (window.APP_CONFIG at deploy
             → app.config.ts at build → hard-coded fallbacks). When brand.gradient
             is empty, the logo uses the accent-hue-derived OKLCH gradient so a
             rebrand can skip the gradient and still look cohesive. -->
        <NuxtLink to="/" class="flex items-center gap-2.5 no-underline shrink-0" :aria-label="`${brand.name} — Home`">
          <span
            class="inline-flex items-center justify-center size-7 rounded-md text-white shadow-sm"
            :style="{ background: brand.gradient || 'linear-gradient(135deg, oklch(62% 0.13 var(--accent-hue)), oklch(72% 0.13 calc(var(--accent-hue) + 40)))' }"
            aria-hidden="true"
          >
            <UIcon name="i-lucide-film" class="size-4" />
          </span>
          <span class="flex flex-col leading-tight">
            <span class="text-[17px] font-extrabold text-highlighted">{{ brand.name }}</span>
            <span class="text-[10px] font-medium text-muted uppercase tracking-[0.1em]">{{ brand.tagline }}</span>
          </span>
        </NuxtLink>

        <!-- Desktop nav — per handoff §6.1: underline-on-active pattern, text #888
             default / #eee active / --accent underline. The `exact-active-class`
             prop ensures only the exact-match route renders the underline (so
             Home doesn't stay lit when on a sub-page). -->
        <nav class="hidden md:flex items-center gap-1">
          <NuxtLink
            v-for="link in navLinks"
            :key="link.to"
            :to="link.to"
            class="nav-link relative flex items-center gap-1.5 px-3 py-1.5 text-sm text-muted hover:text-default transition-colors"
            active-class="nav-link--active text-default"
            :exact-active-class="link.to === '/' ? 'nav-link--active text-default' : ''"
          >
            <UIcon :name="link.icon" class="size-4" />
            {{ link.label }}
            <span
              v-if="link.to === '/' && newCount > 0"
              class="absolute -top-0.5 -right-0.5 min-w-4 h-4 px-0.5 rounded-full bg-primary text-white text-[10px] font-bold flex items-center justify-center"
            >{{ newCount > 99 ? '99+' : newCount }}</span>
          </NuxtLink>
        </nav>

        <!-- Nav search (desktop) -->
        <form class="hidden md:flex items-center flex-1 max-w-xs" @submit.prevent="handleNavSearch">
          <UInput
            v-model="navSearch"
            icon="i-lucide-search"
            placeholder="Search titles, tags..."
            size="sm"
            class="w-full"
            type="search"
          />
        </form>

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

          <!-- Avatar dropdown (logged-in) — per handoff §6.1: circle w/ initial
               letter on an accent gradient, opens popover with Profile, Admin,
               Logout. Closes on outside-click and Escape via UDropdownMenu. -->
          <template v-if="authStore.isLoggedIn">
            <UDropdownMenu
              :items="avatarMenuItems"
              :content="{ align: 'end' }"
              class="hidden md:block"
            >
              <button
                class="inline-flex items-center justify-center size-8 rounded-full text-white text-sm font-bold cursor-pointer hover:brightness-110 transition-[filter] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--surface-page)]"
                style="background: linear-gradient(135deg, oklch(62% 0.13 var(--accent-hue)), oklch(72% 0.13 calc(var(--accent-hue) + 40)));"
                :aria-label="`Account menu for ${authStore.username}`"
              >
                {{ (authStore.username || '?').charAt(0).toUpperCase() }}
              </button>
            </UDropdownMenu>
          </template>
          <template v-else>
            <UButton to="/login" variant="ghost" color="neutral" size="sm" label="Sign in" class="hidden md:flex" />
            <UButton to="/signup" color="primary" size="sm" label="Sign up" class="hidden md:flex" />
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
            v-for="link in mobileNavLinks"
            :key="link.to"
            :to="link.to"
            class="flex items-center gap-2 px-3 py-2.5 rounded-md text-sm text-muted hover:text-default hover:bg-muted transition-colors"
            active-class="text-default bg-muted"
          >
            <UIcon :name="link.icon" class="size-4 shrink-0" />
            {{ link.label }}
          </NuxtLink>
          <!-- Mobile search -->
          <form class="px-1 py-2" @submit.prevent="handleNavSearch">
            <UInput
              v-model="navSearch"
              icon="i-lucide-search"
              placeholder="Search media…"
              size="sm"
              type="search"
            />
          </form>
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
          <p v-if="serverVersion" class="text-xs text-muted">{{ brand.name }} v{{ serverVersion }}</p>
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
        <div class="flex flex-wrap items-center justify-end gap-2 w-full">
          <!-- Leave button per handoff §6.9 — gives users who aren't 18+ or
               don't want to confirm an explicit escape route. -->
          <UButton
            icon="i-lucide-log-out"
            label="Leave"
            variant="ghost"
            color="neutral"
            :disabled="ageGateVerifying"
            @click="leaveAgeGate"
          />
          <UButton
            :loading="ageGateVerifying"
            :disabled="!ageGateTermsAccepted"
            icon="i-lucide-check"
            label="I confirm I am 18 or older"
            color="primary"
            @click="verifyAge"
          />
        </div>
      </template>
    </UModal>
  </div>
</template>
