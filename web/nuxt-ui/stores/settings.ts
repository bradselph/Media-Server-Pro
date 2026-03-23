import { defineStore } from 'pinia'
import type { ServerSettings } from '~/types/api'

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<ServerSettings | null>(null)
  const isLoading = ref(false)

  const features = computed(() => settings.value?.features)

  async function loadSettings() {
    if (settings.value) return
    isLoading.value = true
    try {
      const { get } = useSettingsApi()
      settings.value = await get()
    } catch {
      // Non-critical — use defaults
    } finally {
      isLoading.value = false
    }
  }

  return { settings, isLoading, features, loadSettings }
})
