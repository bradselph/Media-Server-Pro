// https://nuxt.com/docs/api/configuration/nuxt-config
import { resolve } from 'path'

export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',

  modules: [
    '@nuxt/ui',
    '@pinia/nuxt',
    '@nuxt/icon',
    '@nuxt/fonts',
  ],

  css: ['~/assets/css/main.css'],

  // SPA mode — no SSR, matching the current React SPA behavior.
  // The Go backend serves index.html for all SPA routes.
  ssr: false,

  devtools: { enabled: true },

  // Base URL must match the Go backend's static file serving path.
  // The Go server embeds files from web/static/react/ and serves them
  // at /web/static/react/ — our built assets must use this prefix.
  app: {
    baseURL: '/web/static/react/',
    head: {
      title: 'Media Server Pro',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
    },
  },

  // Build output goes to ../static/react/ so the Go backend can embed it
  // via //go:embed static/*. Use `npm run build` (nuxt generate) to produce
  // a fully static SPA with index.html + _nuxt/ assets.
  nitro: {
    output: {
      publicDir: resolve(__dirname, '../static/react'),
    },
  },

  // Dev server proxy — forward API and media requests to Go backend
  devServer: {
    port: 3000,
  },

  vite: {
    build: {
      // Vendor chunks (Vue + Nuxt UI) exceed default 500 kB warning; expected for admin-heavy UI.
      chunkSizeWarningLimit: 900,
    },
    server: {
      proxy: {
        '/api': 'http://localhost:8080',
        '/media': 'http://localhost:8080',
        '/hls': 'http://localhost:8080',
        '/thumbnail': 'http://localhost:8080',
        '/thumbnails': 'http://localhost:8080',
        '/download': 'http://localhost:8080',
        '/metrics': 'http://localhost:8080',
        '/remote': 'http://localhost:8080',
        '/health': 'http://localhost:8080',
        '/ws': { target: 'http://localhost:8080', ws: true },
        '/extractor': 'http://localhost:8080',
      },
    },
  },
})
