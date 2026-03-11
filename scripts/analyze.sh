#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# Full Code Analysis — SonarCloud-equivalent local analysis
#
# Usage:
#   ./scripts/analyze.sh              # run all checks, console output
#   ./scripts/analyze.sh --report     # also generate JSON reports in reports/
#   ./scripts/analyze.sh --go         # Go analysis only
#   ./scripts/analyze.sh --frontend   # Frontend analysis only
#   ./scripts/analyze.sh --fix        # auto-fix what can be fixed
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Flags
RUN_GO=true
RUN_FRONTEND=true
GENERATE_REPORT=false
AUTO_FIX=false
EXIT_CODE=0

for arg in "$@"; do
  case "$arg" in
    --report)    GENERATE_REPORT=true ;;
    --go)        RUN_FRONTEND=false ;;
    --frontend)  RUN_GO=false ;;
    --fix)       AUTO_FIX=true ;;
    --help|-h)
      echo "Usage: $0 [--report] [--go] [--frontend] [--fix]"
      echo ""
      echo "  --report     Generate JSON reports in reports/"
      echo "  --go         Run Go analysis only"
      echo "  --frontend   Run frontend analysis only"
      echo "  --fix        Auto-fix issues where possible"
      echo ""
      exit 0
      ;;
  esac
done

if $GENERATE_REPORT; then
  mkdir -p "$ROOT_DIR/reports"
fi

print_header() {
  echo ""
  echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}${BLUE}  $1${NC}"
  echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════════════${NC}"
  echo ""
}

print_section() {
  echo -e "${CYAN}── $1 ──${NC}"
}

print_pass() {
  echo -e "  ${GREEN}✓${NC} $1"
}

print_fail() {
  echo -e "  ${RED}✗${NC} $1"
  EXIT_CODE=1
}

print_warn() {
  echo -e "  ${YELLOW}!${NC} $1"
}

check_tool() {
  if ! command -v "$1" &>/dev/null; then
    return 1
  fi
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
#  GO ANALYSIS
# ──────────────────────────────────────────────────────────────────────────────
if $RUN_GO; then
  print_header "GO CODE ANALYSIS"

  # 1. go vet
  print_section "go vet (built-in static analysis)"
  if go vet ./... 2>&1; then
    print_pass "go vet passed"
  else
    print_fail "go vet found issues"
  fi

  # 2. golangci-lint
  print_section "golangci-lint (comprehensive linting — 30+ linters)"
  if check_tool golangci-lint; then
    LINT_ARGS="run ./..."
    if $AUTO_FIX; then
      LINT_ARGS="run --fix ./..."
    fi

    if $GENERATE_REPORT; then
      # JSON + HTML reports (golangci-lint v2 flags)
      golangci-lint run \
        --output.json.path "$ROOT_DIR/reports/go-lint.json" \
        --output.html.path "$ROOT_DIR/reports/go-lint.html" \
        --output.text.path stdout \
        ./... 2>&1 || true

      if [ -s "$ROOT_DIR/reports/go-lint.json" ]; then
        ISSUE_COUNT=$(grep -c '"FromLinter"' "$ROOT_DIR/reports/go-lint.json" 2>/dev/null || echo "0")
        if [ "$ISSUE_COUNT" -gt 0 ]; then
          print_fail "golangci-lint found $ISSUE_COUNT issues (reports: reports/go-lint.json, reports/go-lint.html)"
        else
          print_pass "golangci-lint passed (reports: reports/go-lint.json)"
        fi
      else
        print_pass "golangci-lint passed"
      fi
    else
      if golangci-lint $LINT_ARGS 2>&1; then
        print_pass "golangci-lint passed"
      else
        print_fail "golangci-lint found issues (see above)"
      fi
    fi
  else
    print_warn "golangci-lint not installed — run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
  fi

  # 3. govulncheck
  print_section "govulncheck (known vulnerability scan)"
  if check_tool govulncheck; then
    if $GENERATE_REPORT; then
      if govulncheck -format json ./... > "$ROOT_DIR/reports/go-vulns.json" 2>&1; then
        print_pass "govulncheck passed (report: reports/go-vulns.json)"
      else
        print_fail "govulncheck found vulnerabilities (report: reports/go-vulns.json)"
      fi
    else
      if govulncheck ./... 2>&1; then
        print_pass "govulncheck passed"
      else
        print_fail "govulncheck found vulnerabilities"
      fi
    fi
  else
    print_warn "govulncheck not installed — run: go install golang.org/x/vuln/cmd/govulncheck@latest"
  fi

  # 4. go build check
  print_section "go build (compilation check)"
  if go build ./... 2>&1; then
    print_pass "compilation successful"
  else
    print_fail "compilation failed"
  fi
fi

# ──────────────────────────────────────────────────────────────────────────────
#  FRONTEND ANALYSIS
# ──────────────────────────────────────────────────────────────────────────────
if $RUN_FRONTEND; then
  print_header "FRONTEND CODE ANALYSIS"
  FRONTEND_DIR="$ROOT_DIR/web/frontend"

  if [ ! -d "$FRONTEND_DIR/node_modules" ]; then
    print_section "Installing dependencies"
    (cd "$FRONTEND_DIR" && npm ci)
  fi

  # 1. ESLint
  print_section "ESLint (TypeScript + React + SonarJS rules)"
  ESLINT_ARGS="."
  if $AUTO_FIX; then
    ESLINT_ARGS="--fix ."
  fi

  if $GENERATE_REPORT; then
    if (cd "$FRONTEND_DIR" && npx eslint --format json -o "$ROOT_DIR/reports/eslint.json" . 2>&1); then
      print_pass "ESLint passed (report: reports/eslint.json)"
    else
      print_fail "ESLint found issues (report: reports/eslint.json)"
      # Also print human-readable
      (cd "$FRONTEND_DIR" && npx eslint $ESLINT_ARGS 2>&1) || true
    fi
  else
    if (cd "$FRONTEND_DIR" && npx eslint $ESLINT_ARGS 2>&1); then
      print_pass "ESLint passed"
    else
      print_fail "ESLint found issues (see above)"
    fi
  fi

  # 2. TypeScript type checking
  print_section "TypeScript (strict type checking)"
  if (cd "$FRONTEND_DIR" && npx tsc --noEmit 2>&1); then
    print_pass "TypeScript type check passed"
  else
    print_fail "TypeScript found type errors"
  fi

  # 3. npm audit
  print_section "npm audit (dependency vulnerabilities)"
  if $GENERATE_REPORT; then
    (cd "$FRONTEND_DIR" && npm audit --json > "$ROOT_DIR/reports/npm-audit.json" 2>&1) || true
    if (cd "$FRONTEND_DIR" && npm audit --audit-level=high 2>&1); then
      print_pass "npm audit passed (report: reports/npm-audit.json)"
    else
      print_fail "npm audit found high/critical vulnerabilities (report: reports/npm-audit.json)"
    fi
  else
    if (cd "$FRONTEND_DIR" && npm audit --audit-level=high 2>&1); then
      print_pass "npm audit passed"
    else
      print_fail "npm audit found high/critical vulnerabilities"
    fi
  fi
fi

# ──────────────────────────────────────────────────────────────────────────────
#  SUMMARY
# ──────────────────────────────────────────────────────────────────────────────
print_header "ANALYSIS COMPLETE"

if $GENERATE_REPORT; then
  echo -e "Reports saved to: ${BOLD}$ROOT_DIR/reports/${NC}"
  ls -la "$ROOT_DIR/reports/" 2>/dev/null
  echo ""
fi

if [ $EXIT_CODE -eq 0 ]; then
  echo -e "${GREEN}${BOLD}All checks passed!${NC}"
else
  echo -e "${RED}${BOLD}Some checks failed — review the output above.${NC}"
fi

exit $EXIT_CODE
