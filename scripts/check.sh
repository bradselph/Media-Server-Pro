#!/usr/bin/env bash
# Re-exec with bash if invoked as sh (e.g. "sh check.sh") so bash-isms work
[ -z "${BASH:-}" ] && exec bash "$0" "$@"
# =============================================================================
# check.sh — Unified entry point for all project checks and analysis
#
# Usage:
#   ./scripts/check.sh                  # default: pre-push checks
#   ./scripts/check.sh <command> [opts]  # run a specific check suite
#
# Commands:
#   push, pre-push       Pre-push CI checks (build, lint, test, code health)
#   analyze              Same checks + generate JSON/HTML reports in reports/
#   health, codescene    CodeScene-style code health only
#   fix                  AI-powered auto-fix (requires ANTHROPIC_API_KEY)
#   report [FILE]        Generate diagnostics report (Cursor-style)
#   version <type>       Bump version (major|minor|patch)
#   all                  Full checks + reports + all-files code health
#
# Flags (combine with any command):
#   --go-only            Only check Go code
#   --frontend-only      Only check frontend code
#   --fix                Auto-fix where possible
#   --fast               Skip slow checks (security, tests)
#   -h, --help           Show this help
#
# Examples:
#   ./scripts/check.sh                      # pre-push checks
#   ./scripts/check.sh push --fast          # skip tests + security
#   ./scripts/check.sh health               # code health on changed files
#   ./scripts/check.sh health --all         # code health on all files
#   ./scripts/check.sh analyze              # checks + JSON reports
#   ./scripts/check.sh fix --go-only        # AI-fix Go issues only
#   ./scripts/check.sh all                  # everything
#   ./scripts/check.sh version patch        # bump patch version
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"
# Run from repo root so dispatched scripts scan the codebase root
cd "$REPO_ROOT"

require_python() {
  if ! find_python; then
    echo -e "${RED}Python not found${RESET}" >&2
    return 1
  fi
}

# ── Help ─────────────────────────────────────────────────────────────────────
show_help() {
  sed -n '2,/^# =====/p' "$0" | sed 's/^# \{0,2\}//' | head -38
  echo ""
  echo -e "${BOLD}Scripts:${RESET}"
  echo -e "  ${CYAN}pre-push-check.sh${RESET}  CI checks (build, lint, test, security, code health)"
  echo -e "  ${CYAN}codescene-check.py${RESET}  CodeScene-style code health analysis"
  echo -e "  ${CYAN}fix-issues.py${RESET}       AI-powered auto-fix via Claude API"
  echo ""
}

# ── Flag translation ─────────────────────────────────────────────────────────
# Maps unified --go-only / --frontend-only to each script's native flags.

to_push_args() {
  for arg in "$@"; do
    case "$arg" in
      --go-only)       echo "--skip-frontend" ;;
      --frontend-only) echo "--skip-go" ;;
      *)               echo "$arg" ;;
    esac
  done
}

to_health_args() {
  for arg in "$@"; do
    case "$arg" in
      --go-only)       echo "--skip-ts" ;;
      --frontend-only) echo "--skip-go" ;;
      *)               echo "$arg" ;;
    esac
  done
}

to_fix_args() {
  for arg in "$@"; do
    case "$arg" in
      --frontend-only) echo "--ts-only" ;;
      *)               echo "$arg" ;;
    esac
  done
}

# ── Extract command + remaining args ─────────────────────────────────────────
COMMAND=""
REMAINING_ARGS=()

if [[ $# -eq 0 ]]; then
  COMMAND="push"
else
  for arg in "$@"; do
    if [[ -z "$COMMAND" && ! "$arg" =~ ^-- ]]; then
      COMMAND="$arg"
    else
      REMAINING_ARGS+=("$arg")
    fi
  done
  # All flags, no command → default to push
  if [[ -z "$COMMAND" ]]; then
    COMMAND="push"
    REMAINING_ARGS=("$@")
  fi
fi

# ── Dispatch ─────────────────────────────────────────────────────────────────
case "$COMMAND" in

  push|pre-push|ci)
    exec bash "${SCRIPT_DIR}/pre-push-check.sh" \
      $(to_push_args "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}")
    ;;

  analyze|analysis)
    # Same as push but also generates JSON/HTML reports in reports/
    exec bash "${SCRIPT_DIR}/pre-push-check.sh" --save-reports \
      $(to_push_args "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}")
    ;;

  health|codescene|code-health)
    require_python
    exec "$PY" "${SCRIPT_DIR}/codescene-check.py" \
      $(to_health_args "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}")
    ;;

  fix|auto-fix)
    require_python
    if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
      echo -e "${YELLOW}Warning: ANTHROPIC_API_KEY not set — fix-issues.py requires it${RESET}" >&2
    fi
    exec "$PY" "${SCRIPT_DIR}/fix-issues.py" \
      $(to_fix_args "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}")
    ;;

  report|diagnostics)
    require_python
    # Parse report-specific args
    local_file="issues-report.md"
    local_fmt="md"
    extra=()
    for arg in "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}"; do
      case "$arg" in
        *.html) local_file="$arg"; local_fmt="html" ;;
        *.json) local_file="$arg"; local_fmt="json" ;;
        *.md)   local_file="$arg"; local_fmt="md" ;;
        --html) local_file="issues-report.html"; local_fmt="html" ;;
        --json) local_file="issues-report.json"; local_fmt="json" ;;
        *)      extra+=("$arg") ;;
      esac
    done
    exec "$PY" "${SCRIPT_DIR}/fix-issues.py" \
      --report "$local_file" --report-format "$local_fmt" "${extra[@]}"
    ;;

  version|bump)
    exec bash "${SCRIPT_DIR}/pre-push-check.sh" \
      --bump-version "${REMAINING_ARGS[0]:-minor}"
    ;;

  all|full)
    exit_code=0

    echo -e "${BOLD}${CYAN}━━━ [1/2] Full CI Checks + Reports ━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    echo ""
    bash "${SCRIPT_DIR}/pre-push-check.sh" --save-reports \
      $(to_push_args "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}") || exit_code=1
    echo ""

    echo -e "${BOLD}${CYAN}━━━ [2/2] Code Health (all files) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    echo ""
    if find_python; then
      "$PY" "${SCRIPT_DIR}/codescene-check.py" --all \
        $(to_health_args "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}") || exit_code=1
    else
      echo -e "${YELLOW}Python not found — skipping all-files code health${RESET}"
    fi
    echo ""

    if [[ $exit_code -eq 0 ]]; then
      echo -e "  ${GREEN}${BOLD}All check suites passed!${RESET}"
    else
      echo -e "  ${RED}${BOLD}Some checks had failures — review the output above.${RESET}"
    fi
    exit $exit_code
    ;;

  help|-h|--help)
    show_help
    ;;

  *)
    echo -e "${RED}Unknown command: ${COMMAND}${RESET}" >&2
    echo "Run './scripts/check.sh help' for usage."
    exit 1
    ;;
esac
