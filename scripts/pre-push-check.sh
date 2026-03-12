#!/usr/bin/env bash
# Re-exec with bash if invoked as sh (e.g. "sh pre-push-check.sh") so bash-isms work
[ -z "${BASH:-}" ] && exec bash "$0" "$@"
# =============================================================================
# pre-push-check.sh — Run all CI checks locally before pushing to GitHub
#
# Mirrors every job in .github/workflows/ci.yml so failures are caught before
# consuming GitHub Actions minutes. Also applies version bumps (like release-
# and dev-version.yml) and can run fix-issues.py for diagnostics.
#
# Usage:
#   ./scripts/pre-push-check.sh [OPTIONS]
#
# Options:
#   --install-hook         Install this script as a git pre-push hook
#   --bump-version TYPE    Bump version (major|minor|patch) and update files
#   --sync-version         Sync cmd/server/main.go from VERSION (no bump)
#   --bump-dev             On development: stamp main.go with -dev.SHA label
#   --skip-go              Skip all Go checks
#   --skip-frontend        Skip all frontend checks
#   --skip-codescene       Skip CodeScene-style code health analysis
#   --skip-security        Skip govulncheck + npm audit (slow/network)
#   --skip-tests           Skip go test + vitest
#   --fast                 Alias for --skip-security --skip-tests
#   --fix                  Auto-run 'go fmt' before checking
#   --fix-issues           Run fix-issues.py --dry-run to report Go/TS issues
#   --report [FILE]        Run fix-issues.py --report (Cursor-style diagnostics)
#   --report-format FMT    Report format: md (default) | html | json
#   --staticcheck          Include staticcheck in Go diagnostics (--report mode)
#   --lint                 Include ESLint in TypeScript diagnostics (--report mode)
#   --all-sources          Enable all diagnostic sources (--report mode)
#   --save-reports         Generate JSON/HTML reports in reports/ after checks
#   -h, --help             Show this help
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MAIN_GO="${REPO_ROOT}/cmd/server/main.go"
VERSION_FILE="${REPO_ROOT}/VERSION"
CHANGELOG_FILE="${REPO_ROOT}/CHANGELOG.md"

FAILED_STEPS=()
SKIPPED_STEPS=()
START_TIME=$(date +%s)

# ── Parse flags ───────────────────────────────────────────────────────────────
OPT_INSTALL_HOOK=false
OPT_SKIP_GO=false
OPT_SKIP_FRONTEND=false
OPT_SKIP_CODESCENE=false
OPT_SKIP_SECURITY=false
OPT_SKIP_TESTS=false
OPT_FIX=false
OPT_FIX_ISSUES=false
OPT_REPORT=""
OPT_REPORT_FORMAT="md"
OPT_STATICCHECK=false
OPT_LINT=false
OPT_ALL_SOURCES=false
OPT_SAVE_REPORTS=false
OPT_BUMP_VERSION=""
OPT_SYNC_VERSION=false
OPT_BUMP_DEV=false

while [[ $# -gt 0 ]]; do
  arg="$1"
  shift
  case "$arg" in
    --install-hook)   OPT_INSTALL_HOOK=true ;;
    --bump-version=*) OPT_BUMP_VERSION="${arg#*=}" ;;
    --bump-version)
      if [[ $# -gt 0 && "$1" =~ ^(major|minor|patch)$ ]]; then
        OPT_BUMP_VERSION="$1"; shift
      else
        OPT_BUMP_VERSION="minor"
      fi ;;
    --bump-dev)       OPT_BUMP_DEV=true ;;
    --sync-version)   OPT_SYNC_VERSION=true ;;
    --skip-go)        OPT_SKIP_GO=true ;;
    --skip-frontend)  OPT_SKIP_FRONTEND=true ;;
    --skip-codescene) OPT_SKIP_CODESCENE=true ;;
    --skip-security)  OPT_SKIP_SECURITY=true ;;
    --skip-tests)     OPT_SKIP_TESTS=true ;;
    --fast)           OPT_SKIP_SECURITY=true; OPT_SKIP_TESTS=true ;;
    --fix)            OPT_FIX=true ;;
    --fix-issues)     OPT_FIX_ISSUES=true ;;
    --report=*)       OPT_REPORT="${arg#*=}" ;;
    --report)
      if [[ $# -gt 0 && ! "$1" =~ ^-- ]]; then
        OPT_REPORT="$1"; shift
      else
        OPT_REPORT="issues-report.md"
      fi ;;
    --report-format=*) OPT_REPORT_FORMAT="${arg#*=}" ;;
    --report-format)
      if [[ $# -gt 0 ]]; then OPT_REPORT_FORMAT="$1"; shift; fi ;;
    --staticcheck)    OPT_STATICCHECK=true ;;
    --lint)           OPT_LINT=true ;;
    --all-sources)    OPT_ALL_SOURCES=true ;;
    --save-reports)   OPT_SAVE_REPORTS=true ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,2\}//' | sed -n '2,30p'
      exit 0
      ;;
    *) echo -e "${RED}Unknown option: $arg${RESET}" >&2; exit 1 ;;
  esac
done

# ── Version bump / sync (modeled on release-version.yml, dev-version.yml) ─────
# In-place replace Version = "..." in main.go (portable: sed, then Python fallback)
update_main_go_version() {
  local new_ver="$1"
  local re='Version   = "[^"]*"'
  local repl="Version   = \"${new_ver}\""

  if [[ ! -f "$MAIN_GO" ]]; then return 1; fi

  if command -v sed &>/dev/null; then
    if sed -i.bak "s/${re}/${repl}/" "$MAIN_GO" 2>/dev/null; then
      rm -f "${MAIN_GO}.bak" 2>/dev/null
      return 0
    fi
    if sed -i '' "s/${re}/${repl}/" "$MAIN_GO" 2>/dev/null; then
      return 0
    fi
  fi
  if command -v python3 &>/dev/null || command -v python &>/dev/null; then
    local py=python3; command -v python3 &>/dev/null || py=python
    if $py - "$MAIN_GO" "$new_ver" <<'PYEOF' 2>/dev/null; then
import re, sys
path, newv = sys.argv[1], sys.argv[2]
with open(path, 'r', encoding='utf-8') as f: c = f.read()
with open(path, 'w', encoding='utf-8') as f: f.write(re.sub(r'Version   = "[^"]*"', 'Version   = "' + newv + '"', c))
PYEOF
      return 0
    fi
  fi
  return 1
}

apply_version_bump() {
  local bump_type="${1:-minor}"
  local current new_v maj min pat

  if [[ ! -f "$VERSION_FILE" ]]; then
    echo -e "${RED}VERSION file not found${RESET}" >&2
    return 1
  fi
  current=$(tr -d '[:space:]' < "$VERSION_FILE")
  IFS='.' read -r maj min pat <<< "$current"

  case "$bump_type" in
    major) maj=$((maj + 1)); min=0; pat=0 ;;
    minor) min=$((min + 1)); pat=0 ;;
    patch) pat=$((pat + 1)) ;;
    *) echo -e "${RED}Invalid bump type: $bump_type (use major|minor|patch)${RESET}" >&2; return 1 ;;
  esac

  new_v="${maj}.${min}.${pat}"
  echo "$new_v" > "$VERSION_FILE"
  echo -e "  ${PASS} VERSION: ${current} → ${BOLD}${new_v}${RESET}"

  if update_main_go_version "$new_v"; then
    echo -e "  ${PASS} cmd/server/main.go updated"
  fi

  # Update CHANGELOG (like release-version.yml)
  local date_iso commits prev_tag new_entry heading rest
  date_iso=$(date +%Y-%m-%d)
  prev_tag=$(git -C "$REPO_ROOT" describe --tags --abbrev=0 2>/dev/null || echo "")
  if [[ -n "$prev_tag" ]]; then
    commits=$(git -C "$REPO_ROOT" log --pretty=format:"- %s" "${prev_tag}..HEAD" 2>/dev/null | grep -v '\[auto-version\]' || true)
  else
    commits=$(git -C "$REPO_ROOT" log --pretty=format:"- %s" -20 2>/dev/null | grep -v '\[auto-version\]' || true)
  fi
  [[ -z "$commits" ]] && commits="- (no commits)"

  new_entry="## [${new_v}] - ${date_iso} (${bump_type})"$'\n\n'"${commits}"$'\n\n'
  if [[ -f "$CHANGELOG_FILE" ]]; then
    heading=$(head -1 "$CHANGELOG_FILE")
    rest=$(tail -n +2 "$CHANGELOG_FILE")
    printf '%s\n\n%s%s' "$heading" "$new_entry" "$rest" > "$CHANGELOG_FILE"
  else
    printf '# Changelog\n\n%s' "$new_entry" > "$CHANGELOG_FILE"
  fi
  echo -e "  ${PASS} CHANGELOG.md prepended"
  echo ""
  echo -e "  ${DIM}Commit with: git add VERSION cmd/server/main.go CHANGELOG.md && git commit -m \"chore: release ${new_v} (${bump_type} bump)\"${RESET}"
  echo ""
}

sync_version_from_file() {
  local v
  if [[ ! -f "$VERSION_FILE" ]]; then
    echo -e "${RED}VERSION file not found${RESET}" >&2
    return 1
  fi
  v=$(tr -d '[:space:]' < "$VERSION_FILE")
  if update_main_go_version "$v"; then
    echo -e "${PASS} Synced main.go to VERSION: ${v}"
    return 0
  fi
  echo -e "${RED}Failed to update main.go${RESET}" >&2
  return 1
}

apply_dev_label() {
  local current short_sha dev_label
  current=$(tr -d '[:space:]' < "$VERSION_FILE" 2>/dev/null || echo "0.0.0")
  short_sha=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
  dev_label="${current}-dev.${short_sha}"

  if update_main_go_version "$dev_label"; then
    echo -e "${PASS} Stamped main.go with dev label: ${BOLD}${dev_label}${RESET}"
    return 0
  fi
  echo -e "${RED}Failed to update main.go${RESET}" >&2
  return 1
}

if [[ -n "$OPT_BUMP_VERSION" ]]; then
  echo ""
  echo -e "${BOLD}${CYAN}── Version bump (release-version.yml) ────────────────────────${RESET}"
  cd "$REPO_ROOT"
  apply_version_bump "$OPT_BUMP_VERSION"
  exit 0
fi

if $OPT_SYNC_VERSION; then
  echo ""
  echo -e "${BOLD}${CYAN}── Sync version ─────────────────────────────────────────────${RESET}"
  cd "$REPO_ROOT"
  sync_version_from_file
  exit 0
fi

if $OPT_BUMP_DEV; then
  echo ""
  echo -e "${BOLD}${CYAN}── Dev label (dev-version.yml) ──────────────────────────────${RESET}"
  cd "$REPO_ROOT"
  apply_dev_label
  exit 0
fi

# ── fix-issues.py dry-run (report Go + TS issues by file) ─────────────────────
if $OPT_FIX_ISSUES; then
  echo ""
  echo -e "${BOLD}${CYAN}── fix-issues.py (dry-run) ───────────────────────────────────${RESET}"
  cd "$REPO_ROOT"
  FIX_SCRIPT="${SCRIPT_DIR}/fix-issues.py"
  if [[ -f "$FIX_SCRIPT" ]]; then
    if find_python; then
      "$PY" "$FIX_SCRIPT" --dry-run
    else
      echo -e "${RED}Python not found — cannot run fix-issues.py${RESET}" >&2
      exit 1
    fi
  else
    echo -e "${RED}fix-issues.py not found at ${FIX_SCRIPT}${RESET}" >&2
    exit 1
  fi
  exit 0
fi

# ── fix-issues.py --report (Cursor IDE Problems panel report) ─────────────────
if [[ -n "$OPT_REPORT" ]]; then
  echo ""
  echo -e "${BOLD}${CYAN}── fix-issues.py --report (Cursor diagnostics) ──────────────${RESET}"
  cd "$REPO_ROOT"
  FIX_SCRIPT="${SCRIPT_DIR}/fix-issues.py"
  if [[ ! -f "$FIX_SCRIPT" ]]; then
    echo -e "${RED}fix-issues.py not found at ${FIX_SCRIPT}${RESET}" >&2
    exit 1
  fi
  if ! find_python; then
    echo -e "${RED}Python not found — cannot run fix-issues.py${RESET}" >&2
    exit 1
  fi
  REPORT_ARGS=("--report" "$OPT_REPORT" "--report-format" "$OPT_REPORT_FORMAT")
  $OPT_STATICCHECK  && REPORT_ARGS+=("--staticcheck")
  $OPT_LINT         && REPORT_ARGS+=("--lint")
  $OPT_ALL_SOURCES  && REPORT_ARGS+=("--all-sources")
  $OPT_SKIP_GO      && REPORT_ARGS+=("--ts-only")
  $OPT_SKIP_FRONTEND && REPORT_ARGS+=("--go-only")
  "$PY" "$FIX_SCRIPT" "${REPORT_ARGS[@]}"
  echo ""
  echo -e "${PASS}  Report: ${BOLD}${OPT_REPORT}${RESET}"
  exit 0
fi

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

# ── Run from repo root so all scans and paths are from codebase root ─────────
cd "$REPO_ROOT"

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

if $OPT_FIX && ! $OPT_SKIP_GO; then
  echo -e "  ${CYAN}▶${RESET} Auto-formatting with go fmt + goimports..."
  go fmt ./... 2>&1 | sed 's/^/    /'
  if ! command -v goimports &>/dev/null; then
    go install golang.org/x/tools/cmd/goimports@latest 2>/dev/null || true
  fi
  if command -v goimports &>/dev/null; then
    goimports -w . 2>&1 | sed 's/^/    /'
  fi
  echo ""
fi

run_step "go mod download"        "$OPT_SKIP_GO"  go mod download
run_step "go build [linux/amd64]" "$OPT_SKIP_GO" \
  env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
run_step "go vet"                 "$OPT_SKIP_GO"  go vet ./...

# golangci-lint: auto-install if missing
SKIP_LINT="$OPT_SKIP_GO"
if [[ "$SKIP_LINT" == "false" ]] && ! command -v golangci-lint &>/dev/null; then
  printf "  ${CYAN}▶${RESET} %-48s" "install golangci-lint"
  if go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest 2>/dev/null; then
    echo -e "${PASS} ${DIM}installed${RESET}"
  else
    echo -e "${WARN} ${DIM}install failed — skipping golangci-lint${RESET}"
    SKIP_LINT=true
  fi
fi
if $OPT_FIX; then
  run_step "golangci-lint (--fix)" "$SKIP_LINT"  golangci-lint run --fix ./...
else
  run_step "golangci-lint"          "$SKIP_LINT"  golangci-lint run ./...
fi

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

if $OPT_FIX; then
  fe_step "npm run lint (--fix)" "$OPT_SKIP_FRONTEND" "npx eslint --fix ."
else
  fe_step "npm run lint"         "$OPT_SKIP_FRONTEND" "npm run lint"
fi
# tsc --noEmit (from fix-issues.py — catches TS errors without full build)
SKIP_TSC="$(skip_if "$OPT_SKIP_FRONTEND")"
if [[ "$SKIP_TSC" == "false" ]]; then
  printf "  ${CYAN}▶${RESET} %-48s" "tsc --noEmit"
  t0=$(date +%s)
  tsc_out=$(cd "$FRONTEND_DIR" && npx tsc --noEmit --pretty false 2>&1) && tsc_rc=0 || tsc_rc=$?
  elapsed=$(( $(date +%s) - t0 ))
  if [[ $tsc_rc -eq 0 ]]; then
    echo -e "${PASS} ${DIM}${elapsed}s${RESET}"
  else
    echo -e "${FAIL} ${DIM}${elapsed}s${RESET}"
    echo ""
    echo -e "${RED}─── TypeScript errors (file:line) ─────────────────────────────${RESET}"
    echo "$tsc_out" | grep -E '\.tsx?\([0-9]+,' | head -30 | sed 's/^/    /'
    echo -e "${RED}─────────────────────────────────────────────────────────────${RESET}"
    echo ""
    FAILED_STEPS+=("tsc --noEmit")
  fi
fi
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

# ── Code Health (CodeScene-style analysis) ─────────────────────────────────────
section "Code Health (CodeScene-style)"

SKIP_CODESCENE="$OPT_SKIP_CODESCENE"
if [[ "$SKIP_CODESCENE" == "false" ]]; then
  CODESCENE_SCRIPT="${SCRIPT_DIR}/codescene-check.py"
  if [[ ! -f "$CODESCENE_SCRIPT" ]]; then
    echo -e "  ${WARN} codescene-check.py not found — skipping code health analysis"
    SKIP_CODESCENE=true
  fi
fi

if [[ "$SKIP_CODESCENE" == "false" ]]; then
  if ! find_python; then
    echo -e "  ${WARN} Python not found — skipping code health analysis"
    SKIP_CODESCENE=true
  fi
fi

if [[ "$SKIP_CODESCENE" == "false" ]]; then
  # Build args: pass through skip flags + detect base branch
  CS_ARGS=()
  if [[ "$BRANCH" == "development" || "$BRANCH" == "main" ]]; then
    CS_ARGS+=("--base" "main")
  else
    CS_ARGS+=("--base" "development")
  fi
  $OPT_SKIP_GO       && CS_ARGS+=("--skip-go")
  $OPT_SKIP_FRONTEND && CS_ARGS+=("--skip-ts")

  printf "  ${CYAN}▶${RESET} %-48s" "CodeScene code health analysis"
  t0=$(date +%s)
  cs_out=$("$PY" "$CODESCENE_SCRIPT" "${CS_ARGS[@]}" 2>&1) && cs_rc=0 || cs_rc=$?
  elapsed=$(( $(date +%s) - t0 ))

  if [[ $cs_rc -eq 0 ]]; then
    echo -e "${PASS} ${DIM}${elapsed}s${RESET}"
    # Show analysis output (indented)
    if [[ -n "$cs_out" ]]; then
      echo ""
      echo "$cs_out"
      echo ""
    fi
  else
    echo -e "${FAIL} ${DIM}${elapsed}s${RESET}"
    echo ""
    echo -e "${RED}─── Code Health Issues ──────────────────────────────────────────${RESET}"
    echo "$cs_out"
    echo -e "${RED}─────────────────────────────────────────────────────────────────${RESET}"
    echo ""
    FAILED_STEPS+=("CodeScene code health")
  fi
else
  echo -e "  ${SKIP_MARK} ${DIM}CodeScene code health analysis (skipped)${RESET}"
  SKIPPED_STEPS+=("CodeScene code health")
fi

echo ""

# ── Version Info (mirrors dev-version.yml / release-version.yml) ───────────────
section "Version Info"
CURRENT_VER=$(tr -d '[:space:]' < "${REPO_ROOT}/VERSION" 2>/dev/null || echo "unknown")
SHORT_SHA=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
MAIN_GO_VER=$(grep -oE 'Version\s+=\s+"[^"]+"' "${REPO_ROOT}/cmd/server/main.go" 2>/dev/null | sed 's/.*"\([^"]*\)".*/\1/' || echo "")

# Warn if VERSION and main.go are out of sync
if [[ -n "$MAIN_GO_VER" && "$MAIN_GO_VER" != "$CURRENT_VER" && "$MAIN_GO_VER" != *"-dev."* ]]; then
  echo -e "  ${WARN} VERSION (${CURRENT_VER}) ≠ main.go (${MAIN_GO_VER}) — run ${BOLD}--sync-version${RESET} to fix"
fi

if [[ "$BRANCH" == "development" ]]; then
  DEV_LABEL="${CURRENT_VER}-dev.${SHORT_SHA}"
  echo -e "  ${PASS} Dev build label: ${BOLD}${DEV_LABEL}${RESET}"
  echo -e "  ${DIM}Apply locally: ${BOLD}--bump-dev${RESET}${RESET}"
elif [[ "$BRANCH" == "main" ]]; then
  echo -e "  ${DIM}On main — apply bump: ${BOLD}--bump-version=minor${RESET}${DIM} (or major|patch)${RESET}"
  LAST_MSG=$(git -C "$REPO_ROOT" log -1 --pretty=%s 2>/dev/null || echo "")
  if echo "$LAST_MSG" | grep -qiE '(BREAKING CHANGE:|^major:)'; then
    IFS='.' read -r MAJ MIN PAT <<< "$CURRENT_VER"
    echo -e "  ${WARN} BREAKING CHANGE detected — use ${BOLD}--bump-version=major${RESET} → $((MAJ+1)).0.0"
  else
    IFS='.' read -r MAJ MIN PAT <<< "$CURRENT_VER"
    echo -e "  ${DIM}Minor bump would be: ${CURRENT_VER} → ${MAJ}.$((MIN+1)).0${RESET}"
  fi
else
  echo -e "  ${DIM}Branch '${BRANCH}' — version workflows run on main/development${RESET}"
fi

echo ""

# ── Generate Reports (--save-reports) ─────────────────────────────────────────
if $OPT_SAVE_REPORTS; then
  section "Reports"
  mkdir -p "${REPO_ROOT}/reports"

  # golangci-lint → JSON + HTML
  if ! $OPT_SKIP_GO && command -v golangci-lint &>/dev/null; then
    printf "  ${CYAN}▶${RESET} %-48s" "golangci-lint → reports/"
    golangci-lint run \
      --output.json.path "${REPO_ROOT}/reports/go-lint.json" \
      --output.html.path "${REPO_ROOT}/reports/go-lint.html" \
      --output.text.path /dev/null \
      ./... 2>/dev/null || true
    echo -e "${PASS} ${DIM}go-lint.json, go-lint.html${RESET}"
  fi

  # govulncheck → JSON
  if ! $OPT_SKIP_GO && command -v govulncheck &>/dev/null; then
    printf "  ${CYAN}▶${RESET} %-48s" "govulncheck → reports/"
    govulncheck -format json ./... > "${REPO_ROOT}/reports/go-vulns.json" 2>/dev/null || true
    echo -e "${PASS} ${DIM}go-vulns.json${RESET}"
  fi

  # ESLint → JSON
  if ! $OPT_SKIP_FRONTEND && [[ -d "${FRONTEND_DIR:-}" ]]; then
    printf "  ${CYAN}▶${RESET} %-48s" "eslint → reports/"
    (cd "$FRONTEND_DIR" && npx eslint --format json -o "${REPO_ROOT}/reports/eslint.json" . 2>/dev/null) || true
    echo -e "${PASS} ${DIM}eslint.json${RESET}"

    printf "  ${CYAN}▶${RESET} %-48s" "npm audit → reports/"
    (cd "$FRONTEND_DIR" && npm audit --json > "${REPO_ROOT}/reports/npm-audit.json" 2>/dev/null) || true
    echo -e "${PASS} ${DIM}npm-audit.json${RESET}"
  fi

  # CodeScene → JSON
  if find_python && ! $OPT_SKIP_CODESCENE && [[ -f "${SCRIPT_DIR}/codescene-check.py" ]]; then
    printf "  ${CYAN}▶${RESET} %-48s" "code health → reports/"
    "$PY" "${SCRIPT_DIR}/codescene-check.py" --all --json > "${REPO_ROOT}/reports/codescene.json" 2>/dev/null || true
    echo -e "${PASS} ${DIM}codescene.json${RESET}"
  fi

  echo ""
  echo -e "  Reports saved to: ${BOLD}${REPO_ROOT}/reports/${RESET}"
  echo ""
fi

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
