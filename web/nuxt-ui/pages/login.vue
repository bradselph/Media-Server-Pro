<script setup lang="ts">
import type { ServerSettings } from '~/types/api'

definePageMeta({ layout: 'default', title: 'Login' })

const authStore = useAuthStore()
const settingsApi = useSettingsApi()
const router = useRouter()
const route = useRoute()

const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')
const allowRegistration = ref(true) // optimistic default until settings load
const allowGuests = computed(() => authStore.allowGuests)

// Redirect if already logged in.
// Only allow same-origin app routes — reject external URLs, protocol-relative
// paths, and API/raw-resource paths to prevent open redirect abuse.
function loginRedirectDest() {
  const r = route.query.redirect
  if (
    typeof r === 'string' &&
    r.startsWith('/') &&
    !r.startsWith('//') &&
    !r.startsWith('/api/') &&
    !r.startsWith('/extractor/')
  ) {
    return r
  }
  return '/'
}

onMounted(async () => {
  if (!authStore.isLoading && authStore.isLoggedIn) {
    router.replace(loginRedirectDest())
    return
  }
  // Fetch server settings to know if registration is open
  try {
    const settings = await settingsApi.get() as ServerSettings
    allowRegistration.value = settings.auth?.allow_registration ?? true
  } catch {
    // Non-critical — keep default (true) so the link stays visible if the call fails
  }
})

async function handleLogin() {
  error.value = ''
  loading.value = true
  try {
    await authStore.login(form.username, form.password)
    router.replace(loginRedirectDest())
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Invalid credentials'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-[80vh] flex items-center justify-center px-4 py-12">
    <div class="w-full max-w-sm">
      <!-- Header -->
      <div class="text-center mb-8">
        <div class="inline-flex items-center justify-center size-16 rounded-full mb-4"
             style="background: var(--accent-bg-weak); border: 1px solid var(--accent-border);">
          <UIcon name="i-lucide-film" class="size-8" style="color: var(--accent-soft);" />
        </div>
        <h1 class="text-2xl font-extrabold text-highlighted">Sign In</h1>
        <p class="text-muted text-sm mt-1">Media Server Pro</p>
      </div>

      <!-- Card -->
      <div class="rounded-xl border border-white/10 bg-elevated p-7 space-y-5">
        <UAlert
          v-if="error"
          :title="error"
          color="error"
          variant="soft"
          icon="i-lucide-x-circle"
        />
        <form class="space-y-4" @submit.prevent="handleLogin">
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Username</label>
            <UInput
              v-model="form.username"
              name="username"
              placeholder="your username"
              autocomplete="username"
              class="w-full"
              required
              autofocus
            />
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Password</label>
            <UInput
              v-model="form.password"
              name="password"
              type="password"
              placeholder="••••••••"
              autocomplete="current-password"
              class="w-full"
              required
            />
          </div>
          <UButton
            type="submit"
            class="w-full justify-center mt-1"
            :loading="loading"
            label="Sign In"
            color="primary"
          />
        </form>
        <UButton
          v-if="allowGuests"
          class="w-full justify-center"
          variant="outline"
          color="neutral"
          icon="i-lucide-eye"
          label="Browse as Guest"
          @click="router.replace('/')"
        />
      </div>

      <p v-if="allowRegistration" class="text-center text-sm text-muted mt-5">
        Don't have an account?
        <NuxtLink to="/signup" class="font-semibold hover:underline" style="color: var(--accent-soft);">Create one</NuxtLink>
      </p>
      <p v-else class="text-center text-sm text-muted mt-5">
        <UIcon name="i-lucide-lock" class="size-3.5 inline-block mr-1 -mt-0.5" />
        Registration is currently closed.
      </p>
    </div>
  </div>
</template>
