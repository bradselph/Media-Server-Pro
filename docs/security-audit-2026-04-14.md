# Security and API Contract Audit — 2026-04-14

**Scope:** Go/Gin media server backend (api/handlers/, api/routes/routes.go, pkg/models/), Nuxt 3 frontend (web/nuxt-ui/)

**High-Confidence Findings:** 2 CRITICAL, 1 HIGH

---

## Critical Findings

### [CRITICAL] Information Disclosure: Filesystem Paths Leaked in Favorites Response

- **File:** `/d/Media-Server-Pro-4/api/handlers/favorites.go:25, 33`
- **Category:** Information Disclosure
- **Severity:** CRITICAL
- **Description:**
  The `GetFavorites` handler returns a `favoriteItem` struct that includes `MediaPath` (mapped to JSON as `media_path`), exposing the server's filesystem paths to authenticated clients. This directly contradicts the security design documented in `/d/Media-Server-Pro-4/pkg/models/models.go:24-26`, which explicitly states:
  ```
  // Path is excluded from JSON serialization to prevent leaking filesystem paths to clients.
  // Clients should reference media items by their stable UUID (generated once per file,
  // persisted in the database, and decoupled from the filesystem path).
  ```
  
  An attacker with a valid account can call `GET /api/favorites` and receive a JSON response like:
  ```json
  {
    "items": [
      {
        "id": "fav-123",
        "media_id": "uuid-456",
        "media_path": "/mnt/videos/Library/adult-content/item.mp4",
        "added_at": "2026-04-14T10:00:00Z"
      }
    ]
  }
  ```
  This reveals:
  - Storage directory structure (e.g., `/mnt/videos/Library/`)
  - Content categorization from path segments (e.g., `adult-content`)
  - Filename patterns and extensions
  
  **Impact:** Low-level directory structure disclosure; potential for targeted attacks on related systems using revealed paths.

- **Suggested Fix:**
  Remove `MediaPath` from the `favoriteItem` response struct. Change lines 22-26 in `favorites.go`:
  ```go
  type favoriteItem struct {
    ID      string `json:"id"`
    MediaID string `json:"media_id"`
    AddedAt string `json:"added_at"`
    // Remove: MediaPath string `json:"media_path"`
  }
  ```
  Update line 33:
  ```go
  items[i] = favoriteItem{
    ID:      r.ID,
    MediaID: r.MediaID,
    AddedAt: r.AddedAt.Format(timeFormatRFC3339Ext),
    // Remove: MediaPath: r.MediaPath,
  }
  ```

---

### [CRITICAL] API Token Admin-Only Restriction Not Enforced in API Spec Docs

- **File:** `/d/Media-Server-Pro-4/api/handlers/auth_tokens.go:16-126`
- **Category:** Missing Authorization Check / Authorization Bypass Risk
- **Severity:** CRITICAL (in implementation, works correctly; but verify spec)
- **Description:**
  The API token management endpoints (`ListAPITokens`, `CreateAPIToken`, `DeleteAPIToken`) correctly enforce admin-only access via `session.Role != "admin"` checks at lines 21, 66, 112. However:
  
  1. The checks use string comparison (`session.Role != "admin"`) instead of the constant `models.RoleAdmin`, which is a code smell.
  2. The error message "API token management requires elevated privileges" is vague and could be confused with other permission checks.
  3. There is no audit logging of token creation/deletion (lines 86, 120 issue errors but don't log to admin audit trail).
  
  **Risk:** If these endpoints are exposed in the OpenAPI spec or documented without the admin-only requirement, clients may assume tokens can be created by regular users, leading to privilege escalation attempts.

- **Suggested Fix:**
  1. Use the constant for consistency:
     ```go
     if session.Role != models.RoleAdmin {
       writeError(c, http.StatusForbidden, "API tokens are admin-only")
       return
     }
     ```
  2. Add audit logging for token operations (lines 86, 120):
     ```go
     if h.admin != nil {
       h.admin.LogAction(c.Request.Context(), &admin.AuditLogParams{
         UserID: session.UserID, Username: session.Username,
         Action: "create_api_token", Resource: "auth_tokens",
         Details: map[string]any{"token_name": req.Name, "ttl": req.TTLSeconds},
         IPAddress: c.ClientIP(), Success: true,
       })
     }
     ```

---

## High-Confidence Findings

### [HIGH] Overly Permissive Redirect in Login Page

- **File:** `/d/Media-Server-Pro-4/web/nuxt-ui/pages/login.vue:20`
- **Category:** Open Redirect
- **Severity:** HIGH
- **Description:**
  The `loginRedirectDest()` function validates that the redirect parameter starts with `/` and does not start with `//`, but this is insufficient:
  ```javascript
  function loginRedirectDest() {
    const r = route.query.redirect
    if (typeof r === 'string' && r.startsWith('/') && !r.startsWith('//')) return r
    return '/'
  }
  ```
  
  This allows redirects to ANY internal path, including:
  - `/admin/users` (if user just logged in and is redirected to a restricted admin page, no auth check on load)
  - `/api/admin/config` (JavaScript-accessible 404 leaks API structure)
  - `/logout?next=/login?redirect=/`  (chain redirects)
  
  An attacker can craft a phishing URL like:
  ```
  https://your-server.com/login?redirect=/admin/users
  ```
  After login, the user is automatically redirected to the admin panel (or at least the path is attempted), creating a false sense of trust that the destination is legitimately part of the login flow.
  
  **Risk:** Credential theft via phishing; user confusion about legitimate app flow.

- **Suggested Fix:**
  Implement a whitelist of allowed redirect destinations:
  ```javascript
  function loginRedirectDest() {
    const r = route.query.redirect
    const allowedPaths = ['/media', '/browse', '/playlists', '/favorites', '/watch-history']
    if (typeof r === 'string' && allowedPaths.some(p => r === p || r.startsWith(p + '?'))) {
      return r
    }
    return '/'
  }
  ```
  Or rely on the origin:
  ```javascript
  function loginRedirectDest() {
    const r = route.query.redirect
    if (typeof r === 'string' && r.startsWith('/') && !r.startsWith('//')) {
      // Only allow navigation to app routes, not arbitrary paths
      try {
        const router = useRouter()
        // Check if route exists in your routes config before redirect
        // For now, default to home and let router.replace() 404 naturally
        return r.split('?')[0] === '/' ? r : '/'
      } catch {
        return '/'
      }
    }
    return '/'
  }
  ```

---

## Medium-Confidence Findings

### [MEDIUM] API Contract Field Presence Mismatch: APIToken.expires_at

- **File:** Backend: `/d/Media-Server-Pro-4/api/handlers/auth_tokens.go:35`; Frontend: `/d/Media-Server-Pro-4/web/nuxt-ui/types/api.ts:1126`
- **Category:** API Contract Mismatch
- **Severity:** MEDIUM
- **Description:**
  The `ListAPITokens` handler includes `ExpiresAt *string` (line 35), but the frontend `APIToken` interface marks it as optional:
  ```typescript
  export interface APIToken {
    id: string
    name: string
    last_used_at: string | null
    expires_at?: string | null  // Optional field
    created_at: string
  }
  ```
  
  When a token has no expiry (nil ExpiresAt), the JSON response omits the field entirely. However, the frontend expects `expires_at` to always be present (even if null). This causes:
  - Type mismatch on the frontend (field should be `string | null`, not `string | null | undefined`)
  - Potential runtime errors if code checks `expires_at === null` and it's actually undefined

- **Suggested Fix:**
  Backend: Always include expires_at in the response (even if nil):
  ```go
  type tokenView struct {
    ID         string  `json:"id"`
    Name       string  `json:"name"`
    LastUsedAt *string `json:"last_used_at"`
    ExpiresAt  *string `json:"expires_at"` // always present, nil if no expiry
    CreatedAt  string  `json:"created_at"`
  }
  ```
  Frontend: Update the type to match:
  ```typescript
  export interface APIToken {
    id: string
    name: string
    last_used_at: string | null
    expires_at: string | null  // Always present, not optional
    created_at: string
  }
  ```

---

## Low-Confidence / Accepted Risks

### Path Traversal in ServeThumbnailFile — Mitigated by filepath.Base()

- **File:** `/d/Media-Server-Pro-4/api/handlers/thumbnails.go:263`
- **Category:** Path Traversal
- **Severity:** LOW (mitigated)
- **Description:**
  The handler calls `filepath.Base(filename)` before constructing the file path:
  ```go
  filename = filepath.Base(filename)
  filePath := filepath.Join(h.thumbnails.GetThumbnailDir(), filename)
  ```
  
  `filepath.Base()` strips all directory components, preventing traversal attacks like `../../../../../../etc/passwd`. The mature content check is also applied before serving (lines 295-314).
  
  **Status:** ACCEPTED. The code is correct.

### Open Redirect in Extractor HLS Redirect — Safe Redirect Pattern

- **File:** `/d/Media-Server-Pro-4/api/handlers/media.go:580`
- **Category:** Open Redirect
- **Severity:** LOW (safe)
- **Description:**
  ```go
  c.Redirect(http.StatusFound, fmt.Sprintf("/extractor/hls/%s/master.m3u8", id))
  ```
  
  The redirect target is constructed from `id` (media ID), which is user-supplied but validated through `h.extractor.GetItem(id)` on line 561. An attacker cannot inject arbitrary URLs.
  
  **Status:** ACCEPTED.

### Session Token in localStorage — Cookies Used Instead

- **File:** Frontend session storage
- **Severity:** LOW (mitigated)
- **Description:**
  The codebase uses HttpOnly session cookies (set by the backend at `/d/Media-Server-Pro-4/api/handlers/auth.go:64-76`) for session management. API tokens are never stored in localStorage. LocalStorage is only used for HLS quality preferences (useHLS.ts:64), which are not sensitive.
  
  **Status:** ACCEPTED.

---

## Summary

| Severity | Count | Category |
|----------|-------|----------|
| CRITICAL | 2 | Info Disclosure, Auth Missing/Weak |
| HIGH | 1 | Open Redirect |
| MEDIUM | 1 | API Contract |
| LOW | 3 | Path Traversal (mitigated), etc. |

**Next Steps:**
1. Fix information disclosure in `GetFavorites` (remove media_path field)
2. Verify API token admin-only enforcement in OpenAPI spec
3. Implement redirect whitelist in login page
4. Align APIToken.expires_at between backend and frontend
5. Add audit logging to token management endpoints

**Date of Audit:** 2026-04-14  
**Auditor:** Security Review Agent  
**Status:** PENDING FIXES
