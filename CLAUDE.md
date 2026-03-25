# Claude Code — constrained evolutionary synthesis

This repository is a **Go media server** with a **Nuxt 3 frontend**, using **contract-first development** with **blind partitions** for autonomous agents (Cursor + Claude Code). Product layout is defined in `synthesis/partitions.json`. Backend is Go/Gin at the repo root (`api/`, `cmd/`, `internal/`, `pkg/`). Primary frontend is `web/nuxt-ui/`. Legacy frontend `web/frontend/` is read-only reference.

## Read first

- `SYNTHESIS_PROJECT.md` — **your** paths, goals, and pre-flight questions (run `python3 scripts/configure_synthesis.py` to generate/update).
- `docs/CONSOLIDATED_MODEL.md` — full reconciliation of contract-first pipeline + constrained evolutionary synthesis (canonical theory).
- `docs/AGENT_PLAYBOOK.md` — which loop to run, blind frontend rules, merge checklist.
- `docs/PROMPTS.md` — pasteable system prompts per loop (fill paths from `synthesis/partitions.json`).
- `docs/ARCHITECTURE.md` — how this repo maps the model to files and CI.
- `docs/WORKFLOWS.md` — Type A/B/C routing, extension workflow, merge policy.
- `synthesis/partitions.json` — partition paths and `reference_example` CI targets.
- `docs/ADOPTION.md` — point this framework at your own repo layout.

## Operating rules

1. **Stay in your partition** (paths from `synthesis/partitions.json`; backend: `api/`, `cmd/`, `internal/`, `pkg/`; frontend: `web/nuxt-ui/`; contract: `api_spec/`).
   - New API surface: write a proposal under `docs/contract-extensions/` first.

2. **One commit must not mix forbidden partitions** (unless under `reference_example_prefixes` for bundled demos).
   - Do not edit backend and frontend roots in the same commit. Do not put contract + both implementations in one commit. Order: contract → backend → frontend.

3. **No raw HTTP in application code**
   - Use generated SDK from the OpenAPI/Proto toolchain (see `docs/TOOLING.md`). Application frontend code calls through that SDK.

4. **Invalid requests are rejected**
   - Do not “fix” the backend by accepting schema-invalid input. Fix the caller or the contract.

5. **Before claiming done**
   - Run `python3 scripts/check_partition_boundaries.py` if you changed files (requires a resolvable git base; see script help).
   - Follow merge gates in `docs/WORKFLOWS.md`.

## Setup / steering

```bash
python3 scripts/configure_synthesis.py          # interactive: first time or updates
python3 scripts/configure_synthesis.py --sync-only   # refresh SYNTHESIS_PROJECT.md only
python3 scripts/configure_synthesis.py --discover-only  # list prior agent files; no writes
```

If this repo **already** had Cursor/Claude rules or `.claude/` memory, run the full wizard once with **scan** enabled so `SYNTHESIS_PROJECT.md` lists them. Details: `docs/MIGRATION_FROM_PRIOR_AGENTS.md`.

## Commands

```bash
python3 scripts/resolve_ci_paths.py
python3 scripts/check_partition_boundaries.py --base origin/main --head HEAD
pip install -r scripts/requirements-ci.txt
python3 scripts/validate_openapi.py
python3 scripts/check_completeness.py
go test ./...
python3 scripts/integration_smoke.py
cd web/nuxt-ui && npm ci && npm run check
```

## Blind context

When asked to work as **frontend only**, do not read the backend partition for behavior—use the contract and generated types only. When asked to work as **backend only**, do not read the frontend partition.
