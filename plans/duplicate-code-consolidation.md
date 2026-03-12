# Duplicate Code Consolidation — Instructions for Future Runs

**How to run:** Ask the AI to *"run the duplicate code analysis and corrections"*, *"do the duplicate code sweep again"*, or *"run duplicate code consolidation"*. The Cursor rule in `.cursor/rules/duplicate-code-consolidation.mdc` points the AI to this file.

When the user asks for the above, follow these steps.

---

## 1. Run the analysis

1. **Explore the repo** for duplicate or near-duplicate patterns in:
   - **api/handlers/** — limit/offset/page query parsing, get-ID→validate→resolve→404, JSON bind + 400, getSession→401
   - **internal/** — Start/Stop/Health + healthMu/setHealth across modules
   - **internal/repositories/mysql/** — Get/List/Delete by ID, rowToRecord + List loop
   - **internal/config/** — env override patterns (prefer envGetStr/envGetInt/etc. over raw os.Getenv)
   - **web/frontend/src/** — searchParams.get + defaults, useQuery + refetch, form preventDefault + async submit, export fetch

2. **For each finding**, note: (a) file(s) and line/symbol, (b) what is duplicated, (c) suggested consolidation (shared helper, single implementation).

3. **Do not implement** in the analysis phase — only list findings with locations and consolidation ideas.

---

## 2. Apply corrections (consolidation patterns)

Use these patterns when fixing. Prefer **incremental** changes: add shared helpers first, then migrate call sites file-by-file.

### Handlers (api/handlers/)

| Pattern | Consolidation |
|--------|----------------|
| Limit/offset/page from query | Use `ParseQueryInt(c, key, QueryIntOpts{Default, Min, Max})` and/or `ParseLimitOffset(c, LimitOffsetOpts{...})` from handler.go. Remove local helpers in favor of shared ones. |
| Get ID from param → validate → resolve → 404 | Use `RequireParamID(c, paramName)` for "id required" (writes 400 if empty). Use `resolveMediaByID` / `resolveMediaPathOrReceiver` as today; optionally add `RequireResolvedMediaID(c, id)` that wraps resolve + 404. |
| Bind JSON + 400 on error | Use `BindJSON(c, dest, errMessage) bool` from handler.go. When it returns false, handler should return. Use `errInvalidRequest` for generic "Invalid request" or a custom message when needed. |
| getSession then 401 if nil | Use `RequireSession(c) *models.Session`; when it returns nil the helper has already written 401. Or rely on `requireAuth()` middleware and remove redundant checks. |

### Config (internal/config/)

- All env overrides should use **env_helpers.go**: `envGetStr`, `envGetBool`, `envGetInt`, `envGetDuration`, etc. Replace any raw `os.Getenv` in override files with the appropriate helper.

### Modules (internal/*)

- **Health state**: Prefer a shared helper (e.g. in `internal/server/` or a small `modulebase` package) with `HealthState` (mutex, healthy, message), `SetHealth(healthy, msg)`, and `Health() models.HealthStatus`. Migrate modules to use it so Start/Stop/Health boilerplate is reduced.

### Repositories (internal/repositories/mysql/)

- Add **helpers** (e.g. in `helpers.go`) for common GORM patterns: `GetByID`, `DeleteByID`, `ListOrdered` (or equivalent). Keep per-repo types and `rowToRecord`; use helpers only for the repeated DB calls. Optionally add a small generic “map rows to records” loop helper.

### Frontend (web/frontend/src/)

- **Query params**: Add `useSearchParams` or `lib/queryParams` with `getSearchParam`, `getSearchParamNumber`, and optionally “build next URL” for list pages.
- **Forms**: Add `useFormSubmit(onSubmit)` returning `{ handleSubmit, isSubmitting, error }` and use it in forms.
- **Downloads**: Add `api.getBlob(url)` and optionally `api.downloadFile(url, filename)` in the API client; use them for export/download endpoints instead of raw fetch.

---

## 3. After applying fixes

- Run **build and tests**: `go build ./...`, `go vet ./...`, and any project test commands.
- Prefer **one logical change per commit** (e.g. “Add BindJSON and migrate auth handlers” vs “Add all helpers and migrate everything”).

---

## 4. Reference: where shared helpers live (after first consolidation)

- **api/handlers/handler.go**: `BindJSON`, `ParseQueryInt`, `ParseLimitOffset`, `RequireParamID`, `RequireSession`, `writeError`, `writeSuccess`, `getSession`, `getUser`, `resolveMediaByID`, `resolveMediaPathOrReceiver`.
- **internal/config/env_helpers.go**: `envGetStr`, `envGetBool`, `envGetInt`, `envGetInt64`, `envGetFloat64`, `envGetDuration`, `envGetDurationString`.
- **internal/config/env_overrides_*.go**: All use env helpers; no raw `os.Getenv` in override logic.

Use this file as the single source of instructions for “run the same analysis and corrections” requests.
