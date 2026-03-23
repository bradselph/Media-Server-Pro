export default defineNuxtConfig({
  modules: [
    '@nuxt/ui',
    '@nuxt/icon',
    '@pinia/nuxt',
  ],

  css: ['~/assets/css/main.css'],

  colorMode: {
    classSuffix: '',
    preference: 'dark',
    fallback: 'dark',
  },

  // Build output goes to web/static/react/ so Go serves it from the embedded FS
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
      link: [
        { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' },
      ],
    },
  },

  // Proxy API calls to Go backend in dev mode
  routeRules: {
    '/api/**': { proxy: 'http://localhost:8080/api/**' },
    '/media': { proxy: 'http://localhost:8080/media' },
    '/thumbnail': { proxy: 'http://localhost:8080/thumbnail' },
    '/download': { proxy: 'http://localhost:8080/download' },
    '/hls/**': { proxy: 'http://localhost:8080/hls/**' },
    '/upload/**': { proxy: 'http://localhost:8080/upload/**' },
  },

  devtools: { enabled: false },
  compatibilityDate: '2024-11-01',
})
