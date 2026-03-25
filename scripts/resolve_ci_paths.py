#!/usr/bin/env python3
"""Write synthesis CI variables to GITHUB_ENV or print KEY=value lines."""

from __future__ import annotations

import os
import sys
from pathlib import Path

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from _synthesis_config import (  # noqa: E402
    reference_example,
    resolved_backend_dir,
    resolved_frontend_dir,
    REPO_ROOT,
)


def main() -> None:
    ref = reference_example()
    be = resolved_backend_dir().relative_to(REPO_ROOT).as_posix()
    fe = resolved_frontend_dir().relative_to(REPO_ROOT).as_posix()
    lock = REPO_ROOT / fe / "package-lock.json"
    if lock.is_file():
        lock_rel = lock.relative_to(REPO_ROOT).as_posix()
    else:
        pkg = REPO_ROOT / fe / "package.json"
        lock_rel = pkg.relative_to(REPO_ROOT).as_posix() if pkg.is_file() else ""

    ci = ref.get("ci") or {}
    pairs = [
        ("SYNTHESIS_BACKEND_DIR", be),
        ("SYNTHESIS_FRONTEND_DIR", fe),
        ("SYNTHESIS_CLIENT_LOCK", lock_rel),
        ("SYNTHESIS_SKIP_CLIENT_JOB", "true" if ci.get("skip_client_job") else "false"),
        ("SYNTHESIS_SKIP_INTEGRATION_SMOKE", "true" if ci.get("skip_integration_smoke") else "false"),
        ("SYNTHESIS_SKIP_BACKEND_TESTS", "true" if ci.get("skip_backend_tests") else "false"),
        ("SYNTHESIS_PARTITION_BASE", str(ref.get("partition_check_base") or "origin/main")),
    ]

    gh_env = os.environ.get("GITHUB_ENV")
    if gh_env:
        with open(gh_env, "a", encoding="utf-8") as f:
            for k, v in pairs:
                f.write(f"{k}={v}\n")
    else:
        for k, v in pairs:
            print(f"{k}={v}")


if __name__ == "__main__":
    main()
