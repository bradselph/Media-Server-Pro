// Google Analytics 4 wiring.
//
// Loads gtag.js from googletagmanager.com on demand (no inline script, so
// a strict CSP doesn't need 'unsafe-inline'). Skipped on localhost so dev
// runs don't pollute prod analytics.
//
// Gate: useConsent. We don't load gtag at all until the user has
// affirmatively accepted analytics cookies. If they accept later, the
// 'msp:consent-changed' event re-runs initGA().
//
// GA4 doesn't track HTML5 history pushState by default, so we disable
// the automatic page_view (send_page_view: false) and emit our own from
// plugins/analytics.client.ts whenever the route changes.

import {consentFor, onConsentChanged} from './useConsent'

declare global {
    interface Window {
        dataLayer?: unknown[]
        gtag?: (...args: unknown[]) => void
    }
}

let initialized = false
let awaitingConsent = false

function isProductionHost(): boolean {
    if (typeof window === 'undefined') return false
    const h = window.location.hostname
    if (!h) return false
    if (h === 'localhost' || h === '127.0.0.1' || h === '0.0.0.0') return false
    if (h.endsWith('.local')) return false
    return true
}

export function initGA(): void {
    if (initialized || !isProductionHost()) return

    // Measurement ID — read from runtimeConfig.public.gaId. Knob:
    // NUXT_PUBLIC_GA_ID in .deploy.env, baked into the bundle at build
    // time. Empty = analytics off entirely.
    const config = useRuntimeConfig()
    const gaId = (config.public.gaId as string | undefined) || ''
    if (!gaId) return

    if (!consentFor('analytics')) {
        // Wait for consent — re-attempt when the banner reports a change.
        // Register the listener only once: onConsentChanged adds a new (never
        // removed) window listener on every call, so re-entrant initGA() calls
        // while consent is still withheld would otherwise accumulate listeners.
        if (!awaitingConsent) {
            awaitingConsent = true
            onConsentChanged(() => {
                if (consentFor('analytics')) initGA()
            })
        }
        return
    }
    initialized = true

    // Load the gtag.js library asynchronously. Done in JS (not an inline
    // <script> tag) so a strict CSP doesn't need 'unsafe-inline'.
    const s = document.createElement('script')
    s.async = true
    s.src = `https://www.googletagmanager.com/gtag/js?id=${encodeURIComponent(gaId)}`
    document.head.appendChild(s)

    window.dataLayer = window.dataLayer || []
    const gtag: (...args: unknown[]) => void = function () {
        // eslint-disable-next-line prefer-rest-params
        window.dataLayer!.push(arguments)
    }
    window.gtag = gtag

    gtag('js', new Date())
    // send_page_view:false → emitted manually from the analytics plugin
    // on every route change.
    gtag('config', gaId, {send_page_view: false})
}

export function pageview(path?: string): void {
    if (typeof window === 'undefined' || !window.gtag) return
    window.gtag('event', 'page_view', {
        page_path: path || (window.location.pathname + window.location.hash),
        page_location: window.location.href,
        page_title: document.title,
    })
}

export function useAnalytics() {
    return {initGA, pageview}
}
