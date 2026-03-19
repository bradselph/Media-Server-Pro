<script setup lang="ts">
import { ApiError } from '~/composables/useApi'

definePageMeta({
  title: 'Login',
  layout: 'default',
})

const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({
  username: '',
  password: '',
})
const error = ref('')
const loading = ref(false)

/**
 * Parse the redirect query parameter with open-redirect prevention:
 * must start with / and not //.
 */
function parseRedirect(): string {
  const raw = (route.query.redirect as string) || '/'
  const safe = raw.startsWith('/') && !raw.startsWith('//')
  return safe ? raw : '/'
}

/**
 * Resolve the post-login destination.
 * Admins go to /admin; regular users go to the redirect URL.
 */
function resolvePostLoginPath(isAdmin: boolean): string {
  if (isAdmin) return '/admin'
  return parseRedirect()
}

async function handleLogin() {
  error.value = ''
  loading.value = true
  try {
    const result = await authStore.login(form.username, form.password)
    const destination = resolvePostLoginPath(result.isAdmin)
    await router.replace(destination)
  } catch (err: unknown) {
    if (err instanceof ApiError) {
      error.value = err.message
    } else {
      error.value = 'Login failed. Please try again.'
    }
  } finally {
    loading.value = false
  }
}

function browseAsGuest() {
  router.push(parseRedirect())
}
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
            Sign In
          </h1>
          <p class="text-sm text-(--ui-text-muted)">
            Sign in to your media server account
          </p>
        </div>
      </template>

      <form class="space-y-4" @submit.prevent="handleLogin">
        <UFormField label="Username">
          <UInput
            v-model="form.username"
            placeholder="Enter your username"
            icon="i-lucide-user"
            autocomplete="username"
            autofocus
            required
          />
        </UFormField>

        <UFormField label="Password">
          <UInput
            v-model="form.password"
            type="password"
            placeholder="Enter your password"
            icon="i-lucide-lock"
            autocomplete="current-password"
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
          :disabled="!form.username || !form.password"
          label="Sign In"
          icon="i-lucide-log-in"
        />
      </form>

      <template #footer>
        <div class="space-y-3">
          <p class="text-center text-sm text-(--ui-text-muted)">
            Don't have an account?
            <NuxtLink to="/signup" class="text-(--ui-text-highlighted) font-medium hover:underline">
              Sign up
            </NuxtLink>
          </p>
          <UButton
            v-if="authStore.allowGuests"
            block
            variant="outline"
            color="neutral"
            label="Browse as Guest"
            icon="i-lucide-eye"
            @click="browseAsGuest"
          />
        </div>
      </template>
    </UCard>
  </UContainer>
</template>
