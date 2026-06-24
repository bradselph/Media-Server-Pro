---
name: project-guest-stream-login-redirect-2026-06-22
description: Guests were bounced to /login when streaming any media because the global 401 handler redirected unconditionally; now gated on isAuthenticated()
metadata:
  type: project
---

UNCOMMITTED (development). Bug: non-members (guests) attempting to stream **any** media — mature or not — were prompted to log in, even though `Streaming.RequireAuth` defaults to `false` (anonymous streaming allowed) and `/api/media/:id` + `/media?id=` are public for non-mature content.

**Root cause:** `web/nuxt-ui/composables/useApi.ts` `parseEnvelope` redirected to `/login` on **every** 401 (`if (res.status === 401) redirectToLogin()`). The player page (`pages/player.vue`) has no `auth` middleware and fires several **auth-only** background calls for guests: `useHLS` → `/api/hls/check` (`requireAuth()`), and `restorePosition`/`savePosition` → `/api/playback` (`requireAuth()`). Each 401 triggered a hard redirect before the caller's catch could fall back. Universality (mature or not) is the tell — the redirect wasn't the mature gate.

**Fix (frontend-only, no Go change — backend already allows guest streaming):**
- `stores/auth.ts`: added module-scoped `loggedInFlag` + exported `isAuthenticated()` (mirrors `privateSessionFlag` pattern so module-level useApi can read it pre-Pinia-mount). Set in `fetchSession` (`= !!user.value`), `login` (true), `logout` (false).
- `useApi.ts`: `if (res.status === 401 && isAuthenticated()) redirectToLogin()` — only redirect when a session was actually active (genuine expiry/revocation). Guests' optional auth-only 401s now fall through to the caller's catch (HLS → direct streaming; playback → silent no-op).

Mature content for guests still effectively requires login: `GetMedia` 401s → caught in `loadMedia` → shows the inline "marked as mature (18+), please log in" error block (not a forced redirect). Per [[project_requirements]]/[[project_adult_only]] only logged-in users can hold CanViewMature, so mature stays gated. typecheck green.
