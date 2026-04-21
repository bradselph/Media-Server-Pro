<script setup lang="ts">
definePageMeta({ layout: 'default', title: 'Sign Up' })

const { register, getRegistrationToken } = useApiEndpoints()
const settingsApi = useSettingsApi()
const authStore = useAuthStore()
const router = useRouter()

const form = reactive({ username: '', password: '', confirm: '', email: '' })
const loading = ref(false)
const error = ref('')
const registrationClosed = ref(false)
const regToken = ref('')

onMounted(async () => {
  if (authStore.isLoggedIn) { router.replace('/'); return }
  try {
    const settings = await settingsApi.get()
    if (settings?.auth?.allow_registration === false) {
      registrationClosed.value = true
      return
    }
  } catch {}
  try {
    const res = await getRegistrationToken()
    regToken.value = res.token
  } catch {
    error.value = 'Unable to load registration form. Please refresh and try again.'
  }
})

async function handleSignup() {
  error.value = ''
  if (!form.username.trim()) {
    error.value = 'Username is required'
    return
  }
  if (form.username.trim().length < 3) {
    error.value = 'Username must be at least 3 characters'
    return
  }
  if (form.password !== form.confirm) {
    error.value = 'Passwords do not match'
    return
  }
  if (form.password.length < 8) {
    error.value = 'Password must be at least 8 characters'
    return
  }
  loading.value = true
  try {
    await register(form.username, form.password, regToken.value, form.email || undefined)
    // Fetch the full session instead of using the raw register response,
    // which may have null permissions/preferences on a freshly-created account.
    await authStore.fetchSession()
    router.replace('/')
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Registration failed'
    // Token is consumed on any attempt (success or server-side failure after validation).
    // Fetch a fresh one so the user can retry without reloading.
    try {
      const res = await getRegistrationToken()
      regToken.value = res.token
    } catch {}
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
          <UIcon name="i-lucide-user-plus" class="size-8" style="color: var(--accent-soft);" />
        </div>
        <h1 class="text-2xl font-extrabold text-highlighted">Create Account</h1>
        <p class="text-muted text-sm mt-1">Media Server Pro</p>
      </div>

      <!-- Card -->
      <div class="rounded-xl border border-[var(--hairline)] bg-[var(--surface-card)] p-7">
        <div v-if="registrationClosed" class="text-center py-6 space-y-4">
          <UIcon name="i-lucide-user-x" class="size-10 text-muted mx-auto" />
          <p class="text-muted">Registration is currently closed.</p>
          <UButton to="/login" label="Sign In" variant="outline" />
        </div>
        <form v-else class="space-y-4" @submit.prevent="handleSignup">
          <UAlert v-if="error" :title="error" color="error" variant="soft" icon="i-lucide-x-circle" />
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Username <span class="text-red-400">*</span></label>
            <UInput v-model="form.username" name="username" placeholder="username" autocomplete="username" class="w-full" required autofocus />
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Email <span class="text-muted opacity-60 normal-case font-normal">(optional)</span></label>
            <UInput v-model="form.email" name="email" type="email" placeholder="user@example.com" autocomplete="email" class="w-full" />
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Password <span class="text-red-400">*</span></label>
            <UInput v-model="form.password" name="new-password" type="password" placeholder="••••••••" autocomplete="new-password" class="w-full" required minlength="8" />
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Confirm Password <span class="text-red-400">*</span></label>
            <UInput v-model="form.confirm" name="confirm-password" type="password" placeholder="••••••••" autocomplete="new-password" class="w-full" required />
          </div>
          <UButton type="submit" class="w-full justify-center mt-1" :loading="loading" :disabled="!regToken" label="Create Account" color="primary" />
        </form>
      </div>

      <p class="text-center text-sm text-muted mt-5">
        Already have an account?
        <NuxtLink to="/login" class="font-semibold hover:underline" style="color: var(--accent-soft);">Sign in</NuxtLink>
      </p>
    </div>
  </div>
</template>
