<script setup lang="ts">
definePageMeta({
  title: 'Sign Up',
})

const { login, register } = useApiEndpoints()
const authStore = useAuthStore()
const router = useRouter()

const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  email: '',
})
const error = ref('')
const loading = ref(false)

async function handleSignup() {
  error.value = ''

  if (form.password !== form.confirmPassword) {
    error.value = 'Passwords do not match'
    return
  }

  loading.value = true
  try {
    await register(form.username, form.password, form.email || undefined)
    // Auto-login after registration
    await authStore.login(form.username, form.password)
    await router.push('/')
  } catch (err: any) {
    error.value = err?.message || 'Registration failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <UContainer class="py-16 flex justify-center">
    <UCard class="w-full max-w-md">
      <template #header>
        <h1 class="text-xl font-bold text-(--ui-text-highlighted) text-center">
          Create Account
        </h1>
      </template>

      <form class="space-y-4" @submit.prevent="handleSignup">
        <UFormField label="Username">
          <UInput
            v-model="form.username"
            placeholder="Choose a username"
            icon="i-lucide-user"
            autofocus
            required
          />
        </UFormField>

        <UFormField label="Email (optional)">
          <UInput
            v-model="form.email"
            type="email"
            placeholder="Email address"
            icon="i-lucide-mail"
          />
        </UFormField>

        <UFormField label="Password">
          <UInput
            v-model="form.password"
            type="password"
            placeholder="Choose a password"
            icon="i-lucide-lock"
            required
          />
        </UFormField>

        <UFormField label="Confirm Password">
          <UInput
            v-model="form.confirmPassword"
            type="password"
            placeholder="Confirm password"
            icon="i-lucide-lock"
            required
          />
        </UFormField>

        <UAlert
          v-if="error"
          color="error"
          icon="i-lucide-alert-circle"
          :title="error"
        />

        <UButton
          type="submit"
          block
          :loading="loading"
          label="Create Account"
        />
      </form>

      <template #footer>
        <p class="text-center text-sm text-(--ui-text-muted)">
          Already have an account?
          <NuxtLink to="/login" class="text-(--ui-text-highlighted) hover:underline">
            Sign in
          </NuxtLink>
        </p>
      </template>
    </UCard>
  </UContainer>
</template>
