# Workflows — routing, extensions, merge policy

The **Type A / B / C** model and merge policy are defined in full in **`docs/CONSOLIDATED_MODEL.md`** (reconciled with blind partitions and mutation isolation).

## 1. Failure routing

After contract validation and integration tests:

1. If the **client** sends invalid requests or misuses the SDK → **fix frontend** (Loop 2 only).
2. If the **server** fails contract tests or returns invalid responses → **fix backend** (Loop 1 only).
3. If both conform to the spec but the **product need** is unmet → **contract insufficiency** → start **contract extension** (mutation channel), not ad-hoc API in code.

Pseudocode:

```text
if frontend fails contract validation:
    fix frontend (frontend partition only)
elif backend fails contract validation:
    fix backend (backend partition only)
elif both valid but feature blocked:
    contract extension proposal → MINOR/MAJOR → backend → frontend
```

## 2. Contract extension (mutation channel)

Only this path introduces **new** endpoints, fields, or interaction semantics.

1. Author `docs/contract-extensions/YYYYMMDD-slug.md` from `TEMPLATE.md`.
2. Review: backward compatible → **MINOR**; breaking → **MAJOR** + migration.
3. Update your OpenAPI source (default here: `api_spec/openapi.yaml`) and bump `info.version`.
4. Regenerate SDK/validators (`docs/TOOLING.md`).
5. **Backend** commit/PR: implement new spec.
6. **Frontend** commit/PR: consume new SDK.
7. **Integration**: run scenario suite against real API.

## 3. Merge policy

Merge only when:

- Backend passes contract validation and tests.
- Frontend produces **zero** invalid API calls in integration runs.
- Integration scenario suite passes.

Otherwise: isolate the failing side and rerun that loop; do not “balance” failures by weakening validation.

## 4. CI partition check

Pull requests should run:

```bash
python3 scripts/check_partition_boundaries.py --base origin/main --head HEAD
```

(Use your configured base ref if not `origin/main`.)

The checker inspects **each commit** in the range: a commit must not mix backend and frontend partition roots, or ship contract changes together with edits to both implementation partitions in one commit. A PR may contain multiple commits (backend, then frontend) as long as each commit stays within its boundary. Paths come from `synthesis/partitions.json`.

## 5. Blind partitions for agents

- **Frontend agent** prompt: frontend partition, contract, generated SDK; avoid backend implementation.
- **Backend agent** prompt: backend partition, contract; avoid frontend implementation.
- **Contract agent** prompt: contract tree and extension docs; coordinate implementation order.
