<script setup lang="ts">
definePageMeta({ layout: 'default', title: 'Sign Up' })

const { register } = useApiEndpoints()
const settingsApi = useSettingsApi()
const authStore = useAuthStore()
const router = useRouter()

const form = reactive({ username: '', password: '', confirm: '', email: '' })
const loading = ref(false)
const error = ref('')
const registrationClosed = ref(false)

onMounted(async () => {
  if (authStore.isLoggedIn) { router.replace('/'); return }
  try {
    const settings = await settingsApi.get()
    if (settings?.auth?.allow_registration === false) registrationClosed.value = true
  } catch {}
})

async function handleSignup() {
  error.value = ''
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
    await register(form.username, form.password, form.email || undefined)
    // Fetch the full session instead of using the raw register response,
    // which may have null permissions/preferences on a freshly-created account.
    await authStore.fetchSession()
    router.replace('/')
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Registration failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-[80vh] flex items-center justify-center px-4">
    <div class="w-full max-w-sm space-y-6">
      <div class="text-center space-y-2">
        <UIcon name="i-lucide-user-plus" class="size-10 text-primary mx-auto" />
        <h1 class="text-2xl font-bold text-highlighted">Create Account</h1>
      </div>

      <UCard>
        <div v-if="registrationClosed" class="text-center py-6 space-y-3">
          <UIcon name="i-lucide-user-x" class="size-10 text-muted mx-auto" />
          <p class="text-muted">Registration is currently closed.</p>
          <UButton to="/login" label="Sign In" variant="outline" />
        </div>
        <form v-else class="space-y-4" @submit.prevent="handleSignup">
          <UAlert v-if="error" :title="error" color="error" variant="soft" icon="i-lucide-x-circle" />
          <UFormField label="Username" required>
            <UInput v-model="form.username" placeholder="username" autocomplete="username" required />
          </UFormField>
          <UFormField label="Email">
            <UInput v-model="form.email" type="email" placeholder="user@example.com" />
          </UFormField>
          <UFormField label="Password" required>
            <UInput v-model="form.password" type="password" placeholder="••••••••" required minlength="8" />
          </UFormField>
          <UFormField label="Confirm Password" required>
            <UInput v-model="form.confirm" type="password" placeholder="••••••••" required />
          </UFormField>
          <UButton type="submit" class="w-full justify-center" :loading="loading" label="Create Account" />
        </form>
      </UCard>

      <p class="text-center text-sm text-muted">
        Already have an account?
        <NuxtLink to="/login" class="text-primary hover:underline">Sign in</NuxtLink>
      </p>
    </div>
  </div>
</template>
