param(
    [int]$Loops = 10
)

$ErrorActionPreference = "Stop"

# --- Preflight checks ---

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: git not found in PATH" -ForegroundColor Red
    exit 1
}

if (-not (Get-Command agent -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: Cursor CLI 'agent' not found in PATH" -ForegroundColor Red
    Write-Host "Install with:  irm 'https://cursor.com/install?win32=true' | iex" -ForegroundColor Yellow
    exit 1
}

# Check login status
$aboutOutput = agent about 2>&1 | Out-String
if ($aboutOutput -match "Not logged in") {
    Write-Host "ERROR: Cursor CLI not logged in. Run 'agent login' first." -ForegroundColor Red
    exit 1
}

# Verify we're inside a git repo
$null = git rev-parse --show-toplevel 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: not inside a git repository" -ForegroundColor Red
    exit 1
}

Write-Host "Starting $Loops audit loop(s) in: $(Get-Location)" -ForegroundColor Cyan
Write-Host ""

# --- Main loop ---

for ($i = 1; $i -le $Loops; $i++) {

    Write-Host "============================================" -ForegroundColor Green
    Write-Host "  AUDIT LOOP $i / $Loops" -ForegroundColor Green
    Write-Host "============================================" -ForegroundColor Green

    $prompt = @"
You are running audit cycle $i of $Loops.

Follow the audit-loop rule in .cursor/rules/audit-loop.mdc. For each step:

Step 1: Search for ONE TODO/FIXME/HACK/STUB/incomplete implementation and fully implement it.
Step 2: Run linters or static analysis, fix ONE reported issue.
Step 3: Pick one function, trace all call sites, verify args/returns/errors, repair gaps.
Step 4: Harden one function for edge cases (null, empty, overflow, network/fs failures).
Step 5: Check interface contracts between modules, fix any type or API mismatches.
Step 6: Find one silent failure (empty catch, ignored return, bare except) and fix it.
Step 7: Verify the repo builds and lint passes. Fix regressions if any.

After EACH individual fix, commit with: git add -A && git commit -m "<conventional commit message>"
If a step finds nothing to fix, skip it and move on.
Do NOT introduce new TODOs or placeholders.
After all steps, commit: git add -A && git commit -m "chore(audit-loop): cycle $i/$Loops"
"@

    Write-Host "Sending prompt to agent ..." -ForegroundColor DarkGray

    # -p = print/headless mode, --force = allow file writes without confirmation
    agent -p --force $prompt 2>&1 | ForEach-Object { Write-Host $_ }

    if ($LASTEXITCODE -ne 0) {
        Write-Host "WARNING: agent exited with code $LASTEXITCODE" -ForegroundColor Yellow
    }

    # Catch any remaining uncommitted changes
    git add -A 2>$null

    $changes = git diff --cached --name-only 2>&1
    if (-not [string]::IsNullOrWhiteSpace($changes)) {
        Write-Host "Committing remaining changes from cycle $i ..." -ForegroundColor DarkGray
        git commit -m "chore(audit-loop): cycle $i/$Loops" 2>$null
    }

    Write-Host ""
}

Write-Host "============================================" -ForegroundColor Green
Write-Host "  AUDIT COMPLETE: $Loops cycle(s) finished" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
