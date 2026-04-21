/**
 * Brand configuration composable — per design handoff §7.
 *
 * Resolution order (first non-empty wins for each field):
 *   1. window.APP_CONFIG — set by an injected <script> at deploy time
 *      to override the brand without rebuilding the SPA. Enables a single
 *      build to power multiple sibling sites.
 *   2. Nuxt's app.config.ts build-time defaults (useAppConfig().brand).
 *   3. Hard-coded fallbacks ("Media Server Pro" / "Your Library").
 *
 * The gradient field, when empty, falls back to an OKLCH gradient derived
 * from --accent-hue at render time — see default.vue.
 */

export interface BrandConfig {
    name: string
    tagline: string
    /** CSS linear-gradient value (e.g. 'linear-gradient(135deg,#6366f1,#3b82f6)'). Empty = use accent-hue fallback. */
    gradient: string
}

interface WindowAppConfig {
    APP_CONFIG?: Partial<{
        brandName: string
        brandTagline: string
        brandGradient: string
    }>
}

const FALLBACKS: BrandConfig = {
    name: 'Media Server Pro',
    tagline: 'Your Library',
    gradient: '',
}

export function useBrandConfig(): ComputedRef<BrandConfig> {
    // useAppConfig() is SSR-safe and typed once augmented; we access .brand
    // defensively because app.config.ts is author-editable.
    const app = useAppConfig() as { brand?: Partial<BrandConfig> }
    const buildDefaults: Partial<BrandConfig> = app.brand ?? {}

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
        }
    })

    return computed<BrandConfig>(() => ({
        name: runtime.value.name || buildDefaults.name || FALLBACKS.name,
        tagline: runtime.value.tagline || buildDefaults.tagline || FALLBACKS.tagline,
        gradient: runtime.value.gradient || buildDefaults.gradient || FALLBACKS.gradient,
    }))
}
