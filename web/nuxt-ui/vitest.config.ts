import {defineVitestConfig} from '@nuxt/test-utils/config'

// Frontend unit tests. Default environment is happy-dom (fast, DOM available for
// component/store tests). A test file that needs the full Nuxt runtime (real
// auto-imports, useRuntimeConfig, etc.) can opt in with a `// @vitest-environment nuxt`
// docblock at the top of the file. Tests live under tests/ so Nuxt's app scan
// (components/, composables/, ...) never treats them as auto-imported modules.
export default defineVitestConfig({
    test: {
        environment: 'happy-dom',
        include: ['tests/**/*.spec.ts'],
        setupFiles: ['tests/setup/globals.ts'],
    },
})
