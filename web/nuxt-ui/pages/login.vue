<script setup lang="ts">
definePageMeta({ layout: 'default', title: 'Login' })

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')

// Redirect if already logged in
onMounted(async () => {
  if (!authStore.isLoading && authStore.isLoggedIn) {
    router.replace((route.query.redirect as string) || '/')
  }
})

async function handleLogin() {
  error.value = ''
  loading.value = true
  try {
    await authStore.login(form.username, form.password)
    router.replace((route.query.redirect as string) || '/')
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
              placeholder="your username"
              autocomplete="username"
              required
            />
          </UFormField>
          <UFormField label="Password">
            <UInput
              v-model="form.password"
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

      <p class="text-center text-sm text-muted">
        Don't have an account?
        <NuxtLink to="/signup" class="text-primary hover:underline">Sign up</NuxtLink>
      </p>
    </div>
  </div>
</template>
