// Cookie consent: single source of truth.
//
// Storage key:  msp-consent
// Shape:        { strict: true, analytics: bool, advertising: bool, ts: ISO }
// Persistence:  12 months (banner shows again after that).
// Reopen:       window.dispatchEvent(new CustomEvent('msp:cookie-banner:open'))
//
// Pattern: any module that loads a third-party script (GA, ads) checks
// `consentFor()` synchronously on init AND subscribes to the
// 'msp:consent-changed' event so a later opt-in starts loading without
// requiring a page refresh.

export interface ConsentRecord {
    strict: true
    analytics: boolean
    advertising: boolean
    ts: string
}

const KEY = 'msp-consent'
const TTL_MS = 12 * 30 * 24 * 60 * 60 * 1000 // ~12 months

function getConsent(): ConsentRecord | null {
    if (typeof window === 'undefined') return null
    try {
        const raw = window.localStorage.getItem(KEY)
        if (!raw) return null
        const parsed = JSON.parse(raw)
        if (!parsed || typeof parsed !== 'object') return null
        if (parsed.ts && (Date.now() - new Date(parsed.ts).getTime() > TTL_MS)) {
            return null // stale — pretend the user hasn't decided
        }
        return parsed as ConsentRecord
    } catch {
        return null
    }
}

export function setConsent(opts: { analytics?: boolean; advertising?: boolean }): ConsentRecord {
    const next: ConsentRecord = {
        strict: true,
        analytics: !!opts.analytics,
        advertising: !!opts.advertising,
        ts: new Date().toISOString(),
    }
    try {
        window.localStorage.setItem(KEY, JSON.stringify(next))
    } catch {
        // private-browsing mode — localStorage may throw on write
    }
    window.dispatchEvent(new CustomEvent('msp:consent-changed', {detail: next}))
    return next
}

export function onConsentChanged(handler: (c: ConsentRecord | null) => void): () => void {
    const wrapped = (e: Event) => {
        const ce = e as CustomEvent<ConsentRecord>
        handler(ce.detail || getConsent())
    }
    window.addEventListener('msp:consent-changed', wrapped)
    return () => window.removeEventListener('msp:consent-changed', wrapped)
}

// Until the banner is shown + decided, treat every non-strict category as
// off. Strict (essential cookies — session, age-gate) is always allowed.
export function consentFor(category: 'strict' | 'analytics' | 'advertising'): boolean {
    const c = getConsent()
    if (!c) return false
    if (category === 'strict') return true
    return !!c[category]
}
