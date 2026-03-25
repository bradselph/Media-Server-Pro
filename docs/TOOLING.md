# Tooling — contract codegen and validation

## OpenAPI (current default)

The canonical spec path is **per repository**. In this checkout the default is **`api_spec/openapi.yaml`** (see `synthesis/partitions.json` → `reference_example`, or run `python3 scripts/configure_synthesis.py` to change it).

### Recommended tools (install per stack)

Pick one stack and document pinned versions in the server/client package manifests when you add code.

| Goal              | Typical tool        |
|-------------------|---------------------|
| Client SDK (TS)   | `openapi-generator-cli`, `openapi-typescript` |
| Client SDK (Go)   | `oapi-codegen`, `openapi-generator` |
| Server stubs      | Same generators or hand-maintained handlers with contract tests |
| Validation        | OpenAPI request/response validators, Prism mock, etc. |

### Suggested layout

- One OpenAPI file (or split spec) under your **contract** partition — source of truth (here: `api_spec/openapi.yaml`).
- **Client codegen output** — default `client/generated/` (gitignored; CI regenerates). Optional repo-level `generated/` for other stacks (see `generated/README.md`).

### TypeScript client (default scaffold)

From `client/`:

```bash
npm ci
npm run codegen   # writes generated/api.ts (gitignored)
npm run typecheck
```

Pinned generator: `openapi-typescript` (see `client/package.json`).

### Optional wrapper script

Add `scripts/codegen.sh` that:

1. Validates the OpenAPI file.
2. Emits SDK under your chosen path.
3. Fails CI on diff if generated output is committed and drifted.

### CI

GitHub Actions runs `python3 scripts/validate_openapi.py`, `python3 scripts/check_completeness.py`, backend tests, integration smoke, and client codegen + typecheck (see `.github/workflows/synthesis-ci.yml`). Paths come from `scripts/resolve_ci_paths.py` + `project_settings.json`.

## Partition boundary check

```bash
python3 scripts/check_partition_boundaries.py --base origin/main --head HEAD
```

(Base ref is configurable in `project_settings.json` → `git.partition_check_base`.)

## gRPC / Protobuf (optional)

If you switch to gRPC:

- Store `.proto` under your contract partition (for example `api_spec/proto/` or `contracts/proto/`).
- Same partition rules apply; generated code goes under `generated/` or language-specific dirs with explicit ownership in `synthesis/partitions.json`.
