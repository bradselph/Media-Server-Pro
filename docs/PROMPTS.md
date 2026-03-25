# Prompt templates for agentic sessions

Copy the block that matches the loop. Replace `{BACKEND_PATH}`, `{FRONTEND_PATH}`, `{CONTRACT_PATH}` with values from `synthesis/partitions.json`.

---

## System / preamble (any loop)

```text
You are working inside a contract-first monorepo with strict partitions.
- Backend partition: {BACKEND_PATH}
- Frontend partition: {FRONTEND_PATH}
- Contract: {CONTRACT_PATH}
Rules:
- One git commit must not mix backend and frontend partition roots (unless repo policy exempts a demo path).
- New API endpoints or fields require a proposal under docs/contract-extensions/ before implementation.
- Do not weaken the API server to accept invalid JSON/schema-invalid requests to fix client bugs.
- Prefer generated SDK/types from the contract; no ad-hoc HTTP in application code.
```

---

## Loop 1 — Backend agent

```text
Task: BACKEND LOOP ONLY.

You may read and edit files under {BACKEND_PATH} and read {CONTRACT_PATH}.
Do not read or edit {FRONTEND_PATH}.

Implement or fix the server to match the OpenAPI/Proto contract exactly.
Reject invalid requests at the boundary; do not add undocumented routes.
Add or update tests proving behavior for the changed operations.

Do not invent new paths or schemas; if the contract is insufficient, write a short contract extension outline in docs/contract-extensions/ and stop without changing the contract yourself unless this session is explicitly the contract role.
```

---

## Loop 2 — Frontend agent (blind)

```text
Task: FRONTEND LOOP ONLY (BLIND TO BACKEND IMPLEMENTATION).

You may read and edit files under {FRONTEND_PATH}, read {CONTRACT_PATH}, and use generated types under the frontend’s generated/ output.
Do not open or rely on {BACKEND_PATH} for behavior — treat the contract and generated types as the only source of truth.

Use codegen output for request/response typing. If an operation is missing from the spec, do not guess URLs; draft a contract extension request under docs/contract-extensions/ and stop.

After changes, ensure typecheck passes (npm run check or equivalent).
```

---

## Loop 3 — Integration triage

```text
Task: INTEGRATION ANALYSIS.

You may read the whole repository, run tests, and inspect logs.
Run real HTTP requests against the running backend (or describe exact curl commands).

For each failure, classify:
- Type A: client sent invalid request or wrong usage → assign to frontend loop
- Type B: server response or behavior does not match contract → assign to backend loop
- Type C: spec lacks needed capability → assign to contract extension workflow

Output: failure_source (frontend|backend|contract), evidence (request/response or test name), and the next single loop to run.
```

---

## Mutation — Contract extension agent

```text
Task: CONTRACT EXTENSION (MUTATION CHANNEL ONLY).

You may edit files under {CONTRACT_PATH} and create/update docs under docs/contract-extensions/.
Do not edit {BACKEND_PATH} or {FRONTEND_PATH} in this same change set.

Produce an additive (MINOR) or explicitly breaking (MAJOR) change with:
- Justification and usage pattern
- Backward compatibility argument or migration notes
- Updated examples (including negative cases where helpful)
Bump info.version / package version per semver rules.

Reminder: implementation commits must follow in order: backend, then frontend, in separate commits.
```

---

## Multi-run backend synthesis (optional)

```text
Generate {N} alternative implementations for the same contract-constrained task.
Constraints:
- No TODO/FIXME/NotImplementedError in final candidates.
- Each variant must use a meaningfully different structure (not just renames).
After listing variants, recommend one that passes tests and is simplest; explain tradeoffs.
```
