<script setup lang="ts">
definePageMeta({
  title: 'Admin Login',
})

const authStore = useAuthStore()
const router = useRouter()

const form = reactive({
  username: '',
  password: '',
})
const error = ref('')
const loading = ref(false)

async function handleLogin() {
  error.value = ''
  loading.value = true
  try {
    const result = await authStore.login(form.username, form.password)
    if (!result.isAdmin) {
      error.value = 'This account does not have admin privileges'
      await authStore.logout()
      return
    }
    await router.push('/admin')
  } catch (err: any) {
    error.value = err?.message || 'Login failed'
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
          Admin Login
        </h1>
      </template>

      <form class="space-y-4" @submit.prevent="handleLogin">
        <UFormField label="Username">
          <UInput
            v-model="form.username"
            placeholder="Admin username"
            icon="i-lucide-shield"
            autofocus
            required
          />
        </UFormField>

        <UFormField label="Password">
          <UInput
            v-model="form.password"
            type="password"
            placeholder="Admin password"
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
          label="Sign In as Admin"
        />
      </form>

      <template #footer>
        <p class="text-center text-sm text-(--ui-text-muted)">
          <NuxtLink to="/login" class="text-(--ui-text-highlighted) hover:underline">
            Regular login
          </NuxtLink>
        </p>
      </template>
    </UCard>
  </UContainer>
</template>
