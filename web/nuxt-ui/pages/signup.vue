<script setup lang="ts">
definePageMeta({layout: 'default', title: 'Sign Up'})

const {register, getRegistrationToken} = useApiEndpoints()
const settingsApi = useSettingsApi()
const authStore = useAuthStore()
const router = useRouter()

const form = reactive({username: '', password: '', confirm: '', email: ''})
const loading = ref(false)
// Top-level submission error (server-side, e.g. token expired). Per-field
// validation goes through `fieldErrors` so the user sees the message right
// next to the offending input rather than as a banner.
const error = ref('')
const fieldErrors = reactive({username: '', email: '', password: '', confirm: ''})
// Tracks which fields the user has touched, so we only surface "required"
// errors after they leave the field -- typing into an empty field shouldn't
// immediately flash a red border on the field they're filling out.
const touched = reactive({username: false, email: false, password: false, confirm: false})
const registrationClosed = ref(false)
const regToken = ref('')

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function validateField(field: 'username' | 'email' | 'password' | 'confirm', soft = false) {
  // `soft` mode skips empty-field "required" errors so we don't yell at the
  // user while they're still typing. Submit calls with soft=false to catch
  // all of them.
  switch (field) {
    case 'username': {
      const v = form.username.trim()
      if (!v) {
        fieldErrors.username = soft ? '' : 'Username is required';
        break
      }
      if (v.length < 3) {
        fieldErrors.username = 'Must be at least 3 characters';
        break
      }
      fieldErrors.username = ''
      break
    }
    case 'email': {
      // Email is optional, but if provided it must look like an email.
      const v = form.email.trim()
      if (!v) {
        fieldErrors.email = '';
        break
      }
      fieldErrors.email = EMAIL_RE.test(v) ? '' : 'Enter a valid email address'
      break
    }
    case 'password': {
      const v = form.password
      if (!v) {
        fieldErrors.password = soft ? '' : 'Password is required';
        break
      }
      if (v.length < 8) {
        fieldErrors.password = 'Must be at least 8 characters';
        break
      }
      fieldErrors.password = ''
      // Re-validate confirm against the new password value so a fixed
      // password clears the "passwords do not match" error on the confirm
      // field automatically.
      if (touched.confirm || form.confirm) validateField('confirm', true)
      break
    }
    case 'confirm': {
      if (!form.confirm) {
        fieldErrors.confirm = soft ? '' : 'Please confirm your password';
        break
      }
      fieldErrors.confirm = form.confirm === form.password ? '' : 'Passwords do not match'
      break
    }
  }
}

// Live re-validate touched fields as the user types. Keeps the UI honest
// without firing errors on fields they haven't visited yet.
watch(() => form.username, () => {
  if (touched.username) validateField('username', true)
})
watch(() => form.email, () => {
  if (touched.email) validateField('email', true)
})
watch(() => form.password, () => {
  if (touched.password) validateField('password', true)
})
watch(() => form.confirm, () => {
  if (touched.confirm) validateField('confirm', true)
})

onMounted(async () => {
  if (authStore.isLoggedIn) {
    router.replace('/');
    return
  }
  try {
    const settings = await settingsApi.get()
    if (settings?.auth?.allow_registration === false) {
      registrationClosed.value = true
      return
    }
  } catch {
  }
  try {
    const res = await getRegistrationToken()
    regToken.value = res.token ?? ''
  } catch {
    error.value = 'Unable to load registration form. Please refresh and try again.'
  }
})

async function handleSignup() {
  error.value = ''
  // Force a full validation pass so any field the user never touched still
  // gets its "required" error surfaced before submit.
  touched.username = touched.email = touched.password = touched.confirm = true
  validateField('username')
  validateField('email')
  validateField('password')
  validateField('confirm')
  if (fieldErrors.username || fieldErrors.email || fieldErrors.password || fieldErrors.confirm) return

  loading.value = true
  try {
    await register(form.username, form.password, regToken.value, form.email || undefined)
    // Fetch the full session instead of using the raw register response,
    // which may have null permissions/preferences on a freshly-created account.
    await authStore.fetchSession()
    // One-shot flag so the home page can greet the brand-new user with a
    // welcome toast pointing at Categories / Surprise Me.
    if (typeof window !== 'undefined') {
      try {
        localStorage.setItem('msp-welcomed', 'pending')
      } catch { /* private mode */
      }
    }
    router.replace('/')
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Registration failed'
    // Token is consumed on any attempt (success or server-side failure after validation).
    // Fetch a fresh one so the user can retry without reloading.
    try {
      const res = await getRegistrationToken()
      regToken.value = res.token ?? ''
    } catch {
    }
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
          <UIcon name="i-lucide-user-plus" class="size-8" style="color: var(--accent-soft);"/>
        </div>
        <h1 class="text-2xl font-extrabold text-highlighted">Create Account</h1>
        <p class="text-muted text-sm mt-1">Media Server Pro</p>
      </div>

      <!-- Card -->
      <div class="rounded-xl border border-[var(--hairline)] bg-[var(--surface-card)] p-7">
        <div v-if="registrationClosed" class="text-center py-6 space-y-4">
          <UIcon name="i-lucide-user-x" class="size-10 text-muted mx-auto"/>
          <p class="text-muted">Registration is currently closed.</p>
          <p class="text-xs text-muted">If you already have an account you can still sign in.</p>
          <UButton to="/login" label="Sign in" icon="i-lucide-log-in" variant="outline"/>
        </div>
        <form v-else class="space-y-4" @submit.prevent="handleSignup">
          <UAlert v-if="error" :title="error" color="error" variant="soft" icon="i-lucide-x-circle"/>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Username <span
                class="text-error">*</span></label>
            <UInput
                v-model="form.username"
                name="username"
                placeholder="username"
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
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Email <span
                class="text-muted opacity-60 normal-case font-normal">(optional)</span></label>
            <UInput
                v-model="form.email"
                name="email"
                type="email"
                placeholder="user@example.com"
                autocomplete="email"
                class="w-full"
                :aria-invalid="!!fieldErrors.email"
                :aria-describedby="fieldErrors.email ? 'email-error' : undefined"
                @blur="touched.email = true; validateField('email')"
            />
            <p v-if="fieldErrors.email" id="email-error" class="text-[11px] text-error mt-1" role="alert">{{ fieldErrors.email }}</p>
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Password <span
                class="text-error">*</span></label>
            <PasswordInput
                v-model="form.password"
                name="new-password"
                autocomplete="new-password"
                required
                :minlength="8"
                :aria-describedby="fieldErrors.password ? 'password-error' : 'password-hint'"
                @blur="touched.password = true; validateField('password')"
            />
            <PasswordStrength :value="form.password"/>
            <p v-if="fieldErrors.password" id="password-error" class="text-[11px] text-error mt-1" role="alert">{{
                fieldErrors.password
              }}</p>
            <p v-else id="password-hint" class="text-[10px] text-muted mt-1">Minimum 8 characters.</p>
          </div>
          <div>
            <label class="block text-[11px] font-bold text-muted uppercase tracking-wide mb-1.5">Confirm Password <span
                class="text-error">*</span></label>
            <PasswordInput
                v-model="form.confirm"
                name="confirm-password"
                autocomplete="new-password"
                required
                :aria-describedby="fieldErrors.confirm ? 'confirm-error' : undefined"
                @blur="touched.confirm = true; validateField('confirm')"
            />
            <p v-if="fieldErrors.confirm" id="confirm-error" class="text-[11px] text-error mt-1" role="alert">{{
                fieldErrors.confirm
              }}</p>
          </div>
          <UButton type="submit" class="w-full justify-center mt-1" :loading="loading" :disabled="!regToken"
                   label="Create Account" color="primary"/>
        </form>
      </div>

      <p class="text-center text-sm text-muted mt-5">
        Already have an account?
        <NuxtLink to="/login" class="font-semibold hover:underline" style="color: var(--accent-soft);">Sign in
        </NuxtLink>
      </p>
    </div>
  </div>
</template>
