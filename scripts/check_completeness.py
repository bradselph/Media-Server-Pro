#!/usr/bin/env python3
"""
Reject obvious incomplete code in implementation partitions (fitness gate).

Scans backend_dir/**/*.py and frontend_dir/src/**/*.ts (and .tsx) from synthesis/partitions.json.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from _synthesis_config import (  # noqa: E402
    load_project_settings,
    resolved_backend_dir,
    resolved_frontend_dir,
)

REPO_ROOT = Path(__file__).resolve().parents[1]

PATTERNS = [
    (re.compile(r"\bTODO\b", re.I), "TODO"),
    (re.compile(r"\bFIXME\b", re.I), "FIXME"),
    (re.compile(r"raise\s+NotImplementedError\b"), "NotImplementedError"),
    (re.compile(r"panic!\(\"not implemented\"\)"), 'panic!("not implemented")'),
    (re.compile(r"panic\(\"not implemented\"\)"), 'panic("not implemented")'),
    (re.compile(r"return\s+None\s*#\s*placeholder", re.I), "placeholder return"),
]


def iter_files() -> list[Path]:
    files: list[Path] = []
    server = resolved_backend_dir()
    if server.is_dir():
        files.extend(p for p in server.rglob("*.py") if "__pycache__" not in p.parts)
    ps = load_project_settings()
    has_fe = ps is None or ps.get("paths", {}).get("has_frontend", True)
    if has_fe:
        client_src = resolved_frontend_dir() / "src"
        if client_src.is_dir():
            files.extend(client_src.rglob("*.ts"))
            files.extend(client_src.rglob("*.tsx"))
    return sorted(set(files))


def main() -> None:
    bad: list[tuple[Path, int, str, str]] = []
    for path in iter_files():
        try:
            text = path.read_text(encoding="utf-8")
        except OSError as e:
            print(f"Cannot read {path}: {e}", file=sys.stderr)
            sys.exit(1)
        for i, line in enumerate(text.splitlines(), 1):
            for rx, label in PATTERNS:
                if rx.search(line):
                    bad.append((path, i, label, line.strip()))

    if bad:
        print("Completeness check failed:", file=sys.stderr)
        for path, line_no, label, line in bad:
            rel = path.relative_to(REPO_ROOT)
            print(f"  {rel}:{line_no} [{label}] {line}", file=sys.stderr)
        sys.exit(1)

    print("Completeness check passed.")


if __name__ == "__main__":
    main()
