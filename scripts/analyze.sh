#!/usr/bin/env bash
# Re-exec with bash if invoked as sh so bash-isms work
[ -z "${BASH:-}" ] && exec bash "$0" "$@"
# =============================================================================
# analyze.sh — Full code analysis (Go + Frontend). Thin wrapper around
# pre-push-check.sh to avoid duplicating CI logic.
#
# Usage:
#   ./scripts/analyze.sh              # run all checks, console output
#   ./scripts/analyze.sh --report     # also generate JSON reports in reports/
#   ./scripts/analyze.sh --go         # Go analysis only
#   ./scripts/analyze.sh --frontend   # Frontend analysis only
#   ./scripts/analyze.sh --fix        # auto-fix where possible
#
# Equivalent to: ./scripts/check.sh analyze [flags]
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"
# Run from repo root so pre-push-check scans the codebase root
cd "$REPO_ROOT"

ARGS=()

for arg in "$@"; do
  case "$arg" in
    --report)    ARGS+=(--save-reports) ;;
    --go)        ARGS+=(--skip-frontend) ;;
    --frontend)  ARGS+=(--skip-go) ;;
    --fix)       ARGS+=(--fix) ;;
    --help|-h)
      echo "Usage: $0 [--report] [--go] [--frontend] [--fix]"
      echo ""
      echo "  --report     Generate JSON reports in reports/"
      echo "  --go         Run Go analysis only"
      echo "  --frontend   Run frontend analysis only"
      echo "  --fix        Auto-fix issues where possible"
      echo ""
      echo "Runs pre-push-check.sh with the same checks (build, lint, test, code health)."
      exit 0
      ;;
    *)           ARGS+=("$arg") ;;
  esac
done

exec bash "${SCRIPT_DIR}/pre-push-check.sh" "${ARGS[@]+"${ARGS[@]}"}"
