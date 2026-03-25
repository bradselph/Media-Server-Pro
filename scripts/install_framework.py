#!/usr/bin/env python3
"""
Copy constrained-synthesis framework files into an existing project repository.

Usage (from a clone of this framework repo):
  python3 scripts/install_framework.py --target /path/to/your/existing/repo

Or from curl (see docs/INSTALL_IN_YOUR_REPO.md).

Does NOT copy api_spec/, server/, client/, examples/ — only tooling, rules, docs, CI, synthesis manifest.
After install: cd your/repo && python3 scripts/configure_synthesis.py
"""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path

# Repo root containing this script (the framework checkout)
FRAMEWORK_ROOT = Path(__file__).resolve().parents[1]

# Tree copies (relative to framework root)
COPY_DIRS = [
    ".cursor",
    "docs",
    "scripts",
]

# Single files at framework root
COPY_FILES = [
    "CLAUDE.md",
    "CONTRIBUTING.md",
]

# Under synthesis/
SYNTHESIS_FILES = [
    "partitions.json",
    "project_settings.schema.json",
    "project_settings.template.json",
]

WORKFLOW_SRC = FRAMEWORK_ROOT / ".github" / "workflows" / "synthesis-ci.yml"


def copy_tree(src: Path, dst: Path, dry_run: bool, merge: bool) -> tuple[int, list[str]]:
    """Copy directory; if merge and dst exists, only copy missing files. Returns (files_copied, log)."""
    log: list[str] = []
    n = 0
    if not src.is_dir():
        return 0, [f"skip missing dir: {src}"]
    for path in sorted(src.rglob("*")):
        if path.is_dir():
            continue
        if "__pycache__" in path.parts or path.suffix == ".pyc":
            continue
        rel = path.relative_to(src)
        out = dst / rel
        if merge and out.exists():
            log.append(f"skip exists: {out}")
            continue
        if dry_run:
            log.append(f"would copy: {path} -> {out}")
            n += 1
            continue
        out.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, out)
        log.append(f"copied: {out}")
        n += 1
    return n, log


def main() -> None:
    ap = argparse.ArgumentParser(
        description="Install synthesis framework files into an existing project root"
    )
    ap.add_argument(
        "--target",
        type=Path,
        required=True,
        help="Absolute path to your existing project repository root",
    )
    ap.add_argument(
        "--source",
        type=Path,
        default=FRAMEWORK_ROOT,
        help="Framework checkout root (default: directory containing this script)",
    )
    ap.add_argument("--dry-run", action="store_true", help="Print actions only")
    ap.add_argument(
        "--merge",
        action="store_true",
        help="Do not overwrite existing files (only add missing)",
    )
    ap.add_argument(
        "--force-settings",
        action="store_true",
        help="Overwrite synthesis/project_settings.json from template",
    )
    args = ap.parse_args()

    src_root: Path = args.source.resolve()
    target: Path = args.target.resolve()

    if not src_root.is_dir():
        print(f"Source not a directory: {src_root}", file=sys.stderr)
        sys.exit(1)
    if not target.is_dir():
        print(f"Target not a directory: {target}", file=sys.stderr)
        sys.exit(1)
    if target.samefile(src_root):
        print("Target is the framework repo itself; use --target /path/to/your/other/project", file=sys.stderr)
        sys.exit(1)

    total = 0
    all_log: list[str] = []

    for dirname in COPY_DIRS:
        s = src_root / dirname
        d = target / dirname
        n, log = copy_tree(s, d, args.dry_run, args.merge)
        total += n
        all_log.extend(log)

    for name in COPY_FILES:
        s = src_root / name
        if not s.is_file():
            continue
        out = target / name
        if args.merge and out.exists():
            all_log.append(f"skip exists: {out}")
            continue
        if args.dry_run:
            all_log.append(f"would copy: {s} -> {out}")
            total += 1
            continue
        shutil.copy2(s, out)
        all_log.append(f"copied: {out}")
        total += 1

    syn_dst = target / "synthesis"
    if not args.dry_run:
        syn_dst.mkdir(parents=True, exist_ok=True)
    for name in SYNTHESIS_FILES:
        s = src_root / "synthesis" / name
        if not s.is_file():
            all_log.append(f"skip missing: {s}")
            continue
        out = syn_dst / name
        if args.merge and out.exists():
            all_log.append(f"skip exists: {out}")
            continue
        if args.dry_run:
            all_log.append(f"would copy: {s} -> {out}")
            total += 1
            continue
        shutil.copy2(s, out)
        all_log.append(f"copied: {out}")
        total += 1

    # project_settings.json from template when missing or --force-settings
    tmpl = src_root / "synthesis" / "project_settings.template.json"
    ps_out = syn_dst / "project_settings.json"
    if tmpl.is_file():
        write_ps = args.force_settings or not ps_out.exists()
        if args.merge and ps_out.exists() and not args.force_settings:
            write_ps = False
        if write_ps:
            if args.dry_run:
                all_log.append(f"would write: {ps_out} (from template)")
                total += 1
            else:
                shutil.copy2(tmpl, ps_out)
                all_log.append(f"copied template -> {ps_out}")
                total += 1
        else:
            all_log.append(f"skip project_settings.json (exists; use --force-settings to replace)")

    wf_dst = target / ".github" / "workflows" / "synthesis-ci.yml"
    if WORKFLOW_SRC.is_file():
        if args.merge and wf_dst.exists():
            all_log.append(f"skip exists: {wf_dst}")
        elif args.dry_run:
            all_log.append(f"would copy: {WORKFLOW_SRC} -> {wf_dst}")
            total += 1
        else:
            wf_dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(WORKFLOW_SRC, wf_dst)
            all_log.append(f"copied: {wf_dst}")
            total += 1

    for line in all_log:
        print(line)

    print(f"\nDone. {total} file(s) {'would be ' if args.dry_run else ''}installed.")
    if not args.dry_run:
        print("\nNext in YOUR project:")
        print(f"  cd {target}")
        print("  python3 scripts/configure_synthesis.py")
        print("  git add .cursor scripts synthesis docs .github CLAUDE.md CONTRIBUTING.md")
        print("  git commit -m \"Add constrained synthesis framework\"")


if __name__ == "__main__":
    main()
