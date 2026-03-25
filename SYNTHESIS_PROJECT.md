# Synthesis project context (generated)

**Do not hand-edit** except in an emergency; run `python3 scripts/configure_synthesis.py` to update.

## For AI agents (Cursor / Claude Code)

Before substantive work:

1. Read this file and `docs/CONSOLIDATED_MODEL.md` §3 (Type A/B/C routing).
2. Answer the **pre-flight questions** below (or confirm assumptions with the user if answers are missing).
3. Confirm which **loop** you are in: Backend (1), Frontend (2), Integration (3), or Mutation (contract extension).
4. Obey **one commit per partition**; never relax server validation to accept invalid clients.
5. **Respect prior agent artifacts** listed below: they may contain team conventions. If they **conflict** with `docs/CONSOLIDATED_MODEL.md` (partitions, contract-first, no invalid-input hacks), **this workflow wins**—note the conflict to the user.

### Pre-flight questions (ask user if unknown)

- Which loop are we running (backend / frontend / integration / contract extension)?
- What is the acceptance criterion for this change?
- Are we allowed to change the OpenAPI spec in this session?

---

## Prior Cursor / Claude / agent context (discovered)

**Last scan:** 2026-03-25 00:52 UTC

**Framework path allowlist** (partition checker treats these like framework paths): `.claude/`, `.roo/`

### Known files (read for alignment, do not override synthesis rules)

- `.claude/skills/deep-debug-audit/SKILL.md`
- `.claude/skills/deep-debug-audit/deep-debug-audit.md`
- `.claude/skills/nuxt-ui/SKILL.md`
- `.claude/skills/nuxt-ui/references/components.md`
- `.claude/skills/nuxt-ui/references/composables.md`
- `.claude/skills/nuxt-ui/references/layouts/chat.md`
- `.claude/skills/nuxt-ui/references/layouts/dashboard.md`
- `.claude/skills/nuxt-ui/references/layouts/docs.md`
- `.claude/skills/nuxt-ui/references/layouts/editor.md`
- `.claude/skills/nuxt-ui/references/layouts/page.md`
- `.claude/skills/nuxt-ui/references/theming.md`
- `.cursor/rules/agent-playbook.mdc`
- `.cursor/rules/audit-loop.mdc`
- `.cursor/rules/codacy.mdc`
- `.cursor/rules/duplicate-code-consolidation.mdc`
- `.cursor/rules/mutation-contract-extension.mdc`
- `.cursor/rules/partition-backend.mdc`
- `.cursor/rules/partition-contract.mdc`
- `.cursor/rules/partition-frontend.mdc`
- `.cursor/rules/synthesis-authority.mdc`
- `.cursor/rules/verbose-audit-loop.mdc`
- `CLAUDE.md`
- `docs/CONFIG-NOTES.md`

### Imported summary (from prior setup)

_(none)_

---

## Project

| Field | Value |
|-------|--------|
| **Name** | media-server-pro |
| **Partition base (git)** | `origin/main` |

### Goals

Stream media (video, audio, images) to authenticated users with admin management, HLS transcoding, content moderation, remote source federation, and a Nuxt UI frontend.

### Constraints / non-goals

Backend is Go 1.26 + Gin; frontend is Nuxt 3 + @nuxt/ui v3; old Vite frontend in web/frontend/ is legacy (read-only reference). No OpenAPI spec exists yet — contract is implicit in Go route definitions.

### Tech / stack notes

Go module: `media-server-pro`. Backend entry: `cmd/server/main.go`. Routes: `api/routes/routes.go`. Models: `pkg/models/models.go`. Handlers: `api/handlers/`. Business logic: `internal/`. Frontend (primary): `web/nuxt-ui/` (Nuxt 3 + @nuxt/ui v3). Frontend (legacy): `web/frontend/` (Vite + TS, comprehensive but being replaced). Embedded SPA served by `web/server.go`.

---

## Repository paths (authoritative)

| Role | Path |
|------|------|
| Contract directory | `api_spec` |
| OpenAPI file | `api_spec/openapi.yaml` (to be created) |
| Backend | `.` (Go — `api/`, `cmd/`, `internal/`, `pkg/`) |
| Frontend (primary) | `web/nuxt-ui` |
| Frontend (legacy) | `web/frontend` |

### Integration smoke

- **Go entry**: `cmd/server/main.go`
- **Port**: `8765`

### CI commands (local or workflow)

| Step | Command |
|------|---------|
| Backend install | `go mod download` |
| Backend tests | `go test ./...` | skip: `False` |
| Client install | `npm ci` | skip job: `False` |
| Client check | `npm run check` |
| Integration smoke | _(script)_ | skip: `False` |

---

## Steering log (recent)

- **2026-03-24**: Initial alignment audit. Found SYNTHESIS_PROJECT.md misconfigured (assumed Python/ASGI, actual is Go/Gin). No OpenAPI spec exists. Two frontends: `web/nuxt-ui/` (primary, ~55% route coverage) and `web/frontend/` (legacy, ~100% route coverage). Major Nuxt UI type mismatches identified in ServerSettings, StorageUsage, HLSStats, ThumbnailStats, WatchHistoryItem, Suggestion, SessionCheckResponse. ~56 backend endpoints missing from Nuxt UI.

---

_Regenerated: 2026-03-25 01:30 UTC_
