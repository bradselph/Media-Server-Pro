# Architecture — constrained evolutionary synthesis

**Canonical reconciliation** of the two original plans (pipeline + evolutionary synthesis) lives in **`docs/CONSOLIDATED_MODEL.md`**. This file focuses on how the model **maps to this repository** and what is implemented vs. left to adopters.

## 1. Authority model

| Layer    | Owns                                      | Sees (by default)        |
|----------|--------------------------------------------|---------------------------|
| Backend  | Execution semantics, state, side effects | Your backend root (see `synthesis/partitions.json`; default `server/`) |
| Frontend | Presentation, call orchestration          | Your frontend root + generated SDK (default `client/`) |
| Contract | Interaction truth                         | Your spec directory (default `api_spec/`) |

**Shared mutable surface between backend and frontend:** the versioned API spec only.

## 2. Contract layer (hard gate)

- Single canonical spec (OpenAPI 3.x today; Protobuf optional later). Default in this repo: `api_spec/openapi.yaml`. Path is configured in `synthesis/partitions.json` / `project_settings.json` (`reference_example.openapi_spec` for CI).
- Versioned per [semver](https://semver.org/): PATCH (non-breaking clarification/fix), MINOR (additive), MAJOR (breaking).
- **Compiled enforcement** (recommended): generate validators, mock server, and client SDK from the spec. Application code uses the SDK, not hand-written URLs.

**Policy (document in `api_spec/README.md` or your contract README):** whether unknown JSON fields are **rejected** or **ignored**. The same rule must apply in proxy, client, and tests.

## 3. Three autonomous loops

1. **Backend hardening** — Inputs: contract + backend partition. Outputs: implementation that passes contract checks, tests, and agreed performance bounds.
2. **Frontend alignment** — Inputs: contract + generated SDK only (no dependency on backend source for behavior). Outputs: UI that produces zero invalid API calls.
3. **Integration verification** — Inputs: running backend + frontend + contract. Real HTTP, scenario tests, bounded fuzzing. Produces a **failure source**: frontend, backend, or contract.

## 4. Anti-corruption

Place validation at the boundary (reverse proxy, API gateway, or first middleware) using the **same** schema rules as the generated client. Invalid requests: **reject**, **log**, **route to frontend fix loop**—do not widen backend acceptance to “paper over” bad clients.

## 5. Fitness (merge gates)

Hard stops before merge:

- Backend: contract compliance + tests green.
- Frontend: strict adherence to SDK and schemas; no calls to undefined operations.
- Integration: scenario suite green; no undefined behavior relative to the contract.

Soft metrics (selection among candidates): latency, determinism, complexity bounds, structural diversity from the last accepted version.

## 6. Drift prevention

- Contract diffs reviewed for breaking changes; MAJOR requires explicit migration.
- Regression replay on each iteration where multiple candidates are synthesized.
- Optional: cap entropy (max lines/files touched per iteration) to avoid runaway rewrites.

## 7. Memory (anti-regression)

Store hashes or AST fingerprints of **failed** and **accepted** patterns where tooling allows; bias selection away from known failures and toward stable structures.

## 8. Repository layout (this checkout)

```
.cursor/rules/     # Cursor agent rules
api_spec/          # Contract (default)
server/            # Backend (default)
client/            # Frontend (default)
docs/              # Architecture, workflows, adoption
examples/          # Pointers for adopters (see examples/minimal-api/README.md)
generated/         # Optional top-level codegen placeholder
scripts/           # Partition check, OpenAPI validate, completeness, smoke, wizard
synthesis/         # partitions.json + project_settings.json
```

Consumer projects map their own contract, backend, and frontend paths in `synthesis/partitions.json` (see `docs/ADOPTION.md`).

## 8a. Embeddable server package (`pkg/mediaserver/`)

The `pkg/mediaserver/` package exposes the entire server as an importable Go library. Instead of running the standalone binary, consumers can construct a fully-wired server with functional options and embed it in their own applications.

### Quick start

```go
import "media-server-pro/pkg/mediaserver"

srv, err := mediaserver.New(
    mediaserver.WithConfigPath("config.json"),
    mediaserver.WithVersion("1.0.0"),
)
if err != nil {
    log.Fatal(err)
}
if err := srv.ListenAndServe(); err != nil {
    log.Fatal(err)
}
```

### Module selection

By default all modules are registered. Use `WithModuleSet` or `WithModules` to cherry-pick:

```go
// Only core media serving (no HLS, analytics, playlists, etc.)
srv, _ := mediaserver.New(
    mediaserver.WithModuleSet(mediaserver.CoreModules),
)

// Custom selection
srv, _ := mediaserver.New(
    mediaserver.WithModules(
        mediaserver.ModMedia,
        mediaserver.ModStreaming,
        mediaserver.ModHLS,
        mediaserver.ModAuth,
    ),
)
```

### Predefined module sets

| Set | Modules |
|-----|---------|
| `CoreModules` | database, security, auth, media, streaming, tasks, scanner, thumbnails |
| `StandardModules` | Core + HLS, analytics, playlist, admin, upload, suggestions, categorizer |
| `AllModules` | Every available module (default) |

### Programmatic access

After construction, all module references are exported on the `Server` struct:

```go
srv, _ := mediaserver.New()
items := srv.Media.ListMedia(media.Filter{})
srv.Tasks.RegisterTask(tasks.TaskRegistration{...})
```

### Relationship to `cmd/server/`

The standalone binary in `cmd/server/main.go` is a thin wrapper that parses CLI flags and calls `mediaserver.New()`. All module wiring, task registration, and route setup lives in `pkg/mediaserver/`.

### Package layout

```
pkg/mediaserver/
├── doc.go        # Package documentation and examples
├── server.go     # Server type, New() constructor, module construction, route wiring
├── options.go    # Option type and With* functional option functions
├── modules.go    # ModuleID constants, ModuleSet type, predefined sets
└── tasks.go      # Background task registration (extracted from old main.go)
```

## 9. Implemented in this framework vs. adopter extensions

| Area | In-repo today | Typical next steps for a product repo |
|------|----------------|--------------------------------------|
| Authority / path ACL | `synthesis/partitions.json`, `scripts/check_partition_boundaries.py` | Point paths at your services; tune `framework_paths` |
| Executable contract | OpenAPI validate, TS codegen in default `client/` | Add request/response jsonschema validation, Prism/mock |
| Loop 1 / 2 / 3 docs | `docs/AGENT_PLAYBOOK.md`, `docs/PROMPTS.md` | Wire CI scenario tests, bounded fuzz |
| Anti-corruption | Example: invalid JSON rejected in FastAPI middleware | Full schema validator proxy at gateway |
| Fitness — completeness | `scripts/check_completeness.py` | Add lint, complexity caps in CI |
| Fitness — static | Client `tsc`, server pytest | Add mypy/ruff/eslint per stack |
| Mutation channel | `docs/contract-extensions/TEMPLATE.md` | Review process + semver automation |
| Multi-run / AST clustering / memory | Documented in `docs/CONSOLIDATED_MODEL.md` | Optional external orchestrator or manual N-run policy |
