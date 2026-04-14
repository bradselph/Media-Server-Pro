# Deep Static Code Audit — 2026-04-13

**Auditor:** Deep Static Analysis  
**Scope:** Go backend handlers + auth + media; Vue3 frontend (index, player, profile)  
**Status:** 7 behavioral bugs identified; 2 previously fixed by team lead

---

## Previously Fixed (Team Lead Corrections)

1. ✅ **handler.go deletionRequests nil GORM panic** — Fixed with lazy init under mutex
2. ✅ **suggestions.go IP-based guest profiles** — Now skipped during DB persist

---

## NEW FINDINGS

### 🔴 CRITICAL

#### BUG #1: Missing `resolveMediaPathOrReceiver()` handler function
- **File:** `api/handlers/media.go:825, 864, 1438` (invocations without implementation)
- **Severity:** CRITICAL — Runtime panic on first playback position call
- **Root Cause:** Function is called but never defined in handlers package
- **Impact:** GetPlaybackPosition, TrackPlayback, RecordRating all crash immediately
- **Test Case:** Call `/api/playback?id=any` as authenticated user → panic
- **Fix Required:**
```go
// Add to handlers.go or handlers/media.go
func (h *Handler) resolveMediaPathOrReceiver(c *gin.Context, mediaID string) (path string, name string, ok bool) {
  item, err := h.media.GetMediaByID(mediaID)
  if err == nil && item != nil {
    return item.Path, item.Name, true
  }
  if h.receiver != nil {
    if ri := h.receiver.GetMediaItem(mediaID); ri != nil {
      return "receiver:" + mediaID, ri.Name, true
    }
  }
  writeError(c, http.StatusNotFound, "Media not found")
  return "", "", false
}
```

---

#### BUG #2: Storage quota bypass via client-reported zero size
- **File:** `api/handlers/upload.go:73-104` (checkUploadStorageQuota)
- **Severity:** CRITICAL — Security vulnerability
- **Root Cause:** Quota check trusts client `Size` header; when `Size=0`, assumes worst-case (`maxFileSize`). Actual written bytes via `MaxBytesReader` (line 47) truncate silently.
- **Attack:** Client sends multipart with `Size=0` (triggers 10GB worst-case check), but only 1MB is actually written (truncated). Client repeats 10 times → 10MB written, quota thinks 100GB used.
- **Impact:** Quota enforcement is unenforceable; users can bypass storage limits
- **Test Case:**
  ```bash
  # Craft multipart with Size=0, 100MB file body
  # MaxBytesReader truncates to maxFileSize (let's say 10GB)
  # Quota check: "100GB incoming" → rejects
  # But actual file: truncated to 10GB limit, then smaller file written
  # Quota deducted for 10GB, actual use 1MB → bypass
  ```
- **Fix Required:** Validate actual bytes written, not client-reported size:
```go
// After successful upload, before quota deduction
for _, result := range results {
  // Get actual file size from filesystem or result.Size (which was checked during write)
  // Don't trust multipart header; use what was actually persisted
  if result.Size > 0 {
    totalActual += result.Size
  }
}
// Then check: freshUser.StorageUsed + totalActual <= quota
```

---

#### BUG #3: Race condition: Admin user creation during preference update
- **File:** `api/handlers/auth.go:324-342` (UpdatePreferences)
- **Severity:** HIGH — State corruption
- **Root Cause:** Double-check locking not implemented. Two concurrent requests both call `GetUser()`, both miss cache, both call `CreateUser()`. Second fails silently.
- **Impact:** Admin's preference update silently fails; preferences never saved. User sees "success" but changes lost.
- **Test Case:**
  1. Admin logged in with no user record (only admin session)
  2. Two browser tabs simultaneously call `PATCH /api/preferences`
  3. Both threads: `GetUser()` → nil, `CreateUser()` → first succeeds, second fails
  4. First request: updates prefs on the new user record
  5. Second request: silently swallows error (line 338), returns without updating
- **Fix Required:**
```go
if session.Role == models.RoleAdmin {
  // Use GetOrCreate under lock to prevent TOCTOU
  randomPassword, pwdErr := h.auth.GenerateSecurePassword(32)
  if pwdErr != nil {
    randomPassword = "FALLBACK_" + generateRandomString(24)
  }
  _, createErr := h.auth.CreateUser(c.Request.Context(), auth.CreateUserParams{
    Username: session.Username,
    Password: randomPassword,
    Email:    "",
    UserType: "admin",
    Role:     models.RoleAdmin,
  })
  // Only treat as error if it's NOT "user already exists"
  if createErr != nil && !errors.Is(createErr, auth.ErrUserExists) {
    h.log.Warn("Could not create admin user record: %v", createErr)
    writeError(c, http.StatusServiceUnavailable, "User record could not be created")
    return
  }
}
// Proceed to fetch user + update (will now succeed because record exists or already existed)
```

---

### 🟠 HIGH

#### BUG #4: Pagination state not cleared on selection mode toggle
- **File:** `web/nuxt-ui/pages/index.vue:59-71` (toggleSelectionMode)
- **Severity:** HIGH — Silent state divergence
- **Root Cause:** `toggleSelectionMode()` clears `selectedIds` but doesn't reset `params.page` to 1
- **Impact:** User enters selection mode at page 2, exits, page UI shows items from page 1 but URL still has `page=2`. Clicking pagination now jumps unexpectedly.
- **Test Case:**
  1. Load home page
  2. Click pagination → page 2
  3. Click "Select" button → enter selection mode
  4. Click "Cancel" → exit selection mode
  5. Page UI shows items 1-24 but internally `params.page === 2`
  6. Click next page button → loads page 3, not page 2
- **Fix Required:**
```typescript
function toggleSelectionMode() {
  selectionMode.value = !selectionMode.value
  if (!selectionMode.value) {
    selectedIds.value = new Set()
    params.page = 1  // Add this line
  }
}
```

---

#### BUG #5: Concurrent upload quota race — ordering violation
- **File:** `api/handlers/upload.go:107-195` (UploadMedia)
- **Severity:** HIGH — Resource exhaustion
- **Root Cause:** File is registered in media index (line 160) before quota is deducted (line 186). Between these two operations, the file is visible in the library but quota hasn't been charged yet.
- **Impact:** 
  - User A uploads 5GB, registration succeeds (line 160)
  - User A receives response, deletes the file (quota restored)
  - Meanwhile, if quota deduction fails or is slow, file persists and quota isn't properly tracked
  - Repeat: quota tracking becomes inaccurate
- **Test Case:**
  1. User at 9GB quota, tries to upload 2GB file
  2. File written and registered in media index
  3. Between line 160 and 186, admin deletes user's old file (quota restored to 9GB)
  4. Quota deduction now succeeds (9GB - 2GB = 7GB)
  5. But file was visible in library briefly with old quota state
- **Fix Required:** Either:
  - **Option A:** Deduct quota BEFORE RegisterUploadedFile, or
  - **Option B:** Make both operations atomic (transaction or single DB call)
```go
// Suggested: Check quota AFTER all files registered but BEFORE returning success
for _, fh := range fileHeaders {
  result, err := h.upload.ProcessFileHeader(fh, ...)
  // ... register file ...
  totalAdded += result.Size
}
// NOW verify final quota state before returning
if totalAdded > 0 {
  freshUser, _ := h.auth.GetUser(c.Request.Context(), user.Username)
  if freshUser != nil && userType.StorageQuota > 0 {
    if freshUser.StorageUsed + totalAdded > userType.StorageQuota {
      // Reject the entire batch; clean up registered files
      // (requires cleanup logic)
      writeError(c, http.StatusForbidden, "Storage quota exceeded")
      return
    }
  }
}
// Safe to deduct quota
if totalAdded > 0 && user.ID != "admin" {
  h.auth.AddStorageUsed(c.Request.Context(), user.ID, totalAdded)
}
```

---

### 🟡 MEDIUM

#### BUG #6: Silent clipboard copy failure in profile.vue
- **File:** `web/nuxt-ui/pages/profile.vue:320-328` (copyToken)
- **Severity:** MEDIUM — UX bug, data loss risk
- **Root Cause:** Empty catch block; clipboard write failure is silently ignored
- **Impact:** User thinks token was copied when it wasn't. They close modal and lose access to the token.
- **Test Case:** 
  1. Create API token
  2. On a browser without clipboard permissions or in incognito mode, click "Copy to clipboard"
  3. Silent failure; no error toast
  4. User closes modal thinking it's copied
- **Fix Required:**
```typescript
async function copyToken() {
  if (!revealedToken.value) return
  try {
    await navigator.clipboard.writeText(revealedToken.value)
    toast.add({ title: 'Token copied to clipboard', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    // Add this: report error to user
    toast.add({ title: 'Failed to copy token to clipboard', color: 'error', icon: 'i-lucide-x' })
  }
}
```

---

#### BUG #7: CSV export can fail after headers sent
- **File:** `api/handlers/auth.go:737-803` (ExportWatchHistory)
- **Severity:** MEDIUM — File corruption risk
- **Root Cause:** Response writer is partially written before all CSV rows are buffered. If a row write fails partway through, headers are already sent and error response can't be written.
- **Impact:** User downloads truncated CSV with no indication of corruption; they lose data on import
- **Current Code:** Lines 787-791 handle this with a trailer comment, which is good, but the `c.Writer.WriteString("\n# ERROR...")` at line 801 will be silently dropped if the write already started failing.
- **Risk:** The try to write trailer (line 801) after header send (line 798) is unreliable.
- **Test Case:**
  1. User with 10000 watch history items exports CSV
  2. Network drops after writing 5000 rows
  3. Client receives 5000 rows of valid CSV, then connection closed
  4. File appears valid to CSV parser, user loses 5000 rows on re-import
- **Note:** This is partially mitigated by buffering the entire CSV in memory first (line 761), so it's not critical. But the error trailer (line 801) is unreliable.
- **Fix Suggested:** The current buffering approach is correct and sufficient. Just ensure the trailer write doesn't silently fail:
```go
if _, err := c.Writer.Write(buf.Bytes()); err != nil {
  h.log.Error("CSV send failed for user %s: %v", user.Username, err)
  // Don't try to append trailer — connection is already broken
  // The file was already partially sent
}
```

---

## Summary Table

| # | Component | Bug | Severity | Status |
|---|-----------|-----|----------|--------|
| 1 | Go Handlers | Missing `resolveMediaPathOrReceiver()` function | CRITICAL | Unfixed |
| 2 | Go Upload | Storage quota bypass via client size | CRITICAL | Unfixed |
| 3 | Go Auth | Race: Admin user creation in preferences | HIGH | Unfixed |
| 4 | Vue Home | Pagination state leak on selection toggle | HIGH | Unfixed |
| 5 | Go Upload | Quota race — file registered before quota deducted | HIGH | Unfixed |
| 6 | Vue Profile | Silent clipboard copy failure | MEDIUM | Unfixed |
| 7 | Go Auth | CSV export error handling unreliable | MEDIUM | Mitigated |
| — | Go Auth | deletionRequests nil panic | CRITICAL | ✅ Fixed |
| — | Go Suggestions | IP-based guest profiles DB persist | HIGH | ✅ Fixed |

---

## Recommended Fix Order

1. **BUG #1** — ASAP (prevents any playback position from working)
2. **BUG #2** — ASAP (security vulnerability)
3. **BUG #3** — Before next release (data loss for admin users)
4. **BUG #5** — Before next release (resource exhaustion)
5. **BUG #4** — Soon (UX bug, low impact)
6. **BUG #6** — Soon (UX bug, data loss risk)
7. **BUG #7** — Optional (buffering mitigates most risk)

---

## Testing Checklist

- [ ] BUG #1: Call playback endpoints with valid media ID
- [ ] BUG #2: Attempt upload with quota at limit
- [ ] BUG #3: Rapid concurrent preference updates from admin account
- [ ] BUG #4: Toggle selection mode at different pages
- [ ] BUG #5: Upload file while deleting concurrent file
- [ ] BUG #6: Copy token in private/incognito mode
- [ ] BUG #7: Export watch history, verify trailer comment on corruption

