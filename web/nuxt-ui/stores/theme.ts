import { defineStore } from 'pinia'

export const THEMES = [
  { name: 'Light', value: 'light', colorMode: 'light' },
  { name: 'Dark', value: 'dark', colorMode: 'dark' },
  { name: 'Midnight', value: 'midnight', colorMode: 'dark' },
  { name: 'Nord', value: 'nord', colorMode: 'dark' },
  { name: 'Dracula', value: 'dracula', colorMode: 'dark' },
  { name: 'Solarized Light', value: 'solarized-light', colorMode: 'light' },
  { name: 'Forest', value: 'forest', colorMode: 'dark' },
  { name: 'Sunset', value: 'sunset', colorMode: 'dark' },
] as const

export type ThemeValue = typeof THEMES[number]['value']

export const useThemeStore = defineStore('theme', () => {
  const colorMode = useColorMode()

  const currentTheme = ref<ThemeValue>(
    (import.meta.client ? localStorage.getItem('msp-theme') as ThemeValue : null) ?? 'dark',
  )

  function setTheme(theme: ThemeValue) {
    currentTheme.value = theme
    if (import.meta.client) {
      localStorage.setItem('msp-theme', theme)
      const t = THEMES.find(t => t.value === theme)
      colorMode.preference = t?.colorMode ?? 'dark'
      // Remove all theme classes, add new one
      document.documentElement.classList.remove(...THEMES.map(t => `theme-${t.value}`))
      document.documentElement.classList.add(`theme-${theme}`)
    }
  }

  // Apply on init
  if (import.meta.client) {
    setTheme(currentTheme.value)
  }

  return { currentTheme, themes: THEMES, setTheme }
})
