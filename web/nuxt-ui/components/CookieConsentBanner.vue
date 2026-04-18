<script setup lang="ts">
const cookieConsentApi = useCookieConsentApi()

const visible = ref(false)
const accepting = ref(false)

async function checkStatus() {
  try {
    const status = await cookieConsentApi.getStatus()
    if (status.required && !status.given) {
      visible.value = true
    }
  } catch { /* non-critical */ }
}

async function accept(analytics: boolean) {
  if (accepting.value) return
  accepting.value = true
  try {
    await cookieConsentApi.accept(analytics)
    visible.value = false
  } catch { /* keep banner visible on failure */ } finally {
    accepting.value = false
  }
}

onMounted(checkStatus)
</script>

<template>
  <Transition
    enter-active-class="transition-transform duration-300 ease-out"
    enter-from-class="translate-y-full"
    enter-to-class="translate-y-0"
    leave-active-class="transition-transform duration-200 ease-in"
    leave-from-class="translate-y-0"
    leave-to-class="translate-y-full"
  >
    <div
      v-if="visible"
      role="region"
      aria-label="Cookie consent"
      class="fixed bottom-0 left-0 right-0 z-50 border-t border-default bg-elevated shadow-lg"
    >
      <UContainer class="py-4">
        <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
          <div class="flex-1 min-w-0">
            <p class="text-sm font-semibold text-highlighted mb-1">We use cookies</p>
            <p class="text-xs text-muted leading-relaxed">
              This site uses essential cookies for session management and age verification.
              We also use optional analytics cookies to understand how the site is used.
              By continuing you agree to our
              <NuxtLink to="/privacy" class="underline hover:text-default">Privacy Policy</NuxtLink>
              and
              <NuxtLink to="/terms" class="underline hover:text-default">Terms of Service</NuxtLink>.
            </p>
          </div>
          <div class="flex items-center gap-2 shrink-0 flex-wrap">
            <UButton
              variant="outline"
              color="neutral"
              size="sm"
              label="Essential only"
              :loading="accepting"
              @click="accept(false)"
            />
            <UButton
              color="primary"
              size="sm"
              label="Accept all"
              :loading="accepting"
              @click="accept(true)"
            />
          </div>
        </div>
      </UContainer>
    </div>
  </Transition>
</template>
