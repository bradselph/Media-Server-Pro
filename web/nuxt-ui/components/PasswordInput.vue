<script setup lang="ts">
/**
 * PasswordInput — a UInput wrapper with a show/hide-password toggle button.
 *
 * Browser autofill and password managers still see this as a real password
 * field because the underlying input keeps `autocomplete` set to the value
 * the caller passed (e.g. "current-password" for login, "new-password" for
 * signup / change-password). Only the visible `type` toggles between
 * "password" and "text" when the user clicks the eye icon.
 *
 * Used by login.vue, signup.vue, admin-login.vue, and profile.vue's
 * password-change form so the toggle UX is consistent everywhere.
 */
const model = defineModel<string>({default: ''})

const props = withDefaults(defineProps<{
  name?: string
  placeholder?: string
  autocomplete?: string
  required?: boolean
  autofocus?: boolean
  minlength?: number | string
  // Forwarded to the inner input — plain attr fallthrough would land on the
  // wrapper <div>, not the <input>, so callers use this prop instead.
  ariaDescribedby?: string
}>(), {
  name: 'password',
  placeholder: '••••••••',
  autocomplete: 'current-password',
  required: false,
  autofocus: false,
})

const visible = ref(false)
const inputType = computed(() => (visible.value ? 'text' : 'password'))
</script>

<template>
  <div class="relative">
    <UInput
        v-model="model"
        :name="props.name"
        :type="inputType"
        :placeholder="props.placeholder"
        :autocomplete="props.autocomplete"
        :required="props.required"
        :autofocus="props.autofocus"
        :minlength="props.minlength"
        :aria-describedby="props.ariaDescribedby"
        class="w-full pr-9"
    />
    <UButton
        type="button"
        :icon="visible ? 'i-lucide-eye-off' : 'i-lucide-eye'"
        :aria-label="visible ? 'Hide password' : 'Show password'"
        :aria-pressed="visible"
        size="xs"
        variant="ghost"
        color="neutral"
        tabindex="-1"
        class="absolute right-1.5 top-1/2 -translate-y-1/2"
        @click="visible = !visible"
    />
  </div>
</template>
