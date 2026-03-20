<script setup lang="ts">
import { ApiError } from '~/composables/useApi'

definePageMeta({
  title: 'Sign Up',
})

const { register } = useApiEndpoints()
const authStore = useAuthStore()
const router = useRouter()

const form = reactive({
  username: '',
  email: '',
  password: '',
  confirmPassword: '',
})
const error = ref('')
const loading = ref(false)

/** Client-side validation before submitting. */
function validate(): string | null {
  const username = form.username.trim()
  if (username.length < 3 || username.length > 64) {
    return 'Username must be between 3 and 64 characters'
  }
  if (form.password.length < 8) {
    return 'Password must be at least 8 characters'
  }
  if (form.password !== form.confirmPassword) {
    return 'Passwords do not match'
  }
  return null
}

async function handleSignup() {
  error.value = ''

  const validationError = validate()
  if (validationError) {
    error.value = validationError
    return
  }

  loading.value = true
  try {
    // Backend creates user + session cookie
    await register(form.username.trim(), form.password, form.email.trim() || undefined)
    // Fetch full user data via checkSession (same flow as login)
    await authStore.checkSession()
    await router.replace('/')
  } catch (err: unknown) {
    if (err instanceof ApiError) {
      error.value = err.message
    } else {
      error.value = 'Registration failed. Please try again.'
    }
  } finally {
    loading.value = false
  }
}

/** Computed password strength hint. */
const passwordHint = computed(() => {
  if (!form.password) return ''
  if (form.password.length < 8) return `${8 - form.password.length} more characters needed`
  return ''
})

/** Computed match indicator. */
const passwordsMatch = computed(() => {
  if (!form.confirmPassword) return true
  return form.password === form.confirmPassword
})
</script>

<template>
  <UContainer class="py-16 flex justify-center">
    <UCard class="w-full max-w-md">
      <template #header>
        <div class="space-y-2 text-center">
          <NuxtLink
            to="/"
            class="inline-flex items-center gap-1 text-sm text-(--ui-text-muted) hover:text-(--ui-text-highlighted) transition-colors"
          >
            <UIcon name="i-lucide-arrow-left" class="size-4" />
            Back to Library
          </NuxtLink>
          <h1 class="text-xl font-bold text-(--ui-text-highlighted)">
            Create Account
          </h1>
          <p class="text-sm text-(--ui-text-muted)">
            Register for a new media server account
          </p>
        </div>
      </template>

      <form class="space-y-4" @submit.prevent="handleSignup">
        <UFormField label="Username" required>
          <UInput
            v-model="form.username"
            placeholder="Choose a username (3-64 characters)"
            icon="i-lucide-user"
            autocomplete="username"
            autofocus
            required
            minlength="3"
            maxlength="64"
          />
        </UFormField>

        <UFormField label="Email (optional)">
          <UInput
            v-model="form.email"
            type="email"
            placeholder="Enter your email"
            icon="i-lucide-mail"
            autocomplete="email"
          />
        </UFormField>

        <UFormField label="Password" required :hint="passwordHint">
          <UInput
            v-model="form.password"
            type="password"
            placeholder="Choose a password (min 8 characters)"
            icon="i-lucide-lock"
            autocomplete="new-password"
            required
            minlength="8"
          />
        </UFormField>

        <UFormField
          label="Confirm Password"
          required
          :error="!passwordsMatch ? 'Passwords do not match' : undefined"
        >
          <UInput
            v-model="form.confirmPassword"
            type="password"
            placeholder="Confirm your password"
            icon="i-lucide-lock"
            autocomplete="new-password"
            required
            :color="!passwordsMatch ? 'error' : undefined"
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
          :disabled="!form.username || !form.password || !form.confirmPassword"
          label="Create Account"
          icon="i-lucide-user-plus"
        />
      </form>

      <template #footer>
        <p class="text-center text-sm text-(--ui-text-muted)">
          Already have an account?
          <NuxtLink to="/login" class="text-(--ui-text-highlighted) font-medium hover:underline">
            Sign in
          </NuxtLink>
        </p>
      </template>
    </UCard>
  </UContainer>
</template>
