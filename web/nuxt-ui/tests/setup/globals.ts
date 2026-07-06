import * as vue from 'vue'

// Expose the Vue reactivity/lifecycle APIs that Nuxt normally auto-imports as
// globals, so stores and composables that rely on them (e.g. `ref`, `watch`
// without an explicit import) run under the lightweight happy-dom environment
// without booting the full Nuxt runtime. Tests that genuinely need Nuxt-specific
// APIs (useRuntimeConfig, useFetch, ...) should use `// @vitest-environment nuxt`.
const g = globalThis as unknown as Record<string, unknown>
const names = [
    'ref', 'computed', 'reactive', 'readonly', 'shallowRef', 'shallowReactive',
    'toRef', 'toRefs', 'unref', 'isRef', 'watch', 'watchEffect', 'nextTick',
    'onMounted', 'onUnmounted', 'onBeforeUnmount', 'onBeforeMount', 'provide', 'inject',
]
for (const name of names) {
    g[name] = (vue as unknown as Record<string, unknown>)[name]
}
