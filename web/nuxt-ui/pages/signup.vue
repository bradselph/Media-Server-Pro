<script setup lang="ts">
definePageMeta({ layout: 'default', title: 'Sign Up' })

const { register } = useApiEndpoints()
const authStore = useAuthStore()
const router = useRouter()

const form = reactive({ username: '', password: '', confirm: '', email: '' })
const loading = ref(false)
const error = ref('')

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
    const user = await register(form.username, form.password, form.email || undefined)
    authStore.setUser(user)
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
        <h1 class="text-2xl font-bold text-(--ui-text-highlighted)">Create Account</h1>
      </div>

      <UCard>
        <form class="space-y-4" @submit.prevent="handleSignup">
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

      <p class="text-center text-sm text-(--ui-text-muted)">
        Already have an account?
        <NuxtLink to="/login" class="text-primary hover:underline">Sign in</NuxtLink>
      </p>
    </div>
  </div>
</template>
