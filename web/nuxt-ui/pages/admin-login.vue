<script setup lang="ts">
definePageMeta({ layout: 'default', title: 'Admin Login' })

// Reuse the regular login page but redirect to /admin
const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')
const mounted = ref(true)
onBeforeUnmount(() => { mounted.value = false })

const adminDest = () => {
  const r = route.query.redirect
  if (typeof r === 'string' && r.startsWith('/') && !r.startsWith('//') &&
      !r.startsWith('/api/') && !r.startsWith('/extractor/')) return r
  return '/admin'
}

onMounted(async () => {
  if (!authStore.isLoading && authStore.isAdmin) router.replace(adminDest())
})

async function handleLogin() {
  error.value = ''
  loading.value = true
  try {
    await authStore.login(form.username, form.password)
    if (authStore.isAdmin) {
      router.replace(adminDest())
    } else {
      error.value = 'Admin access required'
      await authStore.logout()
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Invalid credentials'
  } finally {
    if (mounted.value) loading.value = false
  }
}
</script>

<template>
  <div class="min-h-[80vh] flex items-center justify-center px-4">
    <div class="w-full max-w-sm space-y-6">
      <div class="text-center space-y-2">
        <UIcon name="i-lucide-shield" class="size-10 text-primary mx-auto" />
        <h1 class="text-2xl font-bold text-highlighted">Admin Login</h1>
        <p class="text-muted text-sm">Sign in with your admin credentials</p>
      </div>
      <UCard>
        <form class="space-y-4" @submit.prevent="handleLogin">
          <UAlert v-if="error" :title="error" color="error" variant="soft" icon="i-lucide-x-circle" />
          <UFormField label="Username">
            <UInput v-model="form.username" name="username" placeholder="admin" autocomplete="username" required />
          </UFormField>
          <UFormField label="Password">
            <UInput v-model="form.password" name="password" type="password" placeholder="••••••••" autocomplete="current-password" required />
          </UFormField>
          <UButton type="submit" class="w-full justify-center" :loading="loading" label="Sign In" />
        </form>
      </UCard>
    </div>
  </div>
</template>
