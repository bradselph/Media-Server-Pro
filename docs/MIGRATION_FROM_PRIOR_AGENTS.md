# Migrating a repo that already used Cursor / Claude Code

This framework can **reuse** prior instructions and memory **without** letting them override contract-first partitions.

## What gets discovered

Running the wizard with **scan enabled** (default) executes `scripts/configure_synthesis.py`, which lists files such as:

- `.cursor/rules/*.mdc`, other files under `.cursor/`
- Root `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.cursorrules`, `.cursorrules.md`
- `docs/CLAUDE.md`
- `.claude/**/*.md` and `.md` under `.roo/`, `.cline/` (markdown only)
- `docs/**/*` whose names suggest memory, context, decisions, or notes

**CI only sees the git tree.** Commit important agent files if you want them in every clone.

## What gets stored

| Field | Purpose |
|-------|---------|
| `prior_agents.known_files` | Paths for models to read (listed in `SYNTHESIS_PROJECT.md`) |
| `prior_agents.import_summary` | Short paste from old `CLAUDE.md` / team norms |
| `prior_agents.framework_path_allowlist` | Extra dirs (e.g. `.claude/`) merged into `partitions.json` so the partition checker does not flag them as “unknown” |
| `prior_agents.discovered_at` | Timestamp of last scan |

## Conflict rule

If prior instructions **contradict** this workflow (e.g. “edit backend and frontend together”, “skip tests”, “add endpoints without OpenAPI”), **synthesis rules win**. Models are told to note the conflict to the user (`SYNTHESIS_PROJECT.md`).

## Commands

```bash
# List what would be picked up (no writes)
python3 scripts/configure_synthesis.py --discover-only

# Full interactive setup + scan
python3 scripts/configure_synthesis.py

# Refresh markdown/partitions after editing project_settings.json
python3 scripts/configure_synthesis.py --sync-only
```

## Optional: submodule or symlink

If memory lives in another repo, symlink it into the tree (e.g. `docs/memory -> ../shared-memory`) and add the real path prefix to `prior_agents.framework_path_allowlist` if edits should be allowed there without partition errors.
