export default defineNuxtConfig({
  modules: [
    '@nuxt/ui',
    '@nuxt/icon',
    '@pinia/nuxt',
  ],

  // SPA mode: no SSR server — output is pure static HTML/JS/CSS
  // that Go embeds via //go:embed static/*
  ssr: false,

  css: ['~/assets/css/main.css'],

  colorMode: {
    classSuffix: '',
    preference: 'dark',
    fallback: 'dark',
  },

  // Build output goes to web/static/react/ so Go embeds it via //go:embed static/*.
  // preset: 'static' + ssr:false → `nuxt generate` writes index.html + _nuxt/ assets
  // directly into the publicDir, with no Node server required.
  nitro: {
    preset: 'static',
    output: {
      publicDir: '../static/react',
    },
  },

  app: {
    head: {
      title: 'Media Server Pro',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
    },
  },

  // Proxy API + media calls to the Go backend in dev mode (nuxt dev).
  // These rules are ignored in the static SPA build — Go handles them directly.
  routeRules: {
    '/api/**': { proxy: 'http://localhost:8080/api/**' },
    '/media/**': { proxy: 'http://localhost:8080/media/**' },
    '/media': { proxy: 'http://localhost:8080/media' },
    '/thumbnail': { proxy: 'http://localhost:8080/thumbnail' },
    '/thumbnails/**': { proxy: 'http://localhost:8080/thumbnails/**' },
    '/download': { proxy: 'http://localhost:8080/download' },
    '/hls/**': { proxy: 'http://localhost:8080/hls/**' },
    '/upload/**': { proxy: 'http://localhost:8080/upload/**' },
    '/ws/**': { proxy: 'http://localhost:8080/ws/**' },
    '/health': { proxy: 'http://localhost:8080/health' },
  },

  // Bundle all icons into the client JS so no runtime fetch to api.iconify.design is needed.
  // This avoids CSP connect-src issues when the Go server sets a strict policy.
  icon: {
    clientBundle: {
      scan: true,
      includeCustomCollections: true,
      // Some Nuxt UI controls (pagination/select/dropdown) resolve these at runtime.
      // Keep them bundled so strict CSP never falls back to api.iconify.design.
      icons: ['lucide:chevron-down', 'lucide:check', 'lucide:chevron-right', 'lucide:chevron-left', 'lucide:chevron-up', 'lucide:chevrons-left', 'lucide:chevrons-right', 'lucide:x', 'lucide:circle-alert', 'lucide:circle-check', 'lucide:info', 'lucide:triangle-alert'],
    },
  },

  devtools: { enabled: false },
  compatibilityDate: '2024-11-01',
})
