// Google Analytics bootstrap, client-only.
//
// 1. Tries to init GA on mount (no-op until consent is granted and a
//    NUXT_PUBLIC_GA_ID is baked into the bundle).
// 2. Emits a synthetic `page_view` event on every router navigation —
//    GA4 doesn't track SPA route changes natively because the URL
//    transition happens via history.pushState rather than a full
//    document load.
// 3. Re-attempts init when the cookie banner reports a change, so the
//    user can opt in mid-session without a refresh.

import {initGA, pageview} from '~/composables/useAnalytics'
import {onConsentChanged} from '~/composables/useConsent'

export default defineNuxtPlugin(() => {
    if (!import.meta.client) return

    initGA()

    onConsentChanged(() => {
        // initGA is idempotent and gated internally on consent + gaId.
        initGA()
    })

    const router = useRouter()
    router.afterEach((to) => {
        // Fire on next tick so document.title reflects the new page —
        // GA4 reads the title at event time.
        nextTick(() => pageview(to.fullPath))
    })
})
