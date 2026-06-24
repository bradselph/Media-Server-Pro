<script setup lang="ts">
import type {ServerSettings} from '~/types/api'

definePageMeta({layout: 'default', title: 'Login'})

const authStore = useAuthStore()
const settingsApi = useSettingsApi()
const router = useRouter()
const route = useRoute()

const form = reactive({username: '', password: ''})
const loading = ref(false)
// Server-side error from the auth API (invalid credentials, account locked,
// etc.). Surfaced as a banner above the form because it doesn't map to a
// single field.
const error = ref('')
// Per-field client-side validation. Empty until the user blurs a field or
// submits, so we don't flash red borders before they've had a chance to type.
const fieldErrors = reactive({username: '', password: ''})
// Username UInput template ref; Nuxt UI exposes the underlying <input> element
// as `inputRef`. Re-focused after a failed login so the user can retype at once.
const usernameInput = ref<{ inputRef?: HTMLInputElement | null } | null>(null)
const touched = reactive({username: false, password: false})
const allowRegistration = ref(true) // optimistic default until settings load
const allowGuests = computed(() => authStore.allowGuests)
// Set when a mature-locked title routed the user here (?reason=mature).
const cameFromMatureGate = computed(() => route.query.reason === 'mature')

function validateField(field: 'username' | 'password', soft = false) {
  if (field === 'username') {
    fieldErrors.username = form.username.trim() ? '' : (soft ? '' : 'Username is required')
  } else {
    fieldErrors.password = form.password ? '' : (soft ? '' : 'Password is required')
  }
}

watch(() => form.username, () => {
  if (touched.username) validateField('username', true)
})
watch(() => form.password, () => {
  if (touched.password) validateField('password', true)
})

// Redirect if already logged in.
// Only allow same-origin app routes -- reject external URLs, protocol-relative
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
    // Non-critical -- keep default (true) so the link stays visible if the call fails
  }
})

async function handleLogin() {
  error.value = ''
  // Mark everything touched so empty-field errors actually show on submit.
  touched.username = touched.password = true
  validateField('username')
  validateField('password')
  if (fieldErrors.username || fieldErrors.password) return

  loading.value = true
  try {
    await authStore.login(form.username, form.password)
    router.replace(loginRedirectDest())
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Invalid credentials'
    nextTick(() => usernameInput.value?.inputRef?.focus())
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
          <UIcon name="i-lucide-film" class="size-8" style="color: var(--accent-soft);"/>
        </div>
        <h1 class="text-2xl font-extrabold text-highlighted">Sign In</h1>
        <p class="text-muted text-sm mt-1">Media Server Pro</p>
      </div>

      <UAlert
          v-if="cameFromMatureGate"
          class="mb-4"
          color="primary"
          variant="soft"
          icon="i-lucide-lock"
          title="Sign in to watch mature content"
          description="That title is mature — sign in, or create a free account, to unlock it."
      />

      <!-- Card -->
      <div class="rounded-xl border border-[var(--hairline)] bg-[var(--surface-card)] p-7 space-y-5">
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
                ref="usernameInput"
                v-model="form.username"
                name="username"
                placeholder="your username"
                autocomplete="username"
                class="w-full"
                required
                autofocus
                :aria-invalid="!!fieldErrors.username"
                :aria-describedby="fieldErrors.username ? 'username-error' : undefined"
                @blur="touched.username = true; validateField('username')"
            />
            <p v-if="fieldErrors.username" id="username-error" class="text-[11px] text-error mt-1" role="alert">{{
                fieldErrors.username
              }}</p>
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Password</label>
            <PasswordInput
                v-model="form.password"
                name="password"
                autocomplete="current-password"
                required
                :aria-describedby="fieldErrors.password ? 'password-error' : undefined"
                @blur="touched.password = true; validateField('password')"
            />
            <p v-if="fieldErrors.password" id="password-error" class="text-[11px] text-error mt-1" role="alert">{{
                fieldErrors.password
              }}</p>
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
        <NuxtLink to="/signup" class="font-semibold hover:underline" style="color: var(--accent-soft);">Create one
        </NuxtLink>
      </p>
      <p v-else class="text-center text-sm text-muted mt-5">
        <UIcon name="i-lucide-lock" class="size-3.5 inline-block mr-1 -mt-0.5"/>
        Registration is currently closed.
      </p>
      <p class="text-center text-xs text-muted mt-3">
        Forgot your password? Contact the site administrator to reset it.
      </p>
    </div>
  </div>
</template>
