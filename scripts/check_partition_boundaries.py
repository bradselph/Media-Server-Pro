#!/usr/bin/env python3
"""
Validate that changed paths respect synthesis partition boundaries.

Usage:
  python3 scripts/check_partition_boundaries.py [--base REF] [--head REF]

Env:
  SYNTHESIS_STRICT=0  — only warn on unknown paths (default: 1 = fail)

Uses synthesis/project_settings.json when initialized; else synthesis/partitions.json.
Compares each commit in base..head: a single commit must not mix forbidden partitions.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
MANIFEST = REPO_ROOT / "synthesis" / "partitions.json"

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from _synthesis_config import (  # noqa: E402
    effective_partition_prefixes,
    partition_check_base,
    reference_example_prefixes_for_demo,
)


def run_git(args: list[str]) -> str:
    r = subprocess.run(
        ["git", "-C", str(REPO_ROOT), *args],
        capture_output=True,
        text=True,
        check=False,
    )
    if r.returncode != 0:
        print(r.stderr or r.stdout, file=sys.stderr)
        sys.exit(r.returncode or 1)
    return r.stdout


def resolve_base(base: str) -> str | None:
    r = subprocess.run(
        ["git", "-C", str(REPO_ROOT), "rev-parse", "--verify", base],
        capture_output=True,
        text=True,
    )
    if r.returncode == 0:
        return r.stdout.strip()
    return None


def load_manifest_file() -> dict:
    with open(MANIFEST, encoding="utf-8") as f:
        return json.load(f)


def normalize_path(p: str) -> str:
    p = p.replace("\\", "/").strip()
    if p.startswith("./"):
        p = p[2:]
    return p


def path_under_prefix(path: str, prefix: str) -> bool:
    if not prefix.endswith("/"):
        prefix = prefix + "/"
    return path == prefix.rstrip("/") or path.startswith(prefix)


def under_any_prefix(path: str, prefixes: list[str]) -> bool:
    for pref in prefixes:
        p = normalize_path(pref)
        if path_under_prefix(path, p) or path == p.rstrip("/"):
            return True
    return False


def classify(path: str, data: dict, eff: dict[str, list[str]]) -> set[str]:
    tags: set[str] = set()
    for name in ("backend", "frontend", "contract"):
        for prefix in eff.get(name, []):
            pfx = normalize_path(prefix)
            if path_under_prefix(path, pfx) or path == pfx.rstrip("/"):
                tags.add(name)
                break
    for prefix in data.get("shared_readonly_paths", []):
        p = normalize_path(prefix)
        if path_under_prefix(path, p) or path == p.rstrip("/"):
            tags.add("shared")
    for prefix in data.get("framework_paths", []):
        p = normalize_path(prefix)
        if path_under_prefix(path, p) or path == p.rstrip("/"):
            tags.add("framework")
    if path_under_prefix(path, "synthesis/") or path == normalize_path("synthesis"):
        tags.add("framework")
    proj_md = normalize_path("SYNTHESIS_PROJECT.md")
    if path == proj_md:
        tags.add("framework")
    return tags


def main() -> None:
    default_base = os.environ.get("SYNTHESIS_BASE") or partition_check_base()
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default=default_base)
    parser.add_argument("--head", default=os.environ.get("SYNTHESIS_HEAD", "HEAD"))
    args = parser.parse_args()
    strict = os.environ.get("SYNTHESIS_STRICT", "1") != "0"

    if not MANIFEST.is_file():
        print(f"Missing manifest: {MANIFEST}", file=sys.stderr)
        sys.exit(1)

    data = load_manifest_file()
    rules = data.get("rules", {})
    eff = effective_partition_prefixes(data)

    b = resolve_base(args.base)
    if b is None:
        print(f"Base ref not found ({args.base}); skipping diff (no comparison).")
        return

    out_all = run_git(["diff", "--name-only", f"{b}...{args.head}"])
    all_paths = [normalize_path(p) for p in out_all.splitlines() if p.strip()]
    if not all_paths:
        print("No changed files; partition check skipped.")
        return

    unknown: list[str] = []
    legacy = [normalize_path(p) for p in data.get("legacy_layout_prefixes", [])]

    for path in all_paths:
        tags = classify(path, data, eff)
        if not tags:
            if any(path_under_prefix(path, lp) or path == lp.rstrip("/") for lp in legacy):
                continue
            unknown.append(path)

    exit_code = 0

    if unknown:
        msg = (
            "Paths outside partitions, shared_readonly_paths, and framework_paths:\n  "
            + "\n  ".join(unknown)
        )
        if strict:
            print(msg, file=sys.stderr)
            exit_code = 1
        else:
            print("WARN: " + msg, file=sys.stderr)

    ref_prefixes = [normalize_path(p) for p in reference_example_prefixes_for_demo()]

    rev_list = run_git(["rev-list", "--reverse", f"{b}..{args.head}"]).strip().splitlines()
    for rev in rev_list:
        if not rev:
            continue
        names = run_git(["diff-tree", "--no-commit-id", "--name-only", "-r", rev])
        paths = [normalize_path(p) for p in names.splitlines() if p.strip()]
        touched = {"backend": False, "frontend": False, "contract": False}
        partition_paths: list[str] = []
        for path in paths:
            tags = classify(path, data, eff)
            if "backend" in tags:
                touched["backend"] = True
                partition_paths.append(path)
            if "frontend" in tags:
                touched["frontend"] = True
                partition_paths.append(path)
            if "contract" in tags:
                touched["contract"] = True
                partition_paths.append(path)

        demo_only = bool(partition_paths) and all(
            under_any_prefix(p, ref_prefixes) for p in partition_paths
        )

        if (
            rules.get("forbid_backend_and_frontend_same_diff")
            and not demo_only
            and touched["backend"]
            and touched["frontend"]
        ):
            print(
                f"Partition violation in commit {rev[:7]}: touches both backend and "
                "frontend partitions in one commit. Split into separate commits.",
                file=sys.stderr,
            )
            exit_code = 1

        if (
            rules.get("allow_contract_with_single_implementation_partition")
            and not demo_only
        ):
            impl_count = sum(1 for k in ("backend", "frontend") if touched[k])
            if touched["contract"] and impl_count > 1:
                print(
                    f"Partition violation in commit {rev[:7]}: contract plus both backend and "
                    "frontend in one commit. Order: contract → backend → frontend (separate commits).",
                    file=sys.stderr,
                )
                exit_code = 1

    if exit_code == 0:
        print("Partition boundary check passed.")

    sys.exit(exit_code)


if __name__ == "__main__":
    main()
