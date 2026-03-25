# Agent playbook — Cursor & Claude Code

Use this when running **autonomous or semi-autonomous** coding sessions so behavior matches the consolidated model (`docs/CONSOLIDATED_MODEL.md`).

---

## Before any session

1. Ensure **`SYNTHESIS_PROJECT.md`** is current (`python3 scripts/configure_synthesis.py` or `--sync-only`). @-mention it (Cursor) or paste the top section (Claude).
2. Open `synthesis/partitions.json` if you need raw paths; they mirror **`project_settings.json`** after the wizard.
3. Decide **which loop** you are running (1, 2, 3, or mutation-only). Do not mix partition writes in one commit.
4. Answer the **pre-flight questions** in `SYNTHESIS_PROJECT.md` with the user (or confirm assumptions).
5. Copy the matching **prompt** from `docs/PROMPTS.md` into the chat (Cursor) or session instructions (Claude Code).

If the user has new direction mid-project, run the wizard again and add a **steering note**, or edit `project_settings.json` → `steering.sessions` and run `--sync-only`.

---

## Loop 1 — Backend hardening

**Goal:** Implementation matches contract; tests green; no acceptance of invalid input.

**Allowed context:** Backend partition + contract spec (+ tests). Avoid reading frontend implementation for “shortcuts.”

**Steps**

1. Read OpenAPI/Proto for affected operations.
2. Implement or fix handlers; add/extend tests (unit + contract-oriented tests).
3. Keep boundary validation strict; log rejections if useful.
4. Run backend test command for your layout (see `README.md` / `docs/ADOPTION.md`).
5. Commit **only** backend partition files (+ shared docs if policy allows).

**Exit:** Contract tests and unit tests pass for this change set.

---

## Loop 2 — Frontend alignment

**Goal:** Zero invalid calls; types from generated SDK; no new endpoints invented.

**Allowed context:** Frontend partition + contract + **generated** API types. **Do not** open backend source to infer undocumented behavior.

**Steps**

1. Run codegen (`docs/TOOLING.md`) so types match current contract.
2. Implement UI/calls through typed helpers; handle documented errors.
3. If an operation is missing from the spec → **stop** and output a contract extension draft (do not call a guessed path).
4. Run typecheck / lint for frontend.
5. Commit **only** frontend partition files.

**Exit:** Typecheck passes; no raw ad-hoc URLs except inside generated code.

---

## Loop 3 — Integration verification

**Goal:** Attribute failures to frontend, backend, or contract using **real** HTTP.

**Allowed context:** Full repo (or test harness), running stack, contract.

**Steps**

1. Start backend (and frontend if needed) per project docs.
2. Run scenario tests and/or smoke (`scripts/integration_smoke.py` is a minimal pattern).
3. Optionally bounded fuzz (schema-respecting payloads only).
4. Classify: Type A → Loop 2; Type B → Loop 1; Type C → extension workflow.

**Exit:** Documented `failure_source` and next loop.

---

## Mutation channel — Contract extension (Type C)

**Goal:** Add capability only through reviewed spec change.

**Steps**

1. Create `docs/contract-extensions/YYYYMMDD-slug.md` from `TEMPLATE.md`.
2. Get human review if required by your org.
3. Commit **contract-only** change (semver bump). Then backend commit. Then frontend commit. Never all three in one commit (unless your manifest exempts a demo tree).

---

## Multi-variant synthesis (optional)

When generating **N** candidates (automated or manual):

1. Vary temperature / system instructions / file-order hints between runs.
2. Discard incomplete outputs (TODO, NotImplementedError, placeholders) — aligns with `scripts/check_completeness.py`.
3. Prefer candidates that pass typecheck, tests, and contract checks.
4. Prefer structural diversity from the last merged version to avoid stagnation.
5. Merge **one** winner; discard or branch the rest.

---

## Checklist before merge

- [ ] `python3 scripts/check_partition_boundaries.py --base origin/main --head HEAD` (or your base branch)
- [ ] OpenAPI valid: `python3 scripts/validate_openapi.py`
- [ ] Completeness: `python3 scripts/check_completeness.py`
- [ ] Backend tests (your command)
- [ ] Frontend codegen + typecheck
- [ ] Integration smoke or full scenario suite
- [ ] No backend change that accepts schema-invalid input to “fix” the client

---

## Cursor vs Claude Code

| Concern | Cursor | Claude Code |
|---------|--------|-------------|
| Scoped rules | `.cursor/rules/*.mdc` (partition globs) | `CLAUDE.md` + paste prompts |
| Context control | @-files, Composer scope | Explicit “read only X” in prompt |
| CI | Same repo scripts | Same |

Both should honor **blind Loop 2**: do not use backend files as behavioral ground truth when aligning the client.
