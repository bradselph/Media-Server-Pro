# Public Adoption Recommendations — Landing Page & Guest Funnel

> Generated 2026-06-23 from a multi-lens audit (first-visit UX, signup funnel, SEO/discoverability,
> performance/trust). These are **recommendations only — nothing here is implemented yet.**
> All respect the project's standing constraints: never redesign page structure (add within the
> existing layout only), age verification is legally required (smooth it, never remove it),
> adult-content-only, no subtitles.

The "deployed projects page" = the public landing/home (`web/nuxt-ui/pages/index.vue`) plus the guest
funnel (`layouts/default.vue`, `signup.vue`, `login.vue`) and the SEO shell the Go server injects
(`api/handlers/shell.go`, `api/handlers/seo.go`, `web/server.go`).

The single biggest theme: **the guest's path is built for "log in," but the growth goal is "sign up."**
On an all-mature library, nearly every guest action currently dead-ends at a login form with no
registration incentive, and returning visitors stare at a blank age-gate screen on every load.

---

## Tier 1 — Quick wins (high impact, low effort). Do these first.

### 1. Kill the blank-screen flash for returning visitors (age gate)
- **Problem:** Every returning visitor sees an opaque white screen until a network round-trip to
  `/api/age-gate/status` resolves — the first thing users experience on every page load, directly
  raising bounce rate.
- **Fix:** In `checkAgeGate()` (`layouts/default.vue:32-41`), read the age-gate cookie from
  `document.cookie` first; if present, set `ageGateChecked.value = true` immediately so the page
  renders at once, then confirm server-side silently in the background. Removes the flash for ~95% of
  returning visitors with no legal risk (the cookie already proves prior acceptance).
- **Files:** `web/nuxt-ui/layouts/default.vue`

### 2. Add a Sign Up link to the mobile hamburger menu
- **Problem:** The mobile drawer's guest block (`default.vue:537-544`) shows only a Login link — there
  is **no path to registration on mobile** unless you know the URL. Mobile is typically 50–70% of
  first-visit adult traffic.
- **Fix:** Add a `<NuxtLink to="/signup">` styled as a primary action alongside Login, mirroring the
  desktop pattern at `default.vue:487-488`.
- **Files:** `web/nuxt-ui/layouts/default.vue`

### 3. Route mature-locked card clicks to `/signup`, not `/login`
- **Problem:** On an all-mature library, almost every card a guest clicks hits `matureGateHref`
  (`index.vue:915-918`), which returns `/login` — dropping users into a login form with no reason to
  register. The goal is **new accounts**, not returning logins.
- **Fix:** Return `/signup` when `!authStore.isLoggedIn` (`index.vue:917`); change overlay copy from
  "Sign in to view" to "Sign up to view" (`index.vue:1827`); apply the same to the player CTA
  (`player.vue:1545`).
- **Files:** `web/nuxt-ui/pages/index.vue`, `web/nuxt-ui/pages/player.vue`

### 4. Show Categories to guests + default guest sort to most-viewed
- **Problem:** Guests see only Home + Browse in the nav (`default.vue:157-160`) — no signal the
  category taxonomy exists. Combined with an A–Z alphabetical default sort (`index.vue:303-304`), the
  first-visit grid is the driest possible view of the library.
- **Fix:** Move the Categories push outside the `isLoggedIn` guard in `navLinks` (`default.vue:161`)
  and `mobileNavLinks` (`default.vue:179`). In `index.vue:303-304`, seed `sort_by:'views'`,
  `sort_order:'desc'` for guests when no query-string sort is present.
- **Files:** `web/nuxt-ui/layouts/default.vue`, `web/nuxt-ui/pages/index.vue`

### 5. Add adult-rating meta tags + fix the generic fallback description (SEO)
- **Problem:** Without the RTA label and `<meta name="rating" content="adult">`, Bing SafeSearch
  suppresses the site by default and Google misclassifies it — cutting off the primary organic channel
  for an adult site. The fallback meta description (`nuxt.config.ts:84`) currently reads "personal
  media library," which undersells every non-enriched page snippet.
- **Fix:** Add to `nuxt.config.ts` `app.head.meta`: `{name:'rating',content:'adult'}` and
  `{name:'RATING',content:'RTA-5042-1996-1400-1577-RTA'}`. Rewrite the fallback description to reflect
  the public streaming nature. Add `og:site_name` and `og:locale` (absent in both `nuxt.config.ts` and
  `shell.go` `shellMetaForDiscovery`).
- **Files:** `web/nuxt-ui/nuxt.config.ts`, `api/handlers/shell.go`

### 6. Fix legal compliance pages (placeholder emails + hardcoded brand)
- **Problem:** `2257.vue:11-12` falls back to `compliance@example.com` and `dmca.vue:8` to
  `dmca@example.com` when brand env vars are unset. These pages are **mandatory for 18 U.S.C. § 2257**;
  serving placeholder addresses live is a legal liability. `privacy.vue`/`terms.vue` also hardcode
  "Media Server Pro" instead of `useBrandConfig()`.
- **Fix:** Emit a `console.warn` + admin toast when `complianceEmail` contains `@example.com`. Replace
  hardcoded brand strings in `privacy.vue:15` and `terms.vue:13` with `brand.value.name`, following the
  `2257.vue:5-10` pattern.
- **Files:** `privacy.vue`, `terms.vue`, `2257.vue`, `dmca.vue`, `layouts/default.vue`
- **Note:** brand config needs real custodian/agent/email values before launch regardless.

### 7. Add `og:image` + JSON-LD `contentUrl` for social shares & video rich results
- **Problem:** Shares to Discord/X/iMessage/Reddit get no `og:image` from `shellMetaForDiscovery`
  (`shell.go:118-126`) — text-only cards. `playerJSONLD` (`shell.go:217-235`) omits `contentUrl`, which
  Google requires to surface `VideoObject` rich results.
- **Fix:** Add an `og:image` (absolute URL of a branded asset / OG banner) in `shellMetaForDiscovery`;
  add `ld['contentUrl'] = canonical` in `playerJSONLD`; optionally add `interactionStatistic` from
  `item.Views`.
- **Files:** `api/handlers/shell.go`, `web/nuxt-ui/nuxt.config.ts`

### 9. Add a value proposition to signup + upgrade the login cross-link
- **Problem:** `signup.vue` is a bare form with no statement of what signing up unlocks. The login
  page's registration link is small plain text below the card (`login.vue:169-173`), competing with
  error banners.
- **Fix:** Add a 1–2 line tagline above the signup form ("Create your free account to unlock mature
  content, save watch history, build playlists, and get personalized picks."). Move "Create one" inside
  the login card as a secondary `UButton`.
- **Files:** `web/nuxt-ui/pages/signup.vue`, `web/nuxt-ui/pages/login.vue`

---

## Tier 2 — High-value, medium effort

### 8. Make category pages crawlable (the highest-leverage SEO surface)
- **Problem:** `/categories/:id` pages are the most topically relevant landing pages for organic
  search ("watch [tag] videos") yet are invisible to crawlers: not in `spaRoutes`
  (`web/server.go:64`), not in the sitemap (`seo.go`), and `categories/[id].vue` has no
  `useSeoMeta`/`useHead` at all.
- **Fix:** (1) Add `/categories` and `/categories/*id` to `spaRoutes` (`web/server.go:64`); (2) extend
  `EnrichSPAShell` (`shell.go`) to resolve `/categories/<id>` → category name as title; (3) in
  `GetSitemap` (`seo.go`), append `baseURL+'/categories/'+cat.ID` at priority 0.7; (4) add `useSeoMeta`
  to `categories/[id].vue` using the already-fetched name/description.
- **Files:** `web/server.go`, `api/handlers/shell.go`, `api/handlers/seo.go`, `categories/[id].vue`

---

## Tier 3 — Performance & trust polish (mostly quick)

### 10. Drop the thumbnail pre-warm burst + fix skeleton grid mismatch (CLS)
- After `load()`, `index.vue:780-789` fires `getThumbnailBatch` + 50 `new Image()` — a ~51-request
  burst while the grid still renders. Remove it (native lazy-loading covers visible/below-fold).
  Separately, `MediaCardSkeleton.vue:9` uses `lg:grid-cols-5` but the real grid uses `xl:grid-cols-6`
  (`index.vue:1768`) — causing a layout shift on every load. Align them.
- **Files:** `web/nuxt-ui/pages/index.vue`, `web/nuxt-ui/components/MediaCardSkeleton.vue`

### 11. Gate the server version string to admins only
- `default.vue:560` renders the exact build version in the public footer — a free fingerprint for
  vulnerability scanners, zero value to visitors. Change `v-if="serverVersion"` →
  `v-if="serverVersion && authStore.isAdmin"`.
- **Files:** `web/nuxt-ui/layouts/default.vue`

### 12. Skip the cookie-consent API call for returning visitors
- `CookieConsentBanner.vue:39` hits `/api/cookie-consent/status` on every load for every visitor. Check
  `localStorage('msp-cookie-consent')` first and return early if a decision is stored — frees a
  first-paint connection slot.
- **Files:** `web/nuxt-ui/components/CookieConsentBanner.vue`

### 16. Add `apple-touch-icon` + web manifest for mobile bookmarking
- `nuxt.config.ts:73-75` declares only `favicon.svg`; iOS uses a screenshot for the home-screen icon and
  there's no PWA manifest. Add a 180×180 `apple-touch-icon`, a minimal `site.webmanifest` (with a
  neutral `short_name` for discreet bookmarking), and `<meta name="theme-color">`.
- **Files:** `web/nuxt-ui/nuxt.config.ts`, `web/nuxt-ui/public/`

---

## Tier 4 — Conversion polish (medium impact)

- **13. "Forgot password?" guidance** — no link and no `/forgot-password` route exists; even a
  "Contact the site administrator" line in `login.vue` prevents silent churn.
- **14. Mature-unlock hint in the welcome toast** — new users default to `can_view_mature:false` and
  see locked overlays with no explanation (`index.vue:875-882`). Add a follow-up toast linking to
  `/profile` when the flag is false.
- **17. Age-gate as blur overlay, not opaque block** — replace the solid overlay (`default.vue:361`)
  with `backdrop-blur` so the content silhouette shows while status resolves (pairs with #1).
- **18. Pass `?reason=mature` to the auth page + contextual banner** — explain *why* the redirect
  happened ("Create a free account to access mature content") on `login.vue`/`signup.vue`.
- **19. Guest-tier explainer banner on home** — a dismissible guest-only `UAlert` wiring
  `libraryStats.total_count` ("Browsing as guest — sign up free for full access to all X,XXX items +
  watch history + personalized picks").
- **20. Replace confirm-password with a show/hide toggle on signup** — with no email reset flow, the
  4th input is a common abandonment point; `PasswordInput` likely already supports a toggle.

---

## Tier 5 — Larger / backend-scoped bets

- **15. Invalidate sitemap + shell-discovery caches on ingest** — new media is invisible to Googlebot
  for up to 1h (`seo.go:43`) / 10min (`shell.go`). Add `InvalidateSitemapCache()` called from the scan
  /import completion hooks.
- **21. View-weighted sitemap priority + `UpdatedAt` lastmod** — all player entries use hardcoded
  priority 0.6 and `lastmod=date_added` (`seo.go:90-92`); admin edits don't bump lastmod. Add
  `UpdatedAt` to `MediaItem` and compute priority from views.
- **22. Show mature suggestions to age-gate-verified guests** — the hero + Popular row exclude
  `is_mature` for guests (`suggestions.go:104`), so on an all-adult library the most representative
  content never shows to new visitors. After the age gate passes, treat verified guests as
  `canViewMature=true` via a session/cookie flag. *(High impact, but needs careful legal review.)*
- **23. Consolidate first-load calls into `/api/init`** — a first guest fires ≥6 parallel calls on
  mount (age-gate, cookie-consent, version, settings, suggestions, media). Merge 3–4 into one response;
  defer `/api/version` to `requestIdleCallback`.

---

### Suggested sequencing
1. **Today (one PR):** #1, #2, #3, #4, #9 — the guest→signup funnel rewire + blank-screen fix. Highest
   ROI, all quick, all front-end only.
2. **This week:** #5, #6, #7, #11, #12, #16 — SEO/trust hardening + the compliance-email safety net.
3. **Next:** #8 (category SEO), #10 (perf), then the Tier 4 conversion polish.
4. **Backlog:** Tier 5 backend work, with #22 gated on legal sign-off.
