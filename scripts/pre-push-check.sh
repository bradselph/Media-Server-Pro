#!/usr/bin/env bash
# =============================================================================
# pre-push-check.sh — Run all CI checks locally before pushing to GitHub
#
# Mirrors every job in .github/workflows/ci.yml so failures are caught before
# consuming GitHub Actions minutes.
#
# Usage:
#   ./scripts/pre-push-check.sh [OPTIONS]
#
# Options:
#   --install-hook      Install this script as a git pre-push hook
#   --skip-go           Skip all Go checks
#   --skip-frontend     Skip all frontend checks
#   --skip-security     Skip govulncheck + npm audit (slow/network)
#   --skip-tests        Skip go test + vitest
#   --fast              Alias for --skip-security --skip-tests
#   --fix               Auto-run 'go fmt' before checking
#   -h, --help          Show this help
# =============================================================================

set -euo pipefail

# ── Colours ──────────────────────────────────────────────────────────────────
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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

FAILED_STEPS=()
SKIPPED_STEPS=()
START_TIME=$(date +%s)

# ── Parse flags ───────────────────────────────────────────────────────────────
OPT_INSTALL_HOOK=false
OPT_SKIP_GO=false
OPT_SKIP_FRONTEND=false
OPT_SKIP_SECURITY=false
OPT_SKIP_TESTS=false
OPT_FIX=false

for arg in "$@"; do
  case "$arg" in
    --install-hook)   OPT_INSTALL_HOOK=true ;;
    --skip-go)        OPT_SKIP_GO=true ;;
    --skip-frontend)  OPT_SKIP_FRONTEND=true ;;
    --skip-security)  OPT_SKIP_SECURITY=true ;;
    --skip-tests)     OPT_SKIP_TESTS=true ;;
    --fast)           OPT_SKIP_SECURITY=true; OPT_SKIP_TESTS=true ;;
    --fix)            OPT_FIX=true ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,2\}//' | sed -n '2,18p'
      exit 0
      ;;
    *) echo -e "${RED}Unknown option: $arg${RESET}" >&2; exit 1 ;;
  esac
done

# ── Install git hook ──────────────────────────────────────────────────────────
if $OPT_INSTALL_HOOK; then
  HOOK_PATH="${REPO_ROOT}/.git/hooks/pre-push"
  cat > "$HOOK_PATH" <<'HOOK'
#!/usr/bin/env bash
exec "$(git rev-parse --show-toplevel)/scripts/pre-push-check.sh" "$@"
HOOK
  chmod +x "$HOOK_PATH"
  echo -e "${GREEN}${BOLD}Git pre-push hook installed at .git/hooks/pre-push${RESET}"
  echo -e "${DIM}Every 'git push' will now run this script automatically.${RESET}"
  echo -e "${DIM}To bypass once:  git push --no-verify${RESET}"
  exit 0
fi

# ── Banner ────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${CYAN}╔══════════════════════════════════════════════════╗${RESET}"
echo -e "${BOLD}${CYAN}║       Media Server Pro — Pre-Push CI Check       ║${RESET}"
echo -e "${BOLD}${CYAN}╚══════════════════════════════════════════════════╝${RESET}"
BRANCH=$(git -C "$REPO_ROOT" branch --show-current 2>/dev/null || echo "unknown")
echo -e "${DIM}Repo:   ${REPO_ROOT}${RESET}"
echo -e "${DIM}Branch: ${BRANCH}${RESET}"
echo ""

# ── Helpers ───────────────────────────────────────────────────────────────────
section() {
  local title="$1"
  local line
  line=$(printf '─%.0s' $(seq 1 50))
  echo -e "${BOLD}${CYAN}── ${title} ${line:${#title}}${RESET}"
}

# skip_if <bool1> [bool2...]  — echoes "true" if ANY arg is "true"
skip_if() {
  for v in "$@"; do
    [[ "$v" == "true" ]] && echo "true" && return
  done
  echo "false"
}

# run_step <label> <skip_bool> <cmd...>
run_step() {
  local label="$1"
  local skip="$2"
  shift 2

  if [[ "$skip" == "true" ]]; then
    echo -e "  ${SKIP_MARK} ${DIM}${label} (skipped)${RESET}"
    SKIPPED_STEPS+=("$label")
    return 0
  fi

  printf "  ${CYAN}▶${RESET} %-48s" "$label"
  local t0
  t0=$(date +%s)
  local out
  local rc=0
  out=$("$@" 2>&1) || rc=$?
  local elapsed=$(( $(date +%s) - t0 ))

  if [[ $rc -eq 0 ]]; then
    echo -e "${PASS} ${DIM}${elapsed}s${RESET}"
  else
    echo -e "${FAIL} ${DIM}${elapsed}s${RESET}"
    echo ""
    echo -e "${RED}─── Output ──────────────────────────────────────────────${RESET}"
    echo "$out" | head -80
    echo -e "${RED}─────────────────────────────────────────────────────────${RESET}"
    echo ""
    FAILED_STEPS+=("$label")
  fi
}

# ── Go checks ─────────────────────────────────────────────────────────────────
section "Go"
cd "$REPO_ROOT"

if $OPT_FIX && ! $OPT_SKIP_GO; then
  echo -e "  ${CYAN}▶${RESET} Auto-formatting with go fmt..."
  go fmt ./... 2>&1 | sed 's/^/    /'
  echo ""
fi

run_step "go mod download"        "$OPT_SKIP_GO"  go mod download
run_step "go build (linux/amd64)" "$OPT_SKIP_GO"  \
         env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
run_step "go vet"                 "$OPT_SKIP_GO"  go vet ./...
run_step "go test -race"          "$(skip_if "$OPT_SKIP_GO" "$OPT_SKIP_TESTS")" \
         go test -race -timeout 120s ./...

# govulncheck: auto-install if missing
SKIP_VULN="$(skip_if "$OPT_SKIP_GO" "$OPT_SKIP_SECURITY")"
if [[ "$SKIP_VULN" == "false" ]] && ! command -v govulncheck &>/dev/null; then
  printf "  ${CYAN}▶${RESET} %-48s" "install govulncheck"
  if go install golang.org/x/vuln/cmd/govulncheck@latest 2>/dev/null; then
    echo -e "${PASS} ${DIM}installed${RESET}"
  else
    echo -e "${WARN} ${DIM}install failed — skipping govulncheck${RESET}"
    SKIP_VULN=true
  fi
fi
run_step "govulncheck" "$SKIP_VULN" govulncheck ./...

echo ""

# ── Frontend checks ───────────────────────────────────────────────────────────
section "Frontend"
FRONTEND_DIR="${REPO_ROOT}/web/frontend"

if ! $OPT_SKIP_FRONTEND; then
  if [[ ! -d "$FRONTEND_DIR" ]]; then
    echo -e "  ${WARN} ${FRONTEND_DIR} not found — skipping all frontend checks"
    OPT_SKIP_FRONTEND=true
  elif ! command -v node &>/dev/null; then
    echo -e "  ${WARN} node not in PATH — skipping all frontend checks"
    OPT_SKIP_FRONTEND=true
  fi
fi

# npm ci (needed by all subsequent steps)
if ! $OPT_SKIP_FRONTEND; then
  printf "  ${CYAN}▶${RESET} %-48s" "npm ci"
  t0=$(date +%s)
  npm_out=$(cd "$FRONTEND_DIR" && npm ci 2>&1) && npm_rc=0 || npm_rc=$?
  elapsed=$(( $(date +%s) - t0 ))
  if [[ $npm_rc -eq 0 ]]; then
    echo -e "${PASS} ${DIM}${elapsed}s${RESET}"
  else
    echo -e "${FAIL} ${DIM}${elapsed}s${RESET}"
    echo "$npm_out" | head -40
    FAILED_STEPS+=("npm ci")
    OPT_SKIP_FRONTEND=true   # nothing else will work
  fi
fi

# Helper: run a command inside FRONTEND_DIR
fe_step() {
  local label="$1"
  local skip="$2"
  shift 2
  run_step "$label" "$skip" bash -c "cd '${FRONTEND_DIR}' && $*"
}

fe_step "npm run lint"       "$OPT_SKIP_FRONTEND" "npm run lint"
fe_step "npm run build"      "$OPT_SKIP_FRONTEND" "npm run build"

# vitest — mirror CI: warn instead of fail when no test files exist
SKIP_VITEST="$(skip_if "$OPT_SKIP_FRONTEND" "$OPT_SKIP_TESTS")"
if [[ "$SKIP_VITEST" == "false" ]]; then
  printf "  ${CYAN}▶${RESET} %-48s" "vitest run"
  t0=$(date +%s)
  vtest_out=$(cd "$FRONTEND_DIR" && npx vitest run 2>&1) && vtest_rc=0 || vtest_rc=$?
  elapsed=$(( $(date +%s) - t0 ))
  if [[ $vtest_rc -eq 0 ]]; then
    echo -e "${PASS} ${DIM}${elapsed}s${RESET}"
  elif echo "$vtest_out" | grep -qiE "no test files|no tests"; then
    echo -e "${WARN} ${DIM}${elapsed}s — no test files configured${RESET}"
  else
    echo -e "${FAIL} ${DIM}${elapsed}s${RESET}"
    echo ""
    echo "$vtest_out" | head -50
    echo ""
    FAILED_STEPS+=("vitest run")
  fi
else
  echo -e "  ${SKIP_MARK} ${DIM}vitest run (skipped)${RESET}"
  SKIPPED_STEPS+=("vitest run")
fi

SKIP_AUDIT="$(skip_if "$OPT_SKIP_FRONTEND" "$OPT_SKIP_SECURITY")"
fe_step "npm audit (high+)"  "$SKIP_AUDIT" "npm audit --audit-level=high"

echo ""

# ── Dev version label (mirrors dev-version.yml) ───────────────────────────────
section "Version Info"
CURRENT_VER=$(tr -d '[:space:]' < "${REPO_ROOT}/VERSION" 2>/dev/null || echo "unknown")
SHORT_SHA=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")

if [[ "$BRANCH" == "development" ]]; then
  DEV_LABEL="${CURRENT_VER}-dev.${SHORT_SHA}"
  echo -e "  ${PASS} Dev build label: ${BOLD}${DEV_LABEL}${RESET}"
  echo -e "  ${DIM}(This is what dev-version.yml would tag this commit as)${RESET}"
elif [[ "$BRANCH" == "main" ]]; then
  echo -e "  ${DIM}On main — release-version.yml will bump VERSION from ${CURRENT_VER}${RESET}"
  # Show what the bump would be based on last commit message
  LAST_MSG=$(git -C "$REPO_ROOT" log -1 --pretty=%s 2>/dev/null || echo "")
  if echo "$LAST_MSG" | grep -qiE '(BREAKING CHANGE:|^major:)'; then
    IFS='.' read -r MAJ MIN PAT <<< "$CURRENT_VER"
    echo -e "  ${WARN} BREAKING CHANGE detected — would bump MAJOR: ${CURRENT_VER} → $((MAJ+1)).0.0"
  else
    IFS='.' read -r MAJ MIN PAT <<< "$CURRENT_VER"
    echo -e "  ${DIM}Would bump minor: ${CURRENT_VER} → ${MAJ}.$((MIN+1)).0${RESET}"
  fi
else
  echo -e "  ${DIM}Branch '${BRANCH}' — version workflows only run on main/development${RESET}"
fi

echo ""

# ── Summary ───────────────────────────────────────────────────────────────────
TOTAL=$(( $(date +%s) - START_TIME ))
section "Summary"

if [[ ${#SKIPPED_STEPS[@]} -gt 0 ]]; then
  echo -e "  ${SKIP_MARK}  Skipped (${#SKIPPED_STEPS[@]}): ${DIM}${SKIPPED_STEPS[*]}${RESET}"
fi

if [[ ${#FAILED_STEPS[@]} -eq 0 ]]; then
  echo -e "  ${PASS}  ${GREEN}${BOLD}All checks passed${RESET} ${DIM}(${TOTAL}s total)${RESET}"
  echo ""
  echo -e "  ${DIM}Safe to push:  git push${RESET}"
  echo ""
  exit 0
else
  echo -e "  ${FAIL}  ${RED}${BOLD}${#FAILED_STEPS[@]} check(s) failed${RESET} ${DIM}(${TOTAL}s total)${RESET}${RED}:${RESET}"
  for s in "${FAILED_STEPS[@]}"; do
    echo -e "       ${RED}•${RESET} $s"
  done
  echo ""
  echo -e "  ${RED}Fix the issues above before pushing to avoid wasting GitHub Actions minutes.${RESET}"
  echo ""
  exit 1
fi
