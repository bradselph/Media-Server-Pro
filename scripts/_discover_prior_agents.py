"""
Discover Cursor / Claude Code / related agent artifacts already in the repo.

Used by configure_synthesis.py to align synthesis workflow with prior setup.
Only scans the working tree (committed + untracked files on disk).
"""

from __future__ import annotations

import fnmatch
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]

SKIP_DIR_NAMES = {
    ".git",
    "node_modules",
    "__pycache__",
    ".venv",
    "venv",
    "dist",
    "build",
    ".pytest_cache",
    ".mypy_cache",
    ".ruff_cache",
    "coverage",
    ".next",
    ".turbo",
}

# Extra dirs to treat as framework-ish when user opts in (avoid partition unknown-path failures)
DEFAULT_EXTRA_FRAMEWORK_PREFIXES = [
    ".claude/",
    ".roo/",
]


def _is_skipped_dir(path: Path) -> bool:
    return any(part in SKIP_DIR_NAMES for part in path.parts)


def _rel(path: Path) -> str:
    try:
        return path.relative_to(REPO_ROOT).as_posix()
    except ValueError:
        return str(path)


def discover_prior_agent_files(repo_root: Path | None = None) -> dict:
    root = repo_root or REPO_ROOT
    files: list[str] = []
    seen: set[str] = set()

    def add(p: Path) -> None:
        if not p.is_file():
            return
        r = _rel(p)
        if r not in seen:
            seen.add(r)
            files.append(r)

    # Root-level conventional files
    for name in (".cursorrules", ".cursorrules.md", "CLAUDE.md", "AGENTS.md", "GEMINI.md"):
        cand = root / name
        if cand.is_file():
            add(cand)

    # docs/CLAUDE.md
    dc = root / "docs" / "CLAUDE.md"
    if dc.is_file():
        add(dc)

    # .cursor/rules and other .cursor files (shallow + rules deep)
    cursor = root / ".cursor"
    if cursor.is_dir():
        for pattern in ("**/*.mdc", "**/*.md"):
            for p in cursor.glob(pattern):
                if p.is_file() and not _is_skipped_dir(p):
                    add(p)

    # .claude — markdown and text only (avoid shipping large binary)
    claude_dir = root / ".claude"
    if claude_dir.is_dir():
        for p in claude_dir.rglob("*"):
            if not p.is_file() or _is_skipped_dir(p):
                continue
            if p.suffix.lower() in (".md", ".txt"):
                add(p)

    # Roo / Cline style
    for agent_dir in (".roo", ".cline"):
        d = root / agent_dir
        if d.is_dir():
            for p in d.rglob("*.md"):
                if p.is_file() and not _is_skipped_dir(p):
                    add(p)

    # docs: memory / context style names
    docs = root / "docs"
    if docs.is_dir():
        patterns = (
            "*MEMORY*",
            "*memory*",
            "*CONTEXT*",
            "*context*",
            "*DECISION*",
            "*decision*",
            "*NOTES*",
            "*notes*",
            "AGENTS.md",
            "agents.md",
        )
        for pat in patterns:
            for p in docs.glob(pat):
                if p.is_file() and not _is_skipped_dir(p):
                    add(p)
        # Second level: filename keywords only (avoid matching AGENT_PLAYBOOK etc.)
        doc_keywords = ("MEMORY", "CONTEXT", "DECISION", "NOTES")
        for p in docs.rglob("*.md"):
            if _is_skipped_dir(p):
                continue
            name = p.name.upper()
            if name in ("AGENTS.MD", "CLAUDE.MD"):
                add(p)
            elif any(k in name for k in doc_keywords):
                add(p)

    files.sort()
    return {
        "files": files,
        "suggested_framework_prefixes": list(DEFAULT_EXTRA_FRAMEWORK_PREFIXES),
    }


def parse_path_list(line: str) -> list[str]:
    """Comma-separated or newline-separated relative paths."""
    parts: list[str] = []
    for chunk in line.replace(",", "\n").splitlines():
        s = chunk.strip().strip('"').strip("'")
        if s:
            parts.append(s)
    return parts
