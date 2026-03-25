#!/usr/bin/env python3
"""
Interactive setup for constrained synthesis: paths, CI hints, project goals, steering.

Run anytime:
  python3 scripts/configure_synthesis.py

Updates:
  - synthesis/project_settings.json
  - synthesis/partitions.json (synced partition + reference_example paths)
  - SYNTHESIS_PROJECT.md (for @-mention in Cursor / Claude)
"""

from __future__ import annotations

import json
import re
import shutil
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
SETTINGS_PATH = REPO_ROOT / "synthesis" / "project_settings.json"
SETTINGS_TEMPLATE = REPO_ROOT / "synthesis" / "project_settings.template.json"
PARTITIONS_PATH = REPO_ROOT / "synthesis" / "partitions.json"
PROJECT_MD = REPO_ROOT / "SYNTHESIS_PROJECT.md"

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from _discover_prior_agents import discover_prior_agent_files, parse_path_list  # noqa: E402


def load_json(path: Path) -> dict:
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_json(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2)
        f.write("\n")


def prompt(msg: str, default: str = "") -> str:
    if default:
        line = input(f"{msg} [{default}]: ").strip()
        return line if line else default
    line = input(f"{msg}: ").strip()
    return line


def prompt_yes(msg: str, default: bool = True) -> bool:
    d = "Y/n" if default else "y/N"
    line = input(f"{msg} ({d}): ").strip().lower()
    if not line:
        return default
    return line in ("y", "yes", "1", "true")


def multiline_until_empty(title: str) -> str:
    print(f"{title} (empty line to finish):")
    lines: list[str] = []
    while True:
        try:
            line = input()
        except EOFError:
            break
        if line.strip() == "" and lines:
            break
        lines.append(line)
    return "\n".join(lines).strip()


def normalize_dir(s: str) -> str:
    s = s.replace("\\", "/").strip().rstrip("/")
    if s.startswith("./"):
        s = s[2:]
    return s


def ensure_under_repo(rel: str) -> str:
    p = (REPO_ROOT / rel).resolve()
    if not str(p).startswith(str(REPO_ROOT.resolve())):
        raise ValueError(f"Path must stay inside repository: {rel}")
    return rel


def norm_manifest_path(raw: str) -> str:
    """
    Directory entries end with /. Single-file entries have no trailing slash
    so path_under_prefix matches the file (e.g. CLAUDE.md, not CLAUDE.md/).
    """
    p = raw.replace("\\", "/").strip()
    if p.startswith("./"):
        p = p[2:]
    p = p.rstrip("/")
    if not p:
        return ""

    root_files = {
        "CLAUDE.md",
        "README.md",
        "CONTRIBUTING.md",
        "SYNTHESIS_PROJECT.md",
        "AGENTS.md",
        "GEMINI.md",
        ".cursorrules",
        ".cursorrules.md",
        ".gitignore",
    }
    if p in root_files:
        return p

    full = REPO_ROOT / p
    try:
        if full.is_dir():
            return p + "/"
        if full.is_file():
            return p
    except OSError:
        pass
    base = Path(p).name
    # Hidden agent dirs often not committed yet
    if base.startswith(".") and "/" not in p:
        return p + "/"
    if "." not in base:
        return p + "/"
    return p


def merge_framework_paths(manifest: dict, ps: dict) -> None:
    """Append prior_agents.framework_path_allowlist into manifest framework_paths (deduped)."""
    prior = ps.get("prior_agents") or {}
    extras = list(prior.get("framework_path_allowlist") or [])
    base = list(manifest.get("framework_paths") or [])

    merged: list[str] = []
    seen: set[str] = set()
    for raw in base + extras:
        np = norm_manifest_path(raw)
        if np and np not in seen:
            merged.append(np)
            seen.add(np)
    manifest["framework_paths"] = merged


def merge_shared_readonly_paths(manifest: dict, ps: dict) -> None:
    """Allow edits under prior agent dirs without unknown-path failures."""
    prior = ps.get("prior_agents") or {}
    extras = list(prior.get("framework_path_allowlist") or [])
    base = list(manifest.get("shared_readonly_paths") or [])

    merged: list[str] = []
    seen: set[str] = set()
    for raw in base + extras:
        np = norm_manifest_path(raw)
        if np and np not in seen:
            merged.append(np)
            seen.add(np)
    manifest["shared_readonly_paths"] = merged


def sync_partitions(manifest: dict, ps: dict) -> dict:
    """Return updated manifest (copy) from project_settings."""
    paths = ps["paths"]
    be = normalize_dir(paths["backend_dir"]) + "/"
    ct = normalize_dir(paths["contract_dir"]) + "/"
    fe_list: list[str] = []
    if paths.get("has_frontend"):
        fe_list = [normalize_dir(paths["frontend_dir"]) + "/"]

    out = json.loads(json.dumps(manifest))
    out["partitions"]["backend"]["paths"] = [be]
    out["partitions"]["contract"]["paths"] = [ct]
    out["partitions"]["frontend"]["paths"] = fe_list

    openapi_rel = normalize_dir(paths["openapi_spec"])
    integ = ps.get("integration", {})
    out["reference_example"] = {
        "openapi_spec": openapi_rel,
        "backend_dir": normalize_dir(paths["backend_dir"]),
        "frontend_dir": normalize_dir(paths["frontend_dir"]),
        "integration_app": integ.get("app_module", "app.main:app"),
        "integration_port": int(integ.get("port", 8765)),
    }

    layout = ps.get("layout") or {}
    prefixes = layout.get("reference_example_prefixes")
    if prefixes is not None:
        out["reference_example_prefixes"] = list(prefixes)

    merge_framework_paths(out, ps)
    merge_shared_readonly_paths(out, ps)

    return out


def render_project_md(ps: dict) -> str:
    paths = ps["paths"]
    proj = ps.get("project", {})
    models = ps.get("models", {})
    steering = ps.get("steering", {})
    ci = ps.get("ci", {})
    integ = ps.get("integration", {})
    git = ps.get("git", {})

    questions = models.get("questions_before_start") or []
    q_block = "\n".join(f"- {q}" for q in questions) if questions else "- _(none configured — add in next `configure_synthesis` run)_"

    sessions = steering.get("sessions") or []
    sess_block = "\n".join(
        f"- **{s.get('date', '')}**: {s.get('note', '')}" for s in sessions[-15:]
    )
    if not sess_block.strip():
        sess_block = "- _(no steering notes yet)_"

    prior = ps.get("prior_agents") or {}
    known = prior.get("known_files") or []
    if known:
        show = known[:40]
        more = len(known) - len(show)
        lines = "\n".join(f"- `{f}`" for f in show)
        if more > 0:
            lines += f"\n- _…and {more} more (see `synthesis/project_settings.json` → `prior_agents.known_files`)_"
        prior_block = lines
    else:
        prior_block = "- _(none discovered — run wizard with scan enabled, or commit your `.cursor` / `CLAUDE.md` / `.claude` files)_"

    allow = prior.get("framework_path_allowlist") or []
    allow_txt = ", ".join(f"`{a}`" for a in allow) if allow else "_(default)_"
    import_sum = prior.get("import_summary") or "_(none)_"
    disc_at = prior.get("discovered_at") or "_(never)_"

    fe_note = (
        f"`{paths['frontend_dir']}`"
        if paths.get("has_frontend")
        else "_No frontend partition (API-only / backend-first)._"
    )

    return f"""# Synthesis project context (generated)

**Do not hand-edit** except in an emergency; run `python3 scripts/configure_synthesis.py` to update.

## For AI agents (Cursor / Claude Code)

Before substantive work:

1. Read this file and `docs/CONSOLIDATED_MODEL.md` §3 (Type A/B/C routing).
2. Answer the **pre-flight questions** below (or confirm assumptions with the user if answers are missing).
3. Confirm which **loop** you are in: Backend (1), Frontend (2), Integration (3), or Mutation (contract extension).
4. Obey **one commit per partition**; never relax server validation to accept invalid clients.
5. **Respect prior agent artifacts** listed below: they may contain team conventions. If they **conflict** with `docs/CONSOLIDATED_MODEL.md` (partitions, contract-first, no invalid-input hacks), **this workflow wins**—note the conflict to the user.

### Pre-flight questions (ask user if unknown)

{q_block}

---

## Prior Cursor / Claude / agent context (discovered)

**Last scan:** {disc_at}

**Framework path allowlist** (partition checker treats these like framework paths): {allow_txt}

### Known files (read for alignment, do not override synthesis rules)

{prior_block}

### Imported summary (from prior setup)

{import_sum}

---

## Project

| Field | Value |
|-------|--------|
| **Name** | {proj.get('name') or '_(unset)_'} |
| **Partition base (git)** | `{git.get('partition_check_base', 'origin/main')}` |

### Goals

{proj.get('goals') or '_(none)_'}

### Constraints / non-goals

{proj.get('constraints') or '_(none)_'}

### Tech / stack notes

{proj.get('tech_notes') or '_(none)_'}

---

## Repository paths (authoritative)

| Role | Path |
|------|------|
| Contract directory | `{paths['contract_dir']}` |
| OpenAPI file | `{paths['openapi_spec']}` |
| Backend | `{paths['backend_dir']}` |
| Frontend | {fe_note} |

### Integration smoke

- **ASGI app**: `{integ.get('app_module', '')}`
- **Port**: `{integ.get('port', '')}`

### CI commands (local or workflow)

| Step | Command / skip |
|------|----------------|
| Backend install | `{ci.get('backend_install_cmd', '')}` |
| Backend tests | `{ci.get('backend_test_cmd', '')}` | skip: `{ci.get('skip_backend_tests', False)}` |
| Client install | `{ci.get('client_install_cmd', '')}` | skip job: `{ci.get('skip_client_job', False)}` |
| Client check | `{ci.get('client_check_cmd', '')}` |
| Integration smoke | _(script)_ | skip: `{ci.get('skip_integration_smoke', False)}` |

---

## Steering log (recent)

{sess_block}

---

_Regenerated: {datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")}_
"""


def run_wizard() -> dict:
    ps = load_json(SETTINGS_PATH)
    manifest = load_json(PARTITIONS_PATH)

    print("\n=== Constrained synthesis — project setup ===\n")
    if ps.get("initialized"):
        print("Existing configuration found. Press Enter to keep each default.\n")
    else:
        print("First-time setup. Defaults match this repo's root layout (`api_spec/`, `server/`, `client/`).\n")

    if ps.get("initialized") and prompt_yes("Add a steering note for this session?", default=False):
        note = prompt("Steering note (one line)")
        if note:
            ps.setdefault("steering", {}).setdefault("sessions", []).append(
                {
                    "date": datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC"),
                    "note": note,
                }
            )

    # Prior Cursor / Claude / other agent files (repos that already used agents)
    if prompt_yes(
        "Scan repo for existing Cursor/Claude/agent files (.cursor, CLAUDE.md, .claude, etc.)?",
        default=True,
    ):
        disc = discover_prior_agent_files(REPO_ROOT)
        ps.setdefault("prior_agents", {})
        ps["prior_agents"]["known_files"] = disc["files"]
        ps["prior_agents"]["discovered_at"] = datetime.now(timezone.utc).strftime(
            "%Y-%m-%d %H:%M UTC"
        )
        sug = list(disc.get("suggested_framework_prefixes") or [])
        cur_allow = list(ps["prior_agents"].get("framework_path_allowlist") or [])
        for p in sug:
            if p not in cur_allow:
                cur_allow.append(p)
        ps["prior_agents"]["framework_path_allowlist"] = cur_allow
        print(f"\nFound {len(disc['files'])} prior agent-related file(s).")
        if disc["files"][:15]:
            print("Sample:")
            for f in disc["files"][:15]:
                print(f"  - {f}")
        if prompt_yes(
            "Paste a short summary of goals/constraints from your old CLAUDE.md or team docs to store for models?",
            default=False,
        ):
            ps["prior_agents"]["import_summary"] = multiline_until_empty("Summary (empty line to finish)")
        extra = prompt(
            "Extra paths to treat as framework (comma-separated, e.g. .kilocode/, docs/team/) — optional",
            ",".join(ps["prior_agents"].get("framework_path_allowlist") or []),
        )
        if extra.strip():
            for p in parse_path_list(extra):
                np = p if p.endswith("/") else p + "/"
                if np not in ps["prior_agents"]["framework_path_allowlist"]:
                    ps["prior_agents"]["framework_path_allowlist"].append(np)
    else:
        ps.setdefault("prior_agents", {})

    use_demo = prompt_yes(
        "Use this repository's default root layout (api_spec/, server/, client/)?",
        default=bool(ps.get("layout", {}).get("use_bundled_demo_paths", True)),
    )
    if use_demo:
        ps["paths"] = {
            "contract_dir": "api_spec",
            "openapi_spec": "api_spec/openapi.yaml",
            "backend_dir": "server",
            "frontend_dir": "client",
            "has_frontend": True,
        }
        ps.setdefault("layout", {})["use_bundled_demo_paths"] = True
        ps["layout"]["reference_example_prefixes"] = []
        ps.setdefault("integration", {})["app_module"] = "app.main:app"
        ps.setdefault("integration", {})["port"] = 8765
    else:
        ps.setdefault("layout", {})["use_bundled_demo_paths"] = False
        ps["layout"]["reference_example_prefixes"] = []
        print("\nEnter paths relative to repository root.\n")
        cd = prompt("Contract directory", ps["paths"].get("contract_dir", "contracts"))
        spec = prompt(
            "OpenAPI spec file",
            ps["paths"].get("openapi_spec", f"{cd}/openapi.yaml"),
        )
        bd = prompt("Backend root directory", ps["paths"].get("backend_dir", "server"))
        has_fe = prompt_yes("Does this project have a frontend partition to enforce?", default=True)
        fd = ""
        if has_fe:
            fd = prompt(
                "Frontend root directory",
                ps["paths"].get("frontend_dir", "client"),
            )
        else:
            fd = ps["paths"].get("frontend_dir", bd)

        for rel in (cd, spec, bd, fd):
            ensure_under_repo(normalize_dir(rel))

        ps["paths"] = {
            "contract_dir": normalize_dir(cd),
            "openapi_spec": normalize_dir(spec),
            "backend_dir": normalize_dir(bd),
            "frontend_dir": normalize_dir(fd),
            "has_frontend": has_fe,
        }

        ex = prompt(
            "Optional: path prefix to exempt from strict per-commit partition rules (demo only, or leave empty)",
            "",
        )
        if ex.strip():
            ps["layout"]["reference_example_prefixes"] = [normalize_dir(ex) + "/"]

    ps["project"]["name"] = prompt("Project name", ps["project"].get("name", ""))
    ps["project"]["goals"] = prompt(
        "Short project goals (one line, optional)",
        ps["project"].get("goals", "").split("\n")[0] if ps["project"].get("goals") else "",
    )
    if prompt_yes("Edit multi-line goals now?", default=False):
        ps["project"]["goals"] = multiline_until_empty("Goals")

    if prompt_yes("Edit constraints / non-goals (multi-line)?", default=False):
        ps["project"]["constraints"] = multiline_until_empty("Constraints")

    if prompt_yes("Edit tech notes (multi-line)?", default=False):
        ps["project"]["tech_notes"] = multiline_until_empty("Tech notes")

    print("\n--- Integration smoke (backend must expose this ASGI app) ---\n")
    ps.setdefault("integration", {})
    ps["integration"]["app_module"] = prompt(
        "Uvicorn app import (e.g. app.main:app)",
        ps["integration"].get("app_module", "app.main:app"),
    )
    port_s = prompt(
        "Port for smoke test",
        str(ps["integration"].get("port", 8765)),
    )
    ps["integration"]["port"] = int(re.sub(r"[^\d]", "", port_s) or "8765")

    print("\n--- Git ---\n")
    ps.setdefault("git", {})
    ps["git"]["partition_check_base"] = prompt(
        "Branch/ref for partition diff base",
        ps["git"].get("partition_check_base", "origin/main"),
    )

    print("\n--- CI / local commands (used in docs and optional copy-paste) ---\n")
    ps.setdefault("ci", {})
    if not ps["paths"].get("has_frontend"):
        ps["ci"]["skip_client_job"] = True
        print("No frontend partition: client CI job will be skipped.\n")
    else:
        ps["ci"]["skip_client_job"] = not prompt_yes("Run client (npm) job in CI?", default=True)

    ps["ci"]["skip_integration_smoke"] = not prompt_yes(
        "Run integration smoke (GET /health) in CI?", default=True
    )
    ps["ci"]["skip_backend_tests"] = not prompt_yes("Run backend tests in CI?", default=True)

    ps["ci"]["backend_install_cmd"] = prompt(
        "Backend install cmd (from backend dir)",
        ps["ci"].get("backend_install_cmd", 'pip install -e ".[dev]"'),
    )
    ps["ci"]["backend_test_cmd"] = prompt(
        "Backend test cmd",
        ps["ci"].get("backend_test_cmd", "python3 -m pytest -q"),
    )
    if ps["paths"].get("has_frontend") and not ps["ci"].get("skip_client_job"):
        ps["ci"]["client_install_cmd"] = prompt(
            "Client install cmd",
            ps["ci"].get("client_install_cmd", "npm ci"),
        )
        ps["ci"]["client_check_cmd"] = prompt(
            "Client check cmd",
            ps["ci"].get("client_check_cmd", "npm run check"),
        )

    print("\n--- Questions models should ask before starting (optional) ---\n")
    print("Enter one question per line; empty line to finish.")
    new_q: list[str] = []
    while True:
        q = input("> ").strip()
        if not q:
            break
        new_q.append(q)
    if new_q:
        ps.setdefault("models", {})["questions_before_start"] = new_q
    elif not ps.get("models", {}).get("questions_before_start"):
        ps.setdefault("models", {})["questions_before_start"] = [
            "Which loop are we running (backend / frontend / integration / contract extension)?",
            "What is the acceptance criterion for this change?",
            "Are we allowed to change the OpenAPI spec in this session?",
        ]

    ps["initialized"] = True
    ps["version"] = max(1, int(ps.get("version", 1)))

    updated_manifest = sync_partitions(manifest, ps)
    save_json(PARTITIONS_PATH, updated_manifest)
    save_json(SETTINGS_PATH, ps)

    md = render_project_md(ps)
    PROJECT_MD.write_text(md, encoding="utf-8")

    print("\n=== Done ===\n")
    print(f"Wrote: {SETTINGS_PATH.relative_to(REPO_ROOT)}")
    print(f"Synced: {PARTITIONS_PATH.relative_to(REPO_ROOT)}")
    print(f"Wrote: {PROJECT_MD.relative_to(REPO_ROOT)}")
    print("\nNext steps:")
    print("  1. Commit SYNTHESIS_PROJECT.md and synthesis/ changes (or keep local).")
    print("  2. In Cursor, @-mention SYNTHESIS_PROJECT.md at session start.")
    print("  3. Run: python3 scripts/check_partition_boundaries.py")
    print("  4. See docs/SCENARIOS.md for edge cases.\n")

    return ps


def discover_only() -> None:
    disc = discover_prior_agent_files(REPO_ROOT)
    print(f"Found {len(disc['files'])} file(s):\n")
    for f in disc["files"]:
        print(f"  {f}")
    print("\nSuggested framework prefixes:", ", ".join(disc["suggested_framework_prefixes"]))


def sync_only() -> None:
    ps = load_json(SETTINGS_PATH)
    if not ps.get("initialized"):
        print("project_settings not initialized; run without --sync-only first.", file=sys.stderr)
        sys.exit(1)
    manifest = load_json(PARTITIONS_PATH)
    updated = sync_partitions(manifest, ps)
    save_json(PARTITIONS_PATH, updated)
    save_json(SETTINGS_PATH, ps)
    PROJECT_MD.write_text(render_project_md(ps), encoding="utf-8")
    print(f"Synced {PARTITIONS_PATH.name} and {PROJECT_MD.name}.")


def main() -> None:
    import argparse

    ap = argparse.ArgumentParser(description="Interactive synthesis project setup")
    ap.add_argument(
        "--sync-only",
        action="store_true",
        help="Regenerate SYNTHESIS_PROJECT.md and sync partitions from project_settings.json (no prompts)",
    )
    ap.add_argument(
        "--discover-only",
        action="store_true",
        help="Print paths to Cursor/Claude/agent-related files and exit (no writes)",
    )
    args = ap.parse_args()

    if not SETTINGS_PATH.is_file():
        if SETTINGS_TEMPLATE.is_file():
            SETTINGS_PATH.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(SETTINGS_TEMPLATE, SETTINGS_PATH)
            print(
                f"Created {SETTINGS_PATH.relative_to(REPO_ROOT)} from template "
                "(initialized=false). Complete the wizard to finish setup.\n"
            )
        else:
            print(
                f"Missing {SETTINGS_PATH}. Clone the framework repo and run:\n"
                f"  python3 scripts/install_framework.py --target {REPO_ROOT}\n",
                file=sys.stderr,
            )
            sys.exit(1)

    if not PARTITIONS_PATH.is_file():
        print(
            f"Missing {PARTITIONS_PATH}.\n"
            "Install framework files into this repository first:\n"
            "  git clone <framework-repo-url> /tmp/synthesis-framework\n"
            f"  python3 /tmp/synthesis-framework/scripts/install_framework.py --target {REPO_ROOT}\n"
            "Then run this wizard again.\n",
            file=sys.stderr,
        )
        sys.exit(1)

    if args.discover_only:
        discover_only()
        return

    if args.sync_only:
        sync_only()
        return

    try:
        run_wizard()
    except KeyboardInterrupt:
        print("\nAborted.", file=sys.stderr)
        sys.exit(130)


if __name__ == "__main__":
    main()
