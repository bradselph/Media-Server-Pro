# Consolidated model — reconciliation

This document merges two descriptions into one operational model:

1. **Contract-first autonomous pipeline** — bidirectional enforcement, mutation isolation, three loops, merge policy.
2. **Constrained evolutionary synthesis** — blind partitions, path ACL, mutation agent, multi-run synthesis, structural convergence, fitness gates, drift suppression, memory.

**Formal name:** **Constrained evolutionary synthesis with blind partitions and contract arbitration.**

The **contract-first pipeline** is the *operational topology* (how work is routed and merged). **Constrained evolutionary synthesis** is the *generation and selection mechanics* inside the backend and frontend loops (N candidates, filters, fitness, convergence).

---

## 1. Authority (single model)

| Layer | Owns | Shared with others |
|--------|------|---------------------|
| **Backend** | Execution semantics, state, side effects | Nothing with frontend |
| **Contract** | Interaction truth (OpenAPI / Protobuf) | **Only** cross-cutting mutable API surface |
| **Frontend** | Presentation, orchestration of calls | Nothing with backend |

**Rules**

- Frontend agents do not modify backend. Backend agents do not modify frontend.
- **Mutation isolation:** only the **mutation / contract extension** channel may introduce new endpoints, fields, or interaction semantics.
- **Path ACL** implements boundaries (`synthesis/partitions.json` + `scripts/check_partition_boundaries.py`): cross-partition writes in one **commit** are rejected unless exempt (e.g. bundled `reference_example_prefixes`).

---

## 2. Contract layer (hard gate, executable)

- Single canonical spec per API surface; **versioned** (PATCH / MINOR / MAJOR); **immutable for a given release cycle** once shipped.
- Contract is **not** prose-only: **machine-verifiable** schema; **generated** validators, SDK, mocks where tooling allows.
- **Bidirectional enforcement:** backend **implements** the spec; frontend **consumes** via generated types/SDK; **mismatch = failure** (CI or merge gate).
- Embed **positive and negative** examples in or alongside the spec when feasible.

**Strictness:** pick one policy for unknown fields (reject vs ignore) and apply it consistently at proxy, client, and tests (`api_spec/README.md` or your contract README).

---

## 3. Change routing (Type A / B / C)

| Type | Symptom | Action |
|------|---------|--------|
| **A** | Frontend fails contract validation (invalid requests, wrong shapes, wrong endpoints) | **Loop 2** — fix frontend only |
| **B** | Backend fails contract validation or contract tests | **Loop 1** — fix backend only |
| **C** | Both conform to spec but capability is missing | **Extension** — proposal → MINOR/MAJOR → backend → frontend (ordered commits) |

Pseudocode:

```text
if frontend fails contract validation:
    fix frontend
elif backend fails contract validation:
    fix backend
elif both valid but feature blocked:
    generate contract extension request (mutation channel)
```

**Never** “fix” the backend by accepting invalid input to unblock the frontend.

---

## 4. Contract extension (sole creativity channel for API surface)

1. Author proposal under `docs/contract-extensions/` (from `TEMPLATE.md`).
2. Validate backward compatibility (or explicit MAJOR + migration).
3. Bump contract version; regenerate SDK / validators.
4. **Backend** implements (separate commit/PR).
5. **Frontend** consumes after backend passes (separate commit/PR).
6. **Integration** verifies real HTTP and scenarios.

No direct backend mutation from the “frontend fix” path to add undocumented behavior.

---

## 5. Three autonomous loops

| Loop | Input | Process | Output |
|------|--------|---------|--------|
| **1 — Backend hardening** | Contract + backend partition | Generate **N** variants (optional); filter; **contract compliance**, tests, performance bounds | Best / accepted backend candidate |
| **2 — Frontend alignment** | Contract + **generated SDK only** (blind to backend impl) | Generate **N** variants; strict schema adherence; no undefined endpoints | Best / accepted frontend candidate |
| **3 — Integration** | Running backend + frontend + contract | **Real** HTTP; bounded fuzz; sequences | `failure_source`: frontend \| backend \| contract |

**Blind partitioning:** Loop 2 must not use backend source as behavioral truth—only contract + generated interfaces.

---

## 6. Anti-corruption

- **Contract validator at the boundary** (proxy, gateway, or first middleware): same rules as generated client.
- Invalid request → **reject**, **log**, route to **Type A** (frontend loop).
- Do not widen backend validation to hide client bugs.

---

## 7. Fitness (strict merge bar + soft selection)

**Hard gates (merge)**

- Backend: contract pass + tests (your bar may be 100% of defined contract tests).
- Frontend: **zero** invalid API calls in integration; adherence to SDK.
- Integration: scenario suite; no undefined behavior vs contract.

**Soft signals (pick among valid candidates)**

- Backend: latency, determinism, contract_pass_rate, test_pass_rate.
- Frontend: successful sequences, clarity of orchestration.
- Integration: end-to-end success rate.

---

## 8. Merge policy

Merge only when hard gates pass. Otherwise **isolate** the failing loop and rerun only that loop (and contract rollback if extension was wrong).

---

## 9. Drift prevention

- Contract diffing in review/CI; **breaking → reject** unless MAJOR + migration.
- **PATCH:** non-breaking fixes/clarifications. **MINOR:** additive. **MAJOR:** breaking, coordinated cycle.
- Optional: snapshot locking, regression replay each iteration, **entropy cap** (reject oversized diffs per iteration).

---

## 10. Evolutionary mechanics (inside Loops 1 & 2)

- **Multi-run:** N generations with varied temperature / prompt constraints / pattern sampling.
- **Diversity:** reject near-duplicates (e.g. AST similarity threshold).
- **Structural convergence:** normalize → cluster by structure → prefer tight clusters that are also diverse from last accepted (not naive majority vote).
- **Fitness pipeline:** completeness scan → static integrity → contract compliance → tests (unit, generated contract tests, bounded fuzz) → behavioral consistency vs last good (unless intentional change).
- **Edge case discipline:** only schema-valid, realistic-bounded cases; filter combinatorial explosion.
- **Memory (optional tooling):** penalize similarity to failed patterns; bias toward stable accepted structures.

---

## 11. Execution skeletons (equivalent control flow)

**Pipeline-style**

```python
while True:
    contract = load_contract()
    backend = run_backend_loop(contract)
    frontend = run_frontend_loop(contract)
    result = integration_test(backend, frontend, contract)
    if result.contract_violation_frontend:
        frontend = fix_frontend(frontend, contract)
        continue
    if result.contract_violation_backend:
        backend = fix_backend(backend, contract)
        continue
    if result.feature_blocked:
        contract = extend_contract(contract)
        continue
    if result.success:
        commit_state(backend, frontend, contract)
```

**Synthesis-style**

```python
while True:
    mutation = maybe_introduce_change()
    contract = enforce_contract(mutation)
    backend_valid = filter_valid(generate_backend(contract))
    frontend_valid = filter_valid(generate_frontend(contract))
    best_backend = select_fittest(backend_valid)
    best_frontend = select_fittest(frontend_valid)
    result = integration_test(best_backend, best_frontend, contract)
    if result.failure_source == "frontend":
        reinforce(frontend_loop)
        continue
    if result.failure_source == "backend":
        reinforce(backend_loop)
        continue
    if result.failure_source == "contract":
        rollback_contract()
        continue
    commit(best_backend, best_frontend, contract)
```

Map `failure_source == "contract"` to extension mistakes or incompatible spec changes.

---

## 12. Resulting properties

- Frontend cannot distort backend; backend cannot silently drift off contract.
- Evolution of the API goes through **controlled contract expansion** (mutation channel).
- Stability comes from **enforced mutation boundaries**, **externalized validation**, and **compliance-first** merge policy—not from informal consensus.

---

## 13. Mapping to this repository

| Concept | Location |
|---------|----------|
| Path ACL / per-commit boundaries | `synthesis/partitions.json`, `scripts/check_partition_boundaries.py` |
| Contract source (default checkout) | `api_spec/` (repoint via `reference_example` / wizard) |
| Executable validation (partial) | `scripts/validate_openapi.py`, codegen + typecheck, pytest, smoke |
| Completeness gate (partial) | `scripts/check_completeness.py` |
| Mutation proposals | `docs/contract-extensions/` |
| Cursor rules | `.cursor/rules/` |
| Claude entry | `CLAUDE.md` |
| Operator guide | `docs/AGENT_PLAYBOOK.md`, `docs/WORKFLOWS.md` |
| Interactive setup & model context | `scripts/configure_synthesis.py`, `synthesis/project_settings.json`, `SYNTHESIS_PROJECT.md` |
| Scenario matrix | `docs/SCENARIOS.md` |
| CI path resolution | `scripts/resolve_ci_paths.py`, `.github/workflows/synthesis-ci.yml` |
| Prior Cursor/Claude alignment | `scripts/configure_synthesis.py` (scan), `prior_agents` in `project_settings.json`, `docs/MIGRATION_FROM_PRIOR_AGENTS.md` |
| Install into existing repo | `scripts/install_framework.py`, `docs/INSTALL_IN_YOUR_REPO.md` |

Gaps for adopters to fill: full **bidirectional** JSON schema validation at runtime, generated contract tests, fuzz/sequence suites, AST clustering, and memory store—the framework supplies the **structure** and **minimum gates**; you extend tooling to match your stack.
