#!/usr/bin/env python3
"""Reject obvious incomplete code in implementation partitions (fitness gate).

When the repo root has go.mod, scans Go sources under api/, cmd/, internal/, and pkg/
(excluding *_test.go and vendor/).

Otherwise scans backend_dir/**/*.py and frontend_dir/src/**/*.{ts,tsx} (legacy layout).
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
    # Avoid matching Go's context.TODO().
    (re.compile(r"(?<!context\.)\bTODO\b", re.I), "TODO"),
    (re.compile(r"\bFIXME\b", re.I), "FIXME"),
    (re.compile(r"raise\s+NotImplementedError\b"), "NotImplementedError"),
    (re.compile(r'panic!\("not implemented"\)'), 'panic!("not implemented")'),
    (re.compile(r'panic\("not implemented"\)'), 'panic("not implemented")'),
    (re.compile(r"return\s+None\s*#\s*placeholder", re.I), "placeholder return"),
]


def iter_go_files() -> list[Path]:
    roots = ("api", "cmd", "internal", "pkg")
    files: list[Path] = []
    for name in roots:
        d = REPO_ROOT / name
        if not d.is_dir():
            continue
        for p in d.rglob("*.go"):
            if p.name.endswith("_test.go"):
                continue
            if "vendor" in p.parts:
                continue
            files.append(p)
    return sorted(set(files))


def iter_python_backend_files() -> list[Path]:
    server = resolved_backend_dir()
    if not server.is_dir():
        return []
    return sorted(
        p
        for p in server.rglob("*.py")
        if "__pycache__" not in p.parts and "venv" not in p.parts
    )


def iter_frontend_ts_files() -> list[Path]:
    ps = load_project_settings()
    has_fe = ps is None or ps.get("paths", {}).get("has_frontend", True)
    if not has_fe:
        return []
    client_src = resolved_frontend_dir() / "src"
    if not client_src.is_dir():
        return []
    files: list[Path] = []
    files.extend(p for p in client_src.rglob("*.ts") if "node_modules" not in p.parts)
    files.extend(p for p in client_src.rglob("*.tsx") if "node_modules" not in p.parts)
    return sorted(set(files))


def iter_files() -> list[Path]:
    if (REPO_ROOT / "go.mod").is_file():
        return iter_go_files()
    return iter_python_backend_files() + iter_frontend_ts_files()


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
