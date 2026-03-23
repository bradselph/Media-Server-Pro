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

  // Build output goes to web/static/react/ so Go serves it from the embedded FS.
  // With ssr:false, `nuxt build` writes index.html + /_nuxt/ assets here directly.
  nitro: {
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

  devtools: { enabled: false },
  compatibilityDate: '2024-11-01',
})
