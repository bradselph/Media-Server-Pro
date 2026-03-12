#!/usr/bin/env python3
"""
codescene-check.py — CodeScene-style code health analysis for pre-push checks

Detects the same code health issues as the CodeScene IDE extension:
  - Complex Method       (high cyclomatic/cognitive complexity)
  - Brain Method         (complex + long + deeply nested)
  - Large Method         (too many lines of code)
  - Bumpy Road Ahead     (multiple conditionals at different nesting levels)
  - Deep Nested Logic    (excessive nesting depth)
  - Excess Arguments     (too many function parameters)
  - Complex Conditional  (long boolean expressions)
  - Large File           (file exceeds healthy size)

Analyzes only changed files (git diff vs base branch) by default.

Usage:
    python scripts/codescene-check.py                      # changed files vs main
    python scripts/codescene-check.py --base development   # custom base branch
    python scripts/codescene-check.py --all                # all project files
    python scripts/codescene-check.py --files a.go b.ts    # specific files
    python scripts/codescene-check.py --threshold 5.0      # min health score
    python scripts/codescene-check.py --json               # JSON output
    python scripts/codescene-check.py --verbose            # show per-function details
"""

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

# ── Thresholds (aligned with CodeScene defaults) ────────────────────────────

CC_WARNING = 10          # Cyclomatic complexity: warning
CC_CRITICAL = 15         # Cyclomatic complexity: critical
COGNITIVE_WARNING = 10   # Cognitive complexity: warning
COGNITIVE_CRITICAL = 15  # Cognitive complexity: critical
FUNC_LINES_WARNING = 60  # Function length: warning
FUNC_LINES_CRITICAL = 100  # Function length: critical
MAX_PARAMS = 4           # Excess function arguments
MAX_NESTING = 3          # Deep nesting threshold (CodeScene uses 3)
FILE_LINES_WARNING = 500  # File length: warning
FILE_LINES_CRITICAL = 800  # File length: critical
BRAIN_CC = 10            # Brain method: CC threshold
BRAIN_LINES = 50         # Brain method: lines threshold
BRAIN_NESTING = 3        # Brain method: nesting threshold
BUMPY_ROAD_CHUNKS = 3    # Bumpy road: min conditional chunks
COMPLEX_COND_OPS = 3     # Complex conditional: min boolean operators

# ── Colors ──────────────────────────────────────────────────────────────────

NO_COLOR = os.environ.get("NO_COLOR", "") != "" or not sys.stdout.isatty()

def _c(code: str, text: str) -> str:
    if NO_COLOR:
        return text
    return f"\033[{code}m{text}\033[0m"

def red(t: str) -> str: return _c("0;31", t)
def green(t: str) -> str: return _c("0;32", t)
def yellow(t: str) -> str: return _c("1;33", t)
def cyan(t: str) -> str: return _c("0;36", t)
def bold(t: str) -> str: return _c("1", t)
def dim(t: str) -> str: return _c("2", t)

# ── Data structures ─────────────────────────────────────────────────────────

@dataclass
class CodeSmell:
    name: str           # e.g. "Complex Method", "Brain Method"
    severity: str       # "warning" or "critical"
    message: str        # Human-readable explanation
    function: str       # Function name (or "" for file-level)
    line: int           # Line number
    details: dict = field(default_factory=dict)

@dataclass
class FuncInfo:
    name: str
    receiver: str       # Go receiver type (empty for plain funcs / TS)
    start_line: int
    end_line: int
    lines: int
    params: int
    max_nesting: int
    nesting_bumps: int  # number of nesting-level changes (for Bumpy Road)
    cc: int = 0         # cyclomatic complexity (filled by gocyclo)
    cognitive: int = 0  # cognitive complexity (filled by gocognit)

@dataclass
class FileHealth:
    path: str
    score: float = 10.0
    smells: list = field(default_factory=list)
    metrics: dict = field(default_factory=dict)
    func_count: int = 0
    total_lines: int = 0

# ── Repository detection ────────────────────────────────────────────────────

def find_repo_root() -> str:
    """Find the git repository root."""
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True, text=True, check=True,
        )
        return result.stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        # Fallback: walk up looking for go.mod
        d = Path.cwd()
        while d != d.parent:
            if (d / "go.mod").exists():
                return str(d)
            d = d.parent
        return str(Path.cwd())

# ── Git helpers ─────────────────────────────────────────────────────────────

def get_changed_files(repo_root: str, base_branch: str) -> list[str]:
    """Get files changed between base branch and HEAD."""
    # Try merge-base first (works for diverged branches)
    try:
        merge_base = subprocess.run(
            ["git", "merge-base", base_branch, "HEAD"],
            capture_output=True, text=True, check=True, cwd=repo_root,
        ).stdout.strip()
        result = subprocess.run(
            ["git", "diff", "--name-only", "--diff-filter=ACMR", merge_base, "HEAD"],
            capture_output=True, text=True, check=True, cwd=repo_root,
        )
        files = result.stdout.strip().splitlines()
    except subprocess.CalledProcessError:
        files = []

    # Also include staged + unstaged changes (working tree)
    try:
        staged = subprocess.run(
            ["git", "diff", "--name-only", "--cached", "--diff-filter=ACMR"],
            capture_output=True, text=True, check=True, cwd=repo_root,
        ).stdout.strip().splitlines()
        unstaged = subprocess.run(
            ["git", "diff", "--name-only", "--diff-filter=ACMR"],
            capture_output=True, text=True, check=True, cwd=repo_root,
        ).stdout.strip().splitlines()
        files = list(set(files + staged + unstaged))
    except subprocess.CalledProcessError:
        pass

    return [f for f in files if f]

def filter_source_files(files: list[str]) -> tuple[list[str], list[str]]:
    """Split files into Go and TypeScript lists."""
    go_files = [f for f in files if f.endswith(".go") and not f.endswith("_test.go")]
    ts_files = [f for f in files if f.endswith((".ts", ".tsx"))
                and "node_modules" not in f
                and not f.endswith((".d.ts", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx"))]
    return go_files, ts_files

# ── Go tool helpers ─────────────────────────────────────────────────────────

def find_go_tool(name: str, install_pkg: str) -> Optional[str]:
    """Find a Go tool in PATH, or auto-install it."""
    # Check PATH
    try:
        result = subprocess.run(
            ["go", "env", "GOPATH"], capture_output=True, text=True, check=True
        )
        gopath = result.stdout.strip()
        gobin = os.path.join(gopath, "bin", name)
        if sys.platform == "win32":
            gobin += ".exe"
        if os.path.isfile(gobin):
            return gobin
    except (subprocess.CalledProcessError, FileNotFoundError):
        pass

    # Try bare name in PATH
    import shutil
    found = shutil.which(name)
    if found:
        return found

    # Auto-install
    try:
        print(f"  {dim('Installing ' + name + '...')}", file=sys.stderr)
        subprocess.run(
            ["go", "install", install_pkg],
            capture_output=True, text=True, check=True,
        )
        # Check again
        found = shutil.which(name)
        if found:
            return found
        # Check GOPATH/bin
        if os.path.isfile(gobin):
            return gobin
    except (subprocess.CalledProcessError, FileNotFoundError):
        pass

    return None

def run_gocyclo(tool_path: str, files: list[str], repo_root: str) -> dict[str, dict[str, int]]:
    """Run gocyclo and return {filepath: {func_name: complexity}}."""
    if not tool_path or not files:
        return {}

    abs_files = [os.path.join(repo_root, f) if not os.path.isabs(f) else f for f in files]
    # Filter to files that exist
    abs_files = [f for f in abs_files if os.path.isfile(f)]
    if not abs_files:
        return {}

    try:
        result = subprocess.run(
            [tool_path, "-over", "1"] + abs_files,
            capture_output=True, text=True, cwd=repo_root,
        )
        output = result.stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return {}

    # Output format: "15 pkg FuncName path/to/file.go:42:1"
    results: dict[str, dict[str, int]] = {}
    for line in output.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) < 4:
            continue
        try:
            cc = int(parts[0])
        except ValueError:
            continue
        func_name = parts[2]
        # File path is the last part, may contain :line:col
        file_part = parts[-1].split(":")[0]
        # Normalize path
        if os.path.isabs(file_part):
            rel_path = os.path.relpath(file_part, repo_root).replace("\\", "/")
        else:
            rel_path = file_part.replace("\\", "/")
        results.setdefault(rel_path, {})[func_name] = cc

    return results

def run_gocognit(tool_path: str, files: list[str], repo_root: str) -> dict[str, dict[str, int]]:
    """Run gocognit and return {filepath: {func_name: complexity}}."""
    if not tool_path or not files:
        return {}

    abs_files = [os.path.join(repo_root, f) if not os.path.isabs(f) else f for f in files]
    abs_files = [f for f in abs_files if os.path.isfile(f)]
    if not abs_files:
        return {}

    try:
        result = subprocess.run(
            [tool_path, "-over", "1"] + abs_files,
            capture_output=True, text=True, cwd=repo_root,
        )
        output = result.stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return {}

    # Output format same as gocyclo: "15 pkg FuncName path/to/file.go:42:1"
    results: dict[str, dict[str, int]] = {}
    for line in output.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) < 4:
            continue
        try:
            cog = int(parts[0])
        except ValueError:
            continue
        func_name = parts[2]
        file_part = parts[-1].split(":")[0]
        if os.path.isabs(file_part):
            rel_path = os.path.relpath(file_part, repo_root).replace("\\", "/")
        else:
            rel_path = file_part.replace("\\", "/")
        results.setdefault(rel_path, {})[func_name] = cog

    return results

# ── Go function parser ──────────────────────────────────────────────────────

# Match: func (r *Receiver) Name(params) or func Name(params)
_GO_FUNC_RE = re.compile(
    r'^func\s+'
    r'(?:\((\w+)\s+\*?[\w.]+\)\s+)?'  # optional receiver
    r'(\w+)\s*\('                       # function name
    r'([^)]*)'                          # params (may span line for complex sigs but rare)
    r'\)'
)

# Patterns that increase nesting in Go
_GO_NESTING_OPENERS = re.compile(
    r'\b(if|for|switch|select)\b'
)

# Complex conditional: count boolean operators in a line
_GO_BOOL_OPS = re.compile(r'&&|\|\|')


def _count_go_params(param_str: str) -> int:
    """Count Go function parameters (handles grouped types like a, b int)."""
    param_str = param_str.strip()
    if not param_str:
        return 0
    # Split by comma, but be careful about func types: func(a, b int) int
    # Simple approach: count commas + 1, subtract func-type internal commas
    # Better: count top-level commas (not inside nested parens)
    depth = 0
    count = 1
    for ch in param_str:
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
        elif ch == ',' and depth == 0:
            count += 1
    return count


def _is_in_string_or_comment(line: str, pos: int) -> bool:
    """Quick check if position might be in a string or comment."""
    # Check for // comment before the position
    dslash = line.find("//")
    if dslash >= 0 and dslash < pos:
        return True
    return False


def parse_go_functions(filepath: str) -> list[FuncInfo]:
    """Parse Go file to extract function metadata."""
    try:
        with open(filepath, "r", encoding="utf-8", errors="replace") as f:
            lines = f.readlines()
    except (OSError, IOError):
        return []

    functions: list[FuncInfo] = []
    i = 0

    while i < len(lines):
        line = lines[i]
        match = _GO_FUNC_RE.match(line)
        if not match:
            i += 1
            continue

        receiver = match.group(1) or ""
        name = match.group(2)
        params_str = match.group(3) or ""
        param_count = _count_go_params(params_str)

        # Find the opening brace
        start_line = i + 1  # 1-indexed
        brace_depth = 0
        func_started = False
        max_nesting = 0
        nesting_levels_seen = set()  # track distinct nesting levels for bumpy road
        j = i

        while j < len(lines):
            for ci, ch in enumerate(lines[j]):
                if ch == '{':
                    if not _is_in_string_or_comment(lines[j], ci):
                        brace_depth += 1
                        func_started = True
                        if brace_depth > max_nesting:
                            max_nesting = brace_depth
                elif ch == '}':
                    if not _is_in_string_or_comment(lines[j], ci):
                        brace_depth -= 1
                        if func_started and brace_depth == 0:
                            end_line = j + 1  # 1-indexed
                            func_lines = end_line - start_line + 1

                            # Count nesting bumps (conditional at different depths)
                            nesting_bumps = 0
                            for k in range(i, j + 1):
                                if _GO_NESTING_OPENERS.search(lines[k]):
                                    # Measure indentation as proxy for nesting level
                                    indent = len(lines[k]) - len(lines[k].lstrip())
                                    nesting_levels_seen.add(indent)
                            nesting_bumps = len(nesting_levels_seen)

                            functions.append(FuncInfo(
                                name=name,
                                receiver=receiver,
                                start_line=start_line,
                                end_line=end_line,
                                lines=func_lines,
                                params=param_count,
                                max_nesting=max_nesting - 1,  # subtract func's own brace
                                nesting_bumps=nesting_bumps,
                            ))
                            i = j
                            break
            else:
                j += 1
                continue
            break

        i += 1

    return functions


def count_complex_conditionals_go(filepath: str) -> list[tuple[int, int]]:
    """Find lines with complex boolean expressions. Returns [(line_num, operator_count)]."""
    results = []
    try:
        with open(filepath, "r", encoding="utf-8", errors="replace") as f:
            for i, line in enumerate(f, 1):
                stripped = line.strip()
                # Only check if/else if/for conditions
                if re.match(r'\b(if|for)\b', stripped):
                    ops = len(_GO_BOOL_OPS.findall(line))
                    if ops >= COMPLEX_COND_OPS:
                        results.append((i, ops))
    except (OSError, IOError):
        pass
    return results

# ── TypeScript function parser ──────────────────────────────────────────────

# Match various TS function forms
_TS_FUNC_PATTERNS = [
    # function name(params) {
    re.compile(r'^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)'),
    # const name = (params) => {  or  const name = function(params) {
    re.compile(r'^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:\([^)]*\)|[^=]+=>\s*\{|function\s*\([^)]*\))'),
    # class method: name(params) {
    re.compile(r'^\s+(?:async\s+)?(\w+)\s*\(([^)]*)\)\s*(?::\s*\S+\s*)?\{'),
]

_TS_NESTING_OPENERS = re.compile(r'\b(if|for|while|switch)\b')


def parse_ts_functions(filepath: str) -> list[FuncInfo]:
    """Parse TypeScript/TSX file to extract function metadata."""
    try:
        with open(filepath, "r", encoding="utf-8", errors="replace") as f:
            lines = f.readlines()
    except (OSError, IOError):
        return []

    functions: list[FuncInfo] = []
    i = 0

    while i < len(lines):
        line = lines[i]
        name = ""
        params_str = ""

        # Try each pattern
        for pattern in _TS_FUNC_PATTERNS:
            m = pattern.match(line)
            if m:
                name = m.group(1)
                params_str = m.group(2) if m.lastindex and m.lastindex >= 2 else ""
                break

        # Also detect React components: const Name = (...) => { (on same or next line)
        if not name:
            m = re.match(r'^\s*(?:export\s+)?(?:const|let)\s+([A-Z]\w+)\s*[:=]', line)
            if m and ('=>' in line or (i + 1 < len(lines) and '=>' in lines[i + 1])):
                name = m.group(1)

        if not name:
            i += 1
            continue

        # Skip common false positives
        if name in ('if', 'for', 'while', 'switch', 'catch', 'else', 'return', 'import', 'from', 'type', 'interface'):
            i += 1
            continue

        # Count params (simple comma count)
        param_count = 0
        if params_str and params_str.strip():
            depth = 0
            param_count = 1
            for ch in params_str:
                if ch in '({[<':
                    depth += 1
                elif ch in ')}]>':
                    depth -= 1
                elif ch == ',' and depth == 0:
                    param_count += 1

        # Find function body by tracking braces
        start_line = i + 1
        brace_depth = 0
        func_started = False
        max_nesting = 0
        nesting_levels_seen = set()
        j = i

        while j < len(lines):
            for ch in lines[j]:
                if ch == '{':
                    brace_depth += 1
                    func_started = True
                    if brace_depth > max_nesting:
                        max_nesting = brace_depth
                elif ch == '}':
                    brace_depth -= 1
                    if func_started and brace_depth == 0:
                        end_line = j + 1
                        func_lines = end_line - start_line + 1

                        for k in range(i, j + 1):
                            if _TS_NESTING_OPENERS.search(lines[k]):
                                indent = len(lines[k]) - len(lines[k].lstrip())
                                nesting_levels_seen.add(indent)

                        functions.append(FuncInfo(
                            name=name,
                            receiver="",
                            start_line=start_line,
                            end_line=end_line,
                            lines=func_lines,
                            params=param_count,
                            max_nesting=max(0, max_nesting - 1),
                            nesting_bumps=len(nesting_levels_seen),
                        ))
                        i = j
                        break
            else:
                j += 1
                # Safety: don't search more than 500 lines for closing brace
                if j - i > 500:
                    break
                continue
            break

        i += 1

    return functions

# ── Code smell detection ────────────────────────────────────────────────────

def detect_smells(func: FuncInfo) -> list[CodeSmell]:
    """Detect CodeScene-style code smells for a single function."""
    smells: list[CodeSmell] = []
    display_name = f"{func.receiver}.{func.name}" if func.receiver else func.name

    # Brain Method: complex + long + deeply nested (compound rule)
    is_brain = (
        func.cc >= BRAIN_CC
        and func.lines >= BRAIN_LINES
        and func.max_nesting >= BRAIN_NESTING
    )
    if is_brain:
        smells.append(CodeSmell(
            name="Brain Method",
            severity="critical",
            message=f"Complex ({func.cc}CC), long ({func.lines} lines), deeply nested ({func.max_nesting}) "
                    f"— this function centralizes too much behavior",
            function=display_name,
            line=func.start_line,
            details={"cc": func.cc, "lines": func.lines, "nesting": func.max_nesting},
        ))
    else:
        # Complex Method (only if not already flagged as Brain Method)
        if func.cc >= CC_CRITICAL:
            smells.append(CodeSmell(
                name="Complex Method",
                severity="critical",
                message=f"Cyclomatic complexity {func.cc} (threshold: {CC_CRITICAL})",
                function=display_name,
                line=func.start_line,
                details={"cc": func.cc, "threshold": CC_CRITICAL},
            ))
        elif func.cc >= CC_WARNING:
            smells.append(CodeSmell(
                name="Complex Method",
                severity="warning",
                message=f"Cyclomatic complexity {func.cc} (threshold: {CC_WARNING})",
                function=display_name,
                line=func.start_line,
                details={"cc": func.cc, "threshold": CC_WARNING},
            ))

        # Large Method
        if func.lines >= FUNC_LINES_CRITICAL:
            smells.append(CodeSmell(
                name="Large Method",
                severity="critical",
                message=f"{func.lines} lines (threshold: {FUNC_LINES_CRITICAL})",
                function=display_name,
                line=func.start_line,
                details={"lines": func.lines, "threshold": FUNC_LINES_CRITICAL},
            ))
        elif func.lines >= FUNC_LINES_WARNING:
            smells.append(CodeSmell(
                name="Large Method",
                severity="warning",
                message=f"{func.lines} lines (threshold: {FUNC_LINES_WARNING})",
                function=display_name,
                line=func.start_line,
                details={"lines": func.lines, "threshold": FUNC_LINES_WARNING},
            ))

    # Deep Nested Complexity
    if func.max_nesting > MAX_NESTING:
        smells.append(CodeSmell(
            name="Deep Nested Logic",
            severity="critical" if func.max_nesting > MAX_NESTING + 2 else "warning",
            message=f"Nesting depth {func.max_nesting} (threshold: {MAX_NESTING})",
            function=display_name,
            line=func.start_line,
            details={"nesting": func.max_nesting, "threshold": MAX_NESTING},
        ))

    # Bumpy Road Ahead: multiple conditionals at different nesting levels
    if func.nesting_bumps >= BUMPY_ROAD_CHUNKS and func.lines >= 30:
        smells.append(CodeSmell(
            name="Bumpy Road Ahead",
            severity="warning",
            message=f"{func.nesting_bumps} conditional groups at different nesting levels",
            function=display_name,
            line=func.start_line,
            details={"bumps": func.nesting_bumps, "threshold": BUMPY_ROAD_CHUNKS},
        ))

    # Excess Function Arguments
    if func.params > MAX_PARAMS:
        smells.append(CodeSmell(
            name="Excess Arguments",
            severity="critical" if func.params > MAX_PARAMS + 3 else "warning",
            message=f"{func.params} parameters (threshold: {MAX_PARAMS})",
            function=display_name,
            line=func.start_line,
            details={"params": func.params, "threshold": MAX_PARAMS},
        ))

    return smells

# ── Health score calculation ────────────────────────────────────────────────

def calculate_health_score(smells: list[CodeSmell], total_lines: int) -> float:
    """
    Calculate a CodeScene-style code health score (1.0 - 10.0).

    CodeScene weights nested complexity more heavily than raw cyclomatic complexity.
    Larger files carry more weight in scoring.
    """
    score = 10.0

    for smell in smells:
        # Deductions vary by smell type and severity
        if smell.severity == "critical":
            if smell.name == "Brain Method":
                score -= 2.5
            elif smell.name == "Complex Method":
                score -= 1.5
            elif smell.name == "Large Method":
                score -= 1.0
            elif smell.name == "Deep Nested Logic":
                score -= 2.0  # CodeScene weights nesting very heavily
            elif smell.name == "Excess Arguments":
                score -= 0.5
            else:
                score -= 1.0
        else:  # warning
            if smell.name == "Bumpy Road Ahead":
                score -= 0.8
            elif smell.name == "Complex Method":
                score -= 0.7
            elif smell.name == "Deep Nested Logic":
                score -= 1.0
            elif smell.name == "Large Method":
                score -= 0.5
            elif smell.name == "Excess Arguments":
                score -= 0.3
            else:
                score -= 0.4

    # File size penalty
    if total_lines > FILE_LINES_CRITICAL:
        score -= 1.0
    elif total_lines > FILE_LINES_WARNING:
        score -= 0.5

    return max(1.0, min(10.0, round(score, 1)))

# ── File analysis ───────────────────────────────────────────────────────────

def count_file_lines(filepath: str) -> int:
    """Count lines in a file."""
    try:
        with open(filepath, "r", encoding="utf-8", errors="replace") as f:
            return sum(1 for _ in f)
    except (OSError, IOError):
        return 0


def analyze_go_file(
    filepath: str,
    rel_path: str,
    cyclo_data: dict[str, int],
    cognit_data: dict[str, int],
) -> FileHealth:
    """Analyze a single Go file."""
    total_lines = count_file_lines(filepath)
    functions = parse_go_functions(filepath)

    # Merge complexity data from tools
    for func in functions:
        func.cc = cyclo_data.get(func.name, 0)
        func.cognitive = cognit_data.get(func.name, 0)
        # Use the higher of cyclomatic and cognitive as the effective complexity
        if func.cognitive > func.cc:
            func.cc = func.cognitive

    # Detect smells per function
    all_smells: list[CodeSmell] = []
    for func in functions:
        all_smells.extend(detect_smells(func))

    # Complex conditionals (file-level check)
    complex_conds = count_complex_conditionals_go(filepath)
    for line_num, ops in complex_conds:
        all_smells.append(CodeSmell(
            name="Complex Conditional",
            severity="warning",
            message=f"{ops} boolean operators in condition",
            function="",
            line=line_num,
            details={"operators": ops, "threshold": COMPLEX_COND_OPS},
        ))

    # File-level smell
    if total_lines > FILE_LINES_CRITICAL:
        all_smells.append(CodeSmell(
            name="Large File",
            severity="critical",
            message=f"{total_lines} lines (threshold: {FILE_LINES_CRITICAL})",
            function="(file)",
            line=0,
            details={"lines": total_lines, "threshold": FILE_LINES_CRITICAL},
        ))
    elif total_lines > FILE_LINES_WARNING:
        all_smells.append(CodeSmell(
            name="Large File",
            severity="warning",
            message=f"{total_lines} lines (threshold: {FILE_LINES_WARNING})",
            function="(file)",
            line=0,
            details={"lines": total_lines, "threshold": FILE_LINES_WARNING},
        ))

    score = calculate_health_score(all_smells, total_lines)

    return FileHealth(
        path=rel_path,
        score=score,
        smells=all_smells,
        metrics={
            "total_lines": total_lines,
            "function_count": len(functions),
            "max_cc": max((f.cc for f in functions), default=0),
            "max_nesting": max((f.max_nesting for f in functions), default=0),
            "max_func_lines": max((f.lines for f in functions), default=0),
        },
        func_count=len(functions),
        total_lines=total_lines,
    )


def analyze_ts_file(filepath: str, rel_path: str) -> FileHealth:
    """Analyze a single TypeScript/TSX file."""
    total_lines = count_file_lines(filepath)
    functions = parse_ts_functions(filepath)

    # TS doesn't have external CC tools readily available, so estimate from nesting
    for func in functions:
        # Rough CC estimate: nesting bumps * 2 + max_nesting
        func.cc = func.nesting_bumps + func.max_nesting

    all_smells: list[CodeSmell] = []
    for func in functions:
        all_smells.extend(detect_smells(func))

    # File-level smell
    if total_lines > FILE_LINES_CRITICAL:
        all_smells.append(CodeSmell(
            name="Large File",
            severity="critical",
            message=f"{total_lines} lines (threshold: {FILE_LINES_CRITICAL})",
            function="(file)",
            line=0,
            details={"lines": total_lines, "threshold": FILE_LINES_CRITICAL},
        ))
    elif total_lines > FILE_LINES_WARNING:
        all_smells.append(CodeSmell(
            name="Large File",
            severity="warning",
            message=f"{total_lines} lines (threshold: {FILE_LINES_WARNING})",
            function="(file)",
            line=0,
            details={"lines": total_lines, "threshold": FILE_LINES_WARNING},
        ))

    score = calculate_health_score(all_smells, total_lines)

    return FileHealth(
        path=rel_path,
        score=score,
        smells=all_smells,
        metrics={
            "total_lines": total_lines,
            "function_count": len(functions),
            "max_cc": max((f.cc for f in functions), default=0),
            "max_nesting": max((f.max_nesting for f in functions), default=0),
            "max_func_lines": max((f.lines for f in functions), default=0),
        },
        func_count=len(functions),
        total_lines=total_lines,
    )

# ── Output formatting ──────────────────────────────────────────────────────

def health_color(score: float) -> str:
    """Color a health score."""
    text = f"{score:.1f}/10"
    if score >= 8.0:
        return green(text)
    elif score >= 6.0:
        return yellow(text)
    else:
        return red(text)


def severity_icon(severity: str) -> str:
    """Get icon for severity."""
    if severity == "critical":
        return red("!!!")
    else:
        return yellow(" ! ")


def print_file_result(fh: FileHealth, verbose: bool = False) -> None:
    """Print analysis result for a single file."""
    if not fh.smells and not verbose:
        # Clean file, show briefly
        print(f"  {green('OK')}  {dim(fh.path):<60} Health: {health_color(fh.score)}")
        return

    # File with issues
    print(f"  {'  '  }  {bold(fh.path):<60} Health: {health_color(fh.score)}")
    for smell in fh.smells:
        icon = severity_icon(smell.severity)
        loc = f":{smell.line}" if smell.line > 0 else ""
        func = f" {smell.function}" if smell.function else ""
        print(f"      {icon} {bold(smell.name)}{func}{dim(loc)} — {smell.message}")


def print_summary(results: list[FileHealth], threshold: float) -> bool:
    """Print summary and return True if all files pass threshold."""
    if not results:
        print(f"  {dim('No source files to analyze.')}")
        return True

    total_smells = sum(len(fh.smells) for fh in results)
    critical_count = sum(1 for fh in results for s in fh.smells if s.severity == "critical")
    warning_count = total_smells - critical_count

    # Weighted average health (larger files carry more weight, like CodeScene)
    total_weight = sum(max(fh.total_lines, 1) for fh in results)
    avg_health = sum(fh.score * max(fh.total_lines, 1) for fh in results) / total_weight
    avg_health = round(avg_health, 1)

    files_below = [fh for fh in results if fh.score < threshold]

    print()
    print(f"  Files analyzed:   {bold(str(len(results)))}")
    print(f"  Average health:   {health_color(avg_health)} {dim('(weighted by file size)')}")
    if total_smells > 0:
        parts = []
        if critical_count:
            parts.append(red(f"{critical_count} critical"))
        if warning_count:
            parts.append(yellow(f"{warning_count} warning{'s' if warning_count != 1 else ''}"))
        print(f"  Issues found:     {', '.join(parts)}")
    else:
        print(f"  Issues found:     {green('none')}")

    if files_below:
        print()
        print(f"  {red('Files below health threshold')} ({threshold}/10):")
        for fh in sorted(files_below, key=lambda f: f.score):
            print(f"    {red('*')} {fh.path} — {health_color(fh.score)}")

    passed = len(files_below) == 0
    print()
    if passed:
        print(f"  {green('OK')} Code health above threshold ({threshold}/10)")
    else:
        print(f"  {red('FAIL')} {len(files_below)} file(s) below health threshold ({threshold}/10)")

    return passed


def to_json(results: list[FileHealth]) -> str:
    """Convert results to JSON."""
    data = []
    for fh in results:
        data.append({
            "path": fh.path,
            "score": fh.score,
            "metrics": fh.metrics,
            "smells": [
                {
                    "name": s.name,
                    "severity": s.severity,
                    "message": s.message,
                    "function": s.function,
                    "line": s.line,
                    "details": s.details,
                }
                for s in fh.smells
            ],
        })
    return json.dumps(data, indent=2)

# ── Main ────────────────────────────────────────────────────────────────────

def main() -> int:
    parser = argparse.ArgumentParser(
        description="CodeScene-style code health analysis",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--base", default="main",
                        help="Base branch for diff (default: main)")
    parser.add_argument("--all", action="store_true",
                        help="Analyze all source files, not just changed")
    parser.add_argument("--files", nargs="+",
                        help="Specific files to analyze")
    parser.add_argument("--threshold", type=float, default=6.0,
                        help="Minimum health score (default: 6.0)")
    parser.add_argument("--json", action="store_true",
                        help="Output as JSON")
    parser.add_argument("--verbose", action="store_true",
                        help="Show details for all files including clean ones")
    parser.add_argument("--skip-go", action="store_true",
                        help="Skip Go file analysis")
    parser.add_argument("--skip-ts", action="store_true",
                        help="Skip TypeScript file analysis")
    args = parser.parse_args()

    repo_root = find_repo_root()

    # Determine which files to analyze
    if args.files:
        all_files = args.files
    elif args.all:
        # Find all Go and TS files
        go_files_all = []
        ts_files_all = []
        for root, dirs, files in os.walk(repo_root):
            # Skip hidden dirs, vendor, node_modules
            dirs[:] = [d for d in dirs if not d.startswith('.') and d not in ('vendor', 'node_modules', '_old_code')]
            for f in files:
                rel = os.path.relpath(os.path.join(root, f), repo_root).replace("\\", "/")
                if f.endswith(".go") and not f.endswith("_test.go"):
                    go_files_all.append(rel)
                elif f.endswith((".ts", ".tsx")) and "node_modules" not in rel:
                    ts_files_all.append(rel)
        all_files = go_files_all + ts_files_all
    else:
        all_files = get_changed_files(repo_root, args.base)

    go_files, ts_files = filter_source_files(all_files)

    if args.skip_go:
        go_files = []
    if args.skip_ts:
        ts_files = []

    if not go_files and not ts_files:
        if not args.json:
            print(f"  {dim('No changed Go/TypeScript files to analyze.')}")
        else:
            print("[]")
        return 0

    if not args.json:
        go_count = len(go_files)
        ts_count = len(ts_files)
        parts = []
        if go_count:
            parts.append(f"{go_count} Go")
        if ts_count:
            parts.append(f"{ts_count} TypeScript")
        print(f"  Analyzing {' + '.join(parts)} changed file{'s' if go_count + ts_count != 1 else ''}...")
        print()

    # Install/find Go analysis tools
    gocyclo_path = None
    gocognit_path = None
    if go_files:
        gocyclo_path = find_go_tool("gocyclo", "github.com/fzipp/gocyclo/cmd/gocyclo@latest")
        gocognit_path = find_go_tool("gocognit", "github.com/uudashr/gocognit/cmd/gocognit@latest")
        if not gocyclo_path and not args.json:
            print(f"  {yellow('!')} gocyclo not available — complexity analysis will be limited")
        if not gocognit_path and not args.json:
            print(f"  {yellow('!')} gocognit not available — cognitive complexity analysis will be limited")

    # Run bulk complexity analysis on all Go files at once (much faster than per-file)
    cyclo_all = run_gocyclo(gocyclo_path, go_files, repo_root) if gocyclo_path else {}
    cognit_all = run_gocognit(gocognit_path, go_files, repo_root) if gocognit_path else {}

    # Analyze each file
    results: list[FileHealth] = []

    for rel_path in sorted(go_files):
        abs_path = os.path.join(repo_root, rel_path)
        if not os.path.isfile(abs_path):
            continue
        norm_path = rel_path.replace("\\", "/")
        cyclo_data = cyclo_all.get(norm_path, {})
        cognit_data = cognit_all.get(norm_path, {})
        fh = analyze_go_file(abs_path, norm_path, cyclo_data, cognit_data)
        results.append(fh)

    for rel_path in sorted(ts_files):
        abs_path = os.path.join(repo_root, rel_path)
        if not os.path.isfile(abs_path):
            continue
        norm_path = rel_path.replace("\\", "/")
        fh = analyze_ts_file(abs_path, norm_path)
        results.append(fh)

    # Output
    if args.json:
        print(to_json(results))
        return 0 if all(fh.score >= args.threshold for fh in results) else 1

    # Print per-file results (only files with issues, unless verbose)
    for fh in results:
        if fh.smells or args.verbose:
            print_file_result(fh, verbose=args.verbose)

    # Summary
    print()
    passed = print_summary(results, args.threshold)
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
