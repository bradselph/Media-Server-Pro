<script setup lang="ts">
definePageMeta({
  title: 'Login',
  layout: 'default',
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
    if (result.isAdmin) {
      await router.push('/admin')
    } else {
      await router.push('/')
    }
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
          Sign In
        </h1>
      </template>

      <form class="space-y-4" @submit.prevent="handleLogin">
        <UFormField label="Username">
          <UInput
            v-model="form.username"
            placeholder="Enter username"
            icon="i-lucide-user"
            autofocus
            required
          />
        </UFormField>

        <UFormField label="Password">
          <UInput
            v-model="form.password"
            type="password"
            placeholder="Enter password"
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
          label="Sign In"
        />
      </form>

      <template #footer>
        <p class="text-center text-sm text-(--ui-text-muted)">
          Don't have an account?
          <NuxtLink to="/signup" class="text-(--ui-text-highlighted) hover:underline">
            Sign up
          </NuxtLink>
        </p>
      </template>
    </UCard>
  </UContainer>
</template>
