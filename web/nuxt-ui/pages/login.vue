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

// Redirect if already logged in
function loginRedirectDest() {
  const r = route.query.redirect
  if (typeof r === 'string' && r.startsWith('/') && !r.startsWith('//')) return r
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
  <div class="min-h-[80vh] flex items-center justify-center px-4">
    <div class="w-full max-w-sm space-y-6">
      <div class="text-center space-y-2">
        <UIcon name="i-lucide-film" class="size-10 text-primary mx-auto" />
        <h1 class="text-2xl font-bold text-highlighted">Media Server Pro</h1>
        <p class="text-muted text-sm">Sign in to your account</p>
      </div>

      <UCard>
        <form class="space-y-4" @submit.prevent="handleLogin">
          <UAlert
            v-if="error"
            :title="error"
            color="error"
            variant="soft"
            icon="i-lucide-x-circle"
          />
          <UFormField label="Username">
            <UInput
              v-model="form.username"
              name="username"
              placeholder="your username"
              autocomplete="username"
              required
            />
          </UFormField>
          <UFormField label="Password">
            <UInput
              v-model="form.password"
              name="password"
              type="password"
              placeholder="••••••••"
              autocomplete="current-password"
              required
            />
          </UFormField>
          <UButton
            type="submit"
            class="w-full justify-center"
            :loading="loading"
            label="Sign In"
          />
        </form>
      </UCard>

      <!-- Registration link — hidden when server has disabled new accounts -->
      <UButton
        v-if="allowGuests"
        class="w-full justify-center"
        variant="outline"
        color="neutral"
        icon="i-lucide-eye"
        label="Browse as Guest"
        @click="router.replace('/')"
      />

      <p v-if="allowRegistration" class="text-center text-sm text-muted">
        Don't have an account?
        <NuxtLink to="/signup" class="text-primary hover:underline">Sign up</NuxtLink>
      </p>
      <p v-else class="text-center text-sm text-muted">
        <UIcon name="i-lucide-lock" class="size-3.5 inline-block mr-1 -mt-0.5" />
        Registration is currently closed.
      </p>
    </div>
  </div>
</template>
