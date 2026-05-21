/**
 * Brand configuration composable — per design handoff §7.
 *
 * Resolution order (first non-empty wins for each field):
 *   1. window.APP_CONFIG — set by an injected <script> at deploy time
 *      to override the brand without rebuilding the SPA. Enables a single
 *      build to power multiple sibling sites.
 *   2. useRuntimeConfig().public — baked into the bundle at build time
 *      from NUXT_PUBLIC_* env vars (see deploy-knobs.sh "Brand" and
 *      "Legal compliance" sections).
 *   3. Nuxt's app.config.ts build-time defaults (useAppConfig().brand).
 *   4. Hard-coded fallbacks ("Media Server Pro" / "Your Library").
 *
 * The gradient field, when empty, falls back to an OKLCH gradient derived
 * from --accent-hue at render time — see default.vue.
 */

export interface BrandConfig {
    name: string
    tagline: string
    /** CSS linear-gradient value (e.g. 'linear-gradient(135deg,#6366f1,#3b82f6)'). Empty = use accent-hue fallback. */
    gradient: string
    /** 18 U.S.C. § 2257 records-custodian email. Surfaced on /2257. */
    complianceEmail: string
    /** 2257 records-custodian postal address. Surfaced on /2257. */
    complianceAddress: string
    /** DMCA designated agent's name (or "DMCA Designated Agent"). Surfaced on /dmca. */
    dmcaAgentName: string
    /** DMCA agent contact email. Surfaced on /dmca. */
    dmcaEmail: string
    /** DMCA agent postal address (must match Copyright Office filing). */
    dmcaAddress: string
}

interface WindowAppConfig {
    APP_CONFIG?: Partial<{
        brandName: string
        brandTagline: string
        brandGradient: string
        complianceEmail: string
        complianceAddress: string
        dmcaAgentName: string
        dmcaEmail: string
        dmcaAddress: string
    }>
}

const FALLBACKS: BrandConfig = {
    name: 'Media Server Pro',
    tagline: 'Your Library',
    gradient: '',
    complianceEmail: '',
    complianceAddress: '',
    dmcaAgentName: '',
    dmcaEmail: '',
    dmcaAddress: '',
}

export function useBrandConfig(): ComputedRef<BrandConfig> {
    // useAppConfig() is SSR-safe and typed once augmented; we access .brand
    // defensively because app.config.ts is author-editable.
    const app = useAppConfig() as { brand?: Partial<BrandConfig> }
    const buildDefaults: Partial<BrandConfig> = app.brand ?? {}

    // useRuntimeConfig().public surfaces NUXT_PUBLIC_* env vars set at
    // build time by deploy.sh. SSR-safe; defaults to empty strings.
    const pub = (useRuntimeConfig().public ?? {}) as Partial<{
        brandName: string
        brandTagline: string
        brandGradient: string
        complianceEmail: string
        complianceAddress: string
        dmcaAgentName: string
        dmcaEmail: string
        dmcaAddress: string
    }>

    // Runtime override comes from window.APP_CONFIG. Only available in the
    // browser — SSR falls back to build defaults.
    const runtime = computed<Partial<BrandConfig>>(() => {
        if (typeof window === 'undefined') return {}
        const cfg = (window as unknown as WindowAppConfig).APP_CONFIG
        if (!cfg) return {}
        return {
            name: cfg.brandName,
            tagline: cfg.brandTagline,
            gradient: cfg.brandGradient,
            complianceEmail: cfg.complianceEmail,
            complianceAddress: cfg.complianceAddress,
            dmcaAgentName: cfg.dmcaAgentName,
            dmcaEmail: cfg.dmcaEmail,
            dmcaAddress: cfg.dmcaAddress,
        }
    })

    return computed<BrandConfig>(() => ({
        name: runtime.value.name || pub.brandName || buildDefaults.name || FALLBACKS.name,
        tagline: runtime.value.tagline || pub.brandTagline || buildDefaults.tagline || FALLBACKS.tagline,
        gradient: runtime.value.gradient || pub.brandGradient || buildDefaults.gradient || FALLBACKS.gradient,
        complianceEmail: runtime.value.complianceEmail || pub.complianceEmail || buildDefaults.complianceEmail || FALLBACKS.complianceEmail,
        complianceAddress: runtime.value.complianceAddress || pub.complianceAddress || buildDefaults.complianceAddress || FALLBACKS.complianceAddress,
        dmcaAgentName: runtime.value.dmcaAgentName || pub.dmcaAgentName || buildDefaults.dmcaAgentName || FALLBACKS.dmcaAgentName,
        dmcaEmail: runtime.value.dmcaEmail || pub.dmcaEmail || buildDefaults.dmcaEmail || FALLBACKS.dmcaEmail,
        dmcaAddress: runtime.value.dmcaAddress || pub.dmcaAddress || buildDefaults.dmcaAddress || FALLBACKS.dmcaAddress,
    }))
}
