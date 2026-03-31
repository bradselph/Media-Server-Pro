# Frontend Contract Gaps

Generated: 2026-03-31  
Severity: 2 CRITICAL + 2 HIGH + 2 MEDIUM + 1 LOW

---

## CRITICAL ISSUES (Fix This Week)

### 1. Four Phantom Suggestion Endpoints (BREAKS RECOMMENDATIONS UI)

**File**: composables/useApiEndpoints.ts lines 172-186  
**Severity**: CRITICAL  

Frontend calls endpoints NOT in OpenAPI spec:

- /api/suggestions/profile (getMyProfile) - NOT IN SPEC
- /api/suggestions/recent (getRecent) - NOT IN SPEC  
- /api/suggestions/new (getNewSinceLastVisit) - NOT IN SPEC
- /api/suggestions/on-deck (getOnDeck) - NOT IN SPEC

**What the spec actually has**:
- GET /api/suggestions - YES
- GET /api/suggestions/trending - YES
- GET /api/suggestions/similar - YES
- GET /api/suggestions/continue - YES
- GET /api/suggestions/personalized - YES

**Where it breaks**:
pages/index.vue:131-143 - Recommendation loader calls all 6, fails on the 4 phantom ones

**User impact**: 3 recommendation sections appear blank (no error, no data)

**Fix** (5 min): Delete the 4 phantom methods from useApiEndpoints.ts useSuggestionsApi() or implement them in backend

---

### 2. Silent Recommendation Failures - No Error Display

**File**: pages/index.vue line 144  
**Severity**: CRITICAL  

Errors are caught and swallowed with NO user feedback:

```javascript
try {
  const [cw, tr, rec, recent, newSince, deck] = await Promise.allSettled([...])
  // ...
} catch { /* non-critical */ }
// NO ERROR MESSAGE, NO SPINNER, NOTHING
```

**User experience**: Page loads, recommendations section is blank, user has no idea what happened

**Fix** (20 min): Add loading state + error display for recommendations

---

## HIGH PRIORITY (Fix Next Week)

### 3. HLS Type Mismatch - Missing media_path Field

**File**: types/api.ts lines 169-193  
**Severity**: HIGH  

OpenAPI spec includes media_path in HLSAvailability and HLSJob responses, but TypeScript types don't capture it.

**Impact**: Silent data loss - backend returns field, frontend ignores it

**Fix** (5 min): Add `media_path?: string` to HLSAvailability and HLSJob interfaces

---

### 4. Player Detail Failures - Silent Error Handling

**File**: pages/player.vue lines 139-143  
**Severity**: HIGH  

Secondary suggestion loads fail silently:
- Similar media section appears empty
- Thumbnail previews don't load
- No error message or retry button

**Fix** (20 min): Add loading states and error display for similar and preview loads

---

## MEDIUM PRIORITY (Fix This Sprint)

### 5. Missing MediaItem Fields in TypeScript

**File**: types/api.ts lines 85-107  
**Severity**: MEDIUM  

OpenAPI spec defines 3 fields missing from TS types:
- path (string)
- is_remote (boolean)
- remote_url (string)

**Impact**: Low - frontend doesn't use these fields, but type safety is incomplete

**Fix** (5 min): Add optional fields to MediaItem interface

---

### 6. blur_hash Type Drift

**File**: types/api.ts line 99  
**Severity**: MEDIUM  

TypeScript has `blur_hash` field not documented in OpenAPI spec (likely frontend-only for placeholder images)

**Fix** (2 min): Document or add to spec

---

## LOW PRIORITY

### 7. Non-JSON Response Type Lie

**File**: composables/useApi.ts line 41  
**Severity**: LOW  

Non-JSON responses are cast to `T` as undefined, which is technically a type lie

**Fix** (5 min): Change return type to Promise<T | void>

---

## Phantom Calls - Details

### API Endpoints Called by Frontend

✓ WORKING:
- /api/auth/login
- /api/auth/logout
- /api/auth/session
- /api/auth/register
- /api/media
- /api/media/{id}
- /api/playback
- /api/playback/batch
- /api/hls/check
- /api/hls/status/{id}
- /api/hls/generate
- /api/playlists
- /api/playlists/{id}
- /api/watch-history
- /api/suggestions
- /api/suggestions/trending
- /api/suggestions/similar
- /api/suggestions/continue
- /api/suggestions/personalized
- /api/ratings
- /api/upload
- /api/favorites
- /api/thumbnails/previews
- /api/thumbnails/batch
- /api/browse/categories
- /api/preferences
- /api/storage-usage
- /api/permissions
- /api/server-settings
- /api/auth/tokens
- /api/auth/change-password
- /api/auth/data-deletion-request
- (plus all admin endpoints)

✗ PHANTOM (NOT IN SPEC):
- /api/suggestions/profile
- /api/suggestions/recent
- /api/suggestions/new
- /api/suggestions/on-deck

---

## Type Mismatches

| Type | Missing/Extra Fields | Impact |
|------|----------------------|--------|
| HLSAvailability | Missing: media_path | Type safety gap |
| HLSJob | Missing: media_path | Type safety gap |
| MediaItem | Missing: path, is_remote, remote_url | Low (not used by UI) |
| User | Missing: previous_last_login | Low (not used by UI) |
| MediaItem | Extra: blur_hash | Likely OK (placeholder) |

---

## Auth & Error Handling Status

✓ Auth flow is correct (session cookie + 401 redirect to login)
✓ Most pages display errors via toast
✗ index.vue: Recommendations don't show errors
✗ player.vue: Details don't show errors

---

## Fix Priority

| Priority | Fix | Time | Impact |
|----------|-----|------|--------|
| 1 | Remove phantom suggestions or implement | 5min | Breaks recommendations UI |
| 2 | Add error display to recommendations | 20min | No user feedback |
| 3 | Fix HLS media_path type | 5min | Type safety |
| 4 | Add player error handling | 20min | UX gap |
| 5 | Complete MediaItem types | 5min | Type completeness |
| 6 | Document blur_hash | 2min | Code clarity |

**Total estimated time**: 62 minutes (critical + high priority items)

---

Generated: 2026-03-31
