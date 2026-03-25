# Scenario coverage — consistent progressive development

Use this with **`docs/CONSOLIDATED_MODEL.md`** (routing) and **`SYNTHESIS_PROJECT.md`** (your goals and paths). The interactive wizard is **`python3 scripts/configure_synthesis.py`**.

---

## Setup & steering

| Scenario | What to do |
|----------|------------|
| First clone / first use | Run `python3 scripts/configure_synthesis.py`. Answer paths, goals, pre-flight questions for models. |
| Repo already had Cursor / Claude | Enable **scan** in the wizard; review `prior_agents.known_files` in `SYNTHESIS_PROJECT.md`. Optionally paste a summary into `import_summary`. See **`docs/MIGRATION_FROM_PRIOR_AGENTS.md`**. |
| List agent files without prompts | `python3 scripts/configure_synthesis.py --discover-only` |
| Paths or stack changed | Run the wizard again; it keeps defaults you press Enter on. |
| Quick regen of model context only | `python3 scripts/configure_synthesis.py --sync-only` |
| User wants to steer mid-flight | Run wizard and choose **yes** for “steering note”, or append to `project_settings.json` → `steering.sessions` and run `--sync-only`. |
| Backend-only repo | Wizard: no frontend partition → client CI skipped; point frontend dir at backend only for manifest completeness; completeness skips client `src/`. |

---

## Type A / B / C (failure routing)

| Scenario | Classification | Next action |
|----------|----------------|-------------|
| Client sends invalid JSON / wrong schema / unknown op | **Type A** | Loop 2 only; fix client or codegen usage. |
| Server returns wrong shape / wrong status / missing op impl vs spec | **Type B** | Loop 1 only; fix server + tests. |
| Spec lacks needed operation or field | **Type C** | Extension proposal → contract commit → backend → frontend (separate commits). |
| Test fails in CI but passes locally | Integration / env | Reproduce with same commands as CI; check `SYNTHESIS_*` env from `resolve_ci_paths.py`. |
| Flaky integration | Timing / state | Add sequence test; avoid sleep-only fixes; cap retries with logging. |

---

## Partition & git

| Scenario | Expected behavior |
|----------|-------------------|
| One commit touches backend + frontend roots | **Fail** partition check (unless path is under `reference_example_prefixes`). |
| One commit touches contract + both implementations | **Fail** partition check (same exemption). |
| PR with backend commit then frontend commit | **Pass** (per-commit check). |
| File outside partitions + shared + framework | **Fail** unknown-path check (or warn if `SYNTHESIS_STRICT=0`). |
| Old layout paths in diff (`api_spec/` at root after move) | Listed in `legacy_layout_prefixes` → ignored for unknown check. |

---

## Contract & semver

| Scenario | Rule |
|----------|------|
| Doc-only clarification in OpenAPI | Usually **PATCH**. |
| New optional field or new endpoint, backward compatible | **MINOR**. |
| Remove/rename/required field change | **MAJOR** + migration; do not sneak as MINOR. |
| Unknown JSON fields | Must match chosen policy everywhere (reject vs ignore). |

---

## CI toggles (via wizard → `project_settings.json`)

| Scenario | Setting |
|----------|---------|
| No Node frontend | `has_frontend: false`, `skip_client_job: true` |
| No ASGI smoke yet | `skip_integration_smoke: true` |
| Backend has no pytest yet | `skip_backend_tests: true` |
| Default branch is not `main` | Set `git.partition_check_base` (e.g. `origin/develop`) |

---

## Agent sessions (Cursor / Claude)

| Scenario | Mitigation |
|----------|------------|
| Model starts coding without context | @-mention **`SYNTHESIS_PROJECT.md`** + `docs/CONSOLIDATED_MODEL.md` §3. |
| Model reads backend while doing frontend | Enforce **Loop 2 blind** prompt from `docs/PROMPTS.md`. |
| Model adds hidden API | Forbidden; require `docs/contract-extensions/` first. |
| Model weakens validation | Reject; Type A fix is client-side. |

---

## Progressive enhancement (tooling gaps)

| Scenario | Suggested next step |
|----------|---------------------|
| Only structural OpenAPI validation | Add runtime request/response jsonschema in tests or proxy. |
| No contract tests | Generate from spec or add schemathesis with bounds. |
| No fuzz | Add domain-limited fuzz (types/ranges only). |
| No memory of failed patterns | Log failures in steering notes or external tracker; paste hashes into session. |

---

## Emergency

| Scenario | Action |
|----------|--------|
| Must land hotfix across partitions | Split commits; never disable partition check permanently. |
| Contract wrong after merge | Revert or MAJOR; **rollback_contract** path in consolidated model. |
