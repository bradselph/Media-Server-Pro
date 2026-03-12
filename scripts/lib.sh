#!/usr/bin/env bash
# =============================================================================
# lib.sh — Shared helpers for check scripts (sourced by check.sh, pre-push-check.sh, analyze.sh)
# Do not run directly.
# =============================================================================

# ── Colours ─────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
RESET='\033[0m'

PASS="${GREEN}✔${RESET}"
FAIL="${RED}✘${RESET}"
SKIP_MARK="${YELLOW}⊘${RESET}"
WARN="${YELLOW}⚠${RESET}"

# ── Script / repo paths (set by caller or when sourced) ─────────────────────
# SCRIPT_DIR = directory containing this script (or the script that sourced us).
# REPO_ROOT  = git toplevel if available, else directory above SCRIPT_DIR.
if [[ -z "${SCRIPT_DIR:-}" ]]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
fi
if [[ -z "${REPO_ROOT:-}" ]]; then
  REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
  if command -v git &>/dev/null; then
    _gr=$(git -C "$REPO_ROOT" rev-parse --show-toplevel 2>/dev/null) && REPO_ROOT="$_gr"
  fi
fi

# ── find_python ─────────────────────────────────────────────────────────────
# Sets PY to python3 or python if available. Returns 0 if found, 1 otherwise.
find_python() {
  PY=""
  command -v python3 &>/dev/null && PY=python3
  [[ -z "$PY" ]] && command -v python &>/dev/null && PY=python
  [[ -n "$PY" ]]
}
