# Frontend API Consumption Map

> Generated: 2026-03-31  
> Source: web/nuxt-ui/  
> Contract: api_spec/openapi.yaml

## Coverage Summary

- **Total spec routes**: 147+ documented endpoints
- **Covered by frontend**: ~85 (57%)
- **Phantom calls** (not in spec): 4 CRITICAL gaps
- **Dead spec routes**: 62+ (mostly admin/diagnostic)
- **Type mismatches**: 3 identified
- **Auth mismatches**: 0 (correct)

---

## CRITICAL ISSUES (Runtime Risk)

### Phantom Endpoints - Frontend calls routes NOT in OpenAPI spec

| Endpoint | Called By | Risk | Evidence |
|----------|-----------|------|----------|
| `/api/suggestions/profile` | composables/useApiEndpoints.ts:172 | CRITICAL 404 | getMyProfile() |
| `/api/suggestions/recent` | composables/useApiEndpoints.ts:174 | CRITICAL 404 | getRecent() |
| `/api/suggestions/new` | composables/useApiEndpoints.ts:181 | CRITICAL 404 | getNewSinceLastVisit() |
| `/api/suggestions/on-deck` | composables/useApiEndpoints.ts:185 | CRITICAL 404 | getOnDeck() |

**Impact**: 
- pages/index.vue:131-143 calls Promise.allSettled() on 6 suggestions endpoints
- 4 of them don't exist in backend spec
- Errors swallowed (line 144), no user feedback
- Recommendation sections appear blank (no data, no error message)

**Evidence**: OpenAPI spec has only these suggestion endpoints:
- ✓ GET /api/suggestions
- ✓ GET /api/suggestions/trending
- ✓ GET /api/suggestions/similar
- ✓ GET /api/suggestions/continue
- ✓ GET /api/suggestions/personalized
- ✗ /api/suggestions/profile (MISSING)
- ✗ /api/suggestions/recent (MISSING)
- ✗ /api/suggestions/new (MISSING)
- ✗ /api/suggestions/on-deck (MISSING)

---

## API Layer Inventory

### composables/useApi.ts
- Core HTTP wrapper, unwraps Go envelope
- Credentials: 'include' (sends session_id cookie)
- 401 handling: redirects to /login via window.location.replace()
- Non-JSON: returns undefined (potential type lie)

### composables/useApiEndpoints.ts (~650 lines)
18 exported factory functions:
- useApiEndpoints() - auth + preferences ✓
- useMediaApi() - media browsing ✓
- useHlsApi() - HLS status/generation ✓
- usePlaybackApi() - position tracking ✓
- useWatchHistoryApi() - history ✓
- useSuggestionsApi() - **HAS PHANTOM CALLS** ✗
- useStorageApi() - storage usage ✓
- usePlaylistApi() - playlists ✓
- useSettingsApi() ✓
- useVersionApi() ✓
- useAgeGateApi() ✓
- useRatingsApi() ✓
- useCategoryBrowseApi() ✓
- useUploadApi() ✓
- useAdminApi() - 47 sub-endpoints ✓
- useAnalyticsApi() ✓
- useFavoritesApi() ✓
- useAPITokensApi() ✓

### composables/useHLS.ts
- Manages hls.js initialization
- Polls /api/hls/check and /api/hls/status
- Correct response handling
- Debounced checks, max 30min polling, 10 consecutive error limit

---

## Store Calls

### auth.ts
- Login: POST /api/auth/login ✓
- Session: GET /api/auth/session ✓
- Logout: POST /api/auth/logout ✓

### playback.ts
- Save: POST /api/playback ✓
- Load: GET /api/playback ✓
- Batch: GET /api/playback/batch ✓

---

## Page API Calls

### pages/index.vue
Working:
- GET /api/media (grid)
- GET /api/media/categories
- GET /api/suggestions (public)
- GET /api/suggestions/trending
- GET /api/suggestions/continue
- GET /api/suggestions/personalized
- GET /api/favorites
- GET /api/thumbnails/batch
- GET /api/playback/batch

Broken (lines 131-143):
- GET /api/suggestions/profile ✗
- GET /api/suggestions/recent ✗
- GET /api/suggestions/new ✗
- GET /api/suggestions/on-deck ✗

Error handling gap: line 144 swallows errors, no user feedback

### pages/player.vue
All calls correct:
- GET /api/media/{id}
- GET /api/suggestions/similar
- GET /api/suggestions/personalized
- GET /api/thumbnails/previews
- GET /api/playback
- POST /api/playback
- POST /api/hls/generate
- POST /api/ratings
- POST /api/playlists/{id}/items

### pages/login.vue
- GET /api/server-settings
- POST /api/auth/login
Status: ✓ Correct

### pages/signup.vue
- POST /api/auth/register
- GET /api/auth/session
Status: ✓ Correct

### pages/profile.vue
All calls correct:
- GET /api/storage-usage
- GET /api/permissions
- GET /api/preferences
- POST /api/preferences
- GET /api/watch-history
- DELETE /api/watch-history
- POST /api/auth/change-password
- POST /api/auth/data-deletion-request
- GET /api/ratings
- GET /api/auth/tokens
- POST /api/auth/tokens
- DELETE /api/auth/tokens/{id}

Error handling: Good - errors displayed via toast

### pages/favorites.vue
- GET /api/favorites ✓
- GET /api/media/{id} (batch) ✓
- DELETE /api/favorites/{id} ✓

### pages/categories.vue
- GET /api/browse/categories ✓
- GET /api/browse/categories?category=<cat> ✓

### pages/playlists.vue
- GET /api/playlists ✓
- POST /api/playlists ✓
- PUT /api/playlists/{id} ✓
- DELETE /api/playlists/{id} ✓
- POST /api/playlists/{id}/copy ✓
- DELETE /api/playlists/{id}/clear ✓

---

## Type Alignment Issues

| Type | Issue | Impact |
|------|-------|--------|
| HLSAvailability | Missing `media_path` field from spec | Silent data loss |
| HLSJob | Missing `media_path` field from spec | Silent data loss |
| MediaItem | Missing `path`, `is_remote`, `remote_url` | Not used by UI (safe) |
| User | Missing `previous_last_login` | Not used by UI (safe) |
| MediaItem | Extra `blur_hash` not in spec | OK - likely legacy |

---

## Non-JSON Endpoints

Status: ✓ All correct with proper encoding
- /media?id=<id> - Direct stream
- /download?id=<id> - Download
- /thumbnail?id=<id> - Thumbnail
- /hls/<id>/master.m3u8 - HLS playlist

---

## Auth Flow

Status: ✓ Correct
- middleware/auth.ts protects authenticated routes
- 401 redirects to /login?redirect=<path>
- Session cookie (session_id) sent with credentials: 'include'

---

## TDZ Safety

Status: ✓ SAFE
- useApiEndpoints.ts imports useApi explicitly (line 37)
- Avoids circular dependency through #imports
- All composables safe from TDZ

---

## Summary

✓ 85+ endpoints correctly implemented
✗ 4 phantom suggestion endpoints (critical fix needed)
⚠ 3 type mismatches (media_path missing)
⚠ Error handling gaps in index.vue (no user feedback)

Estimated fix time: 45 minutes
