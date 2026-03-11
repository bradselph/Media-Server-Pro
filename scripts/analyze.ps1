# ──────────────────────────────────────────────────────────────────────────────
# Full Code Analysis — SonarCloud-equivalent local analysis (Windows PowerShell)
#
# Usage:
#   .\scripts\analyze.ps1              # run all checks
#   .\scripts\analyze.ps1 -Report      # generate JSON reports in reports\
#   .\scripts\analyze.ps1 -GoOnly      # Go analysis only
#   .\scripts\analyze.ps1 -FrontendOnly # Frontend analysis only
#   .\scripts\analyze.ps1 -Fix         # auto-fix where possible
# ──────────────────────────────────────────────────────────────────────────────
param(
    [switch]$Report,
    [switch]$GoOnly,
    [switch]$FrontendOnly,
    [switch]$Fix,
    [switch]$Help
)

$ErrorActionPreference = "Continue"
$RootDir = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
Set-Location $RootDir

$ExitCode = 0
$RunGo = -not $FrontendOnly
$RunFrontend = -not $GoOnly

if ($Help) {
    Write-Host "Usage: .\scripts\analyze.ps1 [-Report] [-GoOnly] [-FrontendOnly] [-Fix]"
    Write-Host ""
    Write-Host "  -Report         Generate JSON reports in reports\"
    Write-Host "  -GoOnly         Run Go analysis only"
    Write-Host "  -FrontendOnly   Run frontend analysis only"
    Write-Host "  -Fix            Auto-fix issues where possible"
    exit 0
}

if ($Report) {
    New-Item -ItemType Directory -Force -Path "$RootDir\reports" | Out-Null
}

function Write-Header($text) {
    Write-Host ""
    Write-Host "=================================================================" -ForegroundColor Blue
    Write-Host "  $text" -ForegroundColor Blue
    Write-Host "=================================================================" -ForegroundColor Blue
    Write-Host ""
}

function Write-Section($text) {
    Write-Host "-- $text --" -ForegroundColor Cyan
}

function Write-Pass($text) {
    Write-Host "  + $text" -ForegroundColor Green
}

function Write-Fail($text) {
    Write-Host "  X $text" -ForegroundColor Red
    $script:ExitCode = 1
}

function Write-Warn($text) {
    Write-Host "  ! $text" -ForegroundColor Yellow
}

function Test-Command($name) {
    return [bool](Get-Command $name -ErrorAction SilentlyContinue)
}

# ──────────────────────────────────────────────────────────────────────────────
#  GO ANALYSIS
# ──────────────────────────────────────────────────────────────────────────────
if ($RunGo) {
    Write-Header "GO CODE ANALYSIS"

    # go vet
    Write-Section "go vet (built-in static analysis)"
    $output = & go vet ./... 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Pass "go vet passed"
    } else {
        Write-Host $output
        Write-Fail "go vet found issues"
    }

    # golangci-lint
    Write-Section "golangci-lint (comprehensive linting)"
    if (Test-Command "golangci-lint") {
        $lintArgs = @("run", "./...")
        if ($Fix) { $lintArgs = @("run", "--fix", "./...") }

        if ($Report) {
            & golangci-lint run --output.json.path "$RootDir\reports\go-lint.json" --output.html.path "$RootDir\reports\go-lint.html" --output.text.path stdout ./... 2>&1
            if ($LASTEXITCODE -eq 0) {
                Write-Pass "golangci-lint passed (reports: reports\go-lint.json, reports\go-lint.html)"
            } else {
                Write-Fail "golangci-lint found issues (reports: reports\go-lint.json, reports\go-lint.html)"
            }
        } else {
            & golangci-lint @lintArgs 2>&1
            if ($LASTEXITCODE -eq 0) {
                Write-Pass "golangci-lint passed"
            } else {
                Write-Fail "golangci-lint found issues"
            }
        }
    } else {
        Write-Warn "golangci-lint not installed -- run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    }

    # govulncheck
    Write-Section "govulncheck (known vulnerability scan)"
    if (Test-Command "govulncheck") {
        if ($Report) {
            & govulncheck -format json ./... 2>&1 | Out-File -FilePath "$RootDir\reports\go-vulns.json" -Encoding utf8
            if ($LASTEXITCODE -eq 0) {
                Write-Pass "govulncheck passed (report: reports\go-vulns.json)"
            } else {
                Write-Fail "govulncheck found vulnerabilities (report: reports\go-vulns.json)"
            }
        } else {
            & govulncheck ./... 2>&1
            if ($LASTEXITCODE -eq 0) {
                Write-Pass "govulncheck passed"
            } else {
                Write-Fail "govulncheck found vulnerabilities"
            }
        }
    } else {
        Write-Warn "govulncheck not installed -- run: go install golang.org/x/vuln/cmd/govulncheck@latest"
    }

    # build check
    Write-Section "go build (compilation check)"
    & go build ./... 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Pass "compilation successful"
    } else {
        Write-Fail "compilation failed"
    }
}

# ──────────────────────────────────────────────────────────────────────────────
#  FRONTEND ANALYSIS
# ──────────────────────────────────────────────────────────────────────────────
if ($RunFrontend) {
    Write-Header "FRONTEND CODE ANALYSIS"
    $FrontendDir = "$RootDir\web\frontend"

    if (-not (Test-Path "$FrontendDir\node_modules")) {
        Write-Section "Installing dependencies"
        Push-Location $FrontendDir
        & npm ci
        Pop-Location
    }

    # ESLint
    Write-Section "ESLint (TypeScript + React + SonarJS rules)"
    Push-Location $FrontendDir
    if ($Report) {
        & npx eslint --format json -o "$RootDir\reports\eslint.json" . 2>&1
        if ($LASTEXITCODE -eq 0) {
            Write-Pass "ESLint passed (report: reports\eslint.json)"
        } else {
            Write-Fail "ESLint found issues (report: reports\eslint.json)"
            if ($Fix) { & npx eslint --fix . 2>&1 } else { & npx eslint . 2>&1 }
        }
    } else {
        if ($Fix) { & npx eslint --fix . 2>&1 } else { & npx eslint . 2>&1 }
        if ($LASTEXITCODE -eq 0) {
            Write-Pass "ESLint passed"
        } else {
            Write-Fail "ESLint found issues"
        }
    }
    Pop-Location

    # TypeScript
    Write-Section "TypeScript (strict type checking)"
    Push-Location $FrontendDir
    & npx tsc --noEmit 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Pass "TypeScript type check passed"
    } else {
        Write-Fail "TypeScript found type errors"
    }
    Pop-Location

    # npm audit
    Write-Section "npm audit (dependency vulnerabilities)"
    Push-Location $FrontendDir
    if ($Report) {
        & npm audit --json 2>&1 | Out-File -FilePath "$RootDir\reports\npm-audit.json" -Encoding utf8
    }
    & npm audit --audit-level=high 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Pass "npm audit passed"
    } else {
        Write-Fail "npm audit found high/critical vulnerabilities"
    }
    Pop-Location
}

# ──────────────────────────────────────────────────────────────────────────────
#  SUMMARY
# ──────────────────────────────────────────────────────────────────────────────
Write-Header "ANALYSIS COMPLETE"

if ($Report) {
    Write-Host "Reports saved to: $RootDir\reports\"
    Get-ChildItem "$RootDir\reports" | Format-Table Name, Length, LastWriteTime
}

if ($ExitCode -eq 0) {
    Write-Host "All checks passed!" -ForegroundColor Green
} else {
    Write-Host "Some checks failed -- review the output above." -ForegroundColor Red
}

exit $ExitCode
