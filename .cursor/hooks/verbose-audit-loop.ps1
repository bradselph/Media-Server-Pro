param(
    [int]$Loops = 10,
    [string]$Model = "",
    [switch]$ListModels,
    [switch]$DryRun,
    [switch]$Quiet
)

$ErrorActionPreference = "Stop"

# ── Helpers ──────────────────────────────────────────────────────────

function Write-Banner {
    param([string]$Text, [ConsoleColor]$Color = "Green")
    $bar = "=" * 60
    Write-Host ""
    Write-Host $bar -ForegroundColor $Color
    Write-Host "  $Text" -ForegroundColor $Color
    Write-Host $bar -ForegroundColor $Color
}

function Write-Step {
    param([string]$Label, [string]$Detail = "")
    $ts = (Get-Date).ToString("HH:mm:ss")
    Write-Host "[$ts] " -ForegroundColor DarkGray -NoNewline
    Write-Host "$Label" -ForegroundColor Cyan -NoNewline
    if ($Detail) { Write-Host " $Detail" -ForegroundColor White }
    else         { Write-Host "" }
}

function Write-Metric {
    param([string]$Label, [string]$Value, [ConsoleColor]$ValueColor = "Yellow")
    Write-Host "       $Label : " -ForegroundColor DarkGray -NoNewline
    Write-Host $Value -ForegroundColor $ValueColor
}

function Get-CommitCount {
    $count = git rev-list --count HEAD 2>$null
    if ($LASTEXITCODE -ne 0) { return 0 }
    return [int]$count
}

function Get-ElapsedStr {
    param([datetime]$Start)
    $span = (Get-Date) - $Start
    if ($span.TotalMinutes -ge 1) {
        return "{0}m {1}s" -f [math]::Floor($span.TotalMinutes), $span.Seconds
    }
    return "{0:F1}s" -f $span.TotalSeconds
}

# ── Preflight ────────────────────────────────────────────────────────

Write-Banner "AUDIT-LOOP  —  Preflight" "Cyan"

# git
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host "  ERROR: git not found in PATH" -ForegroundColor Red
    exit 1
}
Write-Step "git" "$(git --version)"

# agent CLI
if (-not (Get-Command agent -ErrorAction SilentlyContinue)) {
    Write-Host "  ERROR: Cursor CLI 'agent' not found in PATH" -ForegroundColor Red
    Write-Host "  Install:  irm 'https://cursor.com/install?win32=true' | iex" -ForegroundColor Yellow
    exit 1
}
$agentVersion = agent --version 2>&1 | Out-String
Write-Step "agent" $agentVersion.Trim()

# --list-models: print available models and exit
if ($ListModels) {
    Write-Banner "Available Models" "Cyan"
    agent --list-models 2>&1 | ForEach-Object { Write-Host "  $_" }
    exit 0
}

# Login check
$aboutOutput = agent about 2>&1 | Out-String
if ($aboutOutput -match "Not logged in") {
    Write-Host "  ERROR: Cursor CLI not logged in. Run 'agent login' first." -ForegroundColor Red
    exit 1
}
Write-Step "auth" "logged in"

# Git repo
$repoRoot = git rev-parse --show-toplevel 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "  ERROR: not inside a git repository" -ForegroundColor Red
    exit 1
}
Write-Step "repo" $repoRoot
Write-Step "branch" (git branch --show-current 2>$null)

# Model
$modelArg = @()
if ($Model) {
    $modelArg = @("-m", $Model)
    Write-Step "model" $Model
} else {
    Write-Step "model" "(default — pass -Model <name> to override)"
}

if ($DryRun) {
    Write-Step "mode" "DRY RUN — will print prompts but not execute"
}

# Session summary
Write-Banner "Configuration" "Cyan"
Write-Metric "Loops"    "$Loops"
Write-Metric "Model"    $(if ($Model) { $Model } else { "default" })
Write-Metric "Repo"     $repoRoot
Write-Metric "Branch"   (git branch --show-current 2>$null)
Write-Metric "DryRun"   "$DryRun"

$sessionStart = Get-Date
$cycleStats   = @()
$startCommits = Get-CommitCount

Write-Banner "Starting $Loops audit cycle(s)" "Green"

# ── Main loop ────────────────────────────────────────────────────────

for ($i = 1; $i -le $Loops; $i++) {

    $cycleStart   = Get-Date
    $commitsBefore = Get-CommitCount

    Write-Banner "AUDIT CYCLE $i / $Loops" "Green"
    Write-Step "cycle-start" "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"

    # Snapshot file counts
    $filesBefore = (git diff --stat HEAD~0 2>$null) # baseline

    $prompt = @"
You are running audit cycle $i of $Loops.

Follow the verbose-audit-loop rule in .cursor/rules/verbose-audit-loop.mdc exactly.

## IMPORTANT — Verbose Output

You MUST narrate your work in real time. For EVERY step, print progress as you go using this exact format:

### Entering a step:
  [step N] <step name> — searching ...

### When you find something:
  [step N]  found: <file>:<line> — <brief description of what you found>

### When you fix it:
  [step N]  fix: <file>:<line> — <what you changed and why>

### When you commit:
  [step N]  commit: <full conventional commit message>

### When a step finds nothing:
  [step N] <step name> — nothing found, skipping.

Do NOT work silently. The operator is watching the terminal and needs to see what you are doing as you do it.

## Steps

Step 1 — TODO / Incomplete: search for ONE TODO/FIXME/HACK/STUB/incomplete implementation. Fully implement it.
Step 2 — Lint / Static Analysis: run project linters, fix ONE reported issue.
Step 3 — Function Trace: pick one function, trace all call sites, verify args/returns/errors, repair gaps.
Step 4 — Edge-Case Hardening: harden one function (null, empty, overflow, network/fs failures).
Step 5 — Interface Contracts: check module boundaries, fix type or API mismatches.
Step 6 — Silent Failures: find one silent failure (empty catch, ignored return, bare except), fix it.
Step 7 — Validation Pass: verify the repo builds and lint passes. Fix regressions if any.

## Commit Rules

After EACH individual fix, commit:  git add -A && git commit -m "<type>(<scope>): <description>"
If a step finds nothing to fix, skip it and move on.
Do NOT introduce new TODOs or placeholders.

## Summary Table

After all steps, output a summary table in this EXACT format:

## Audit Cycle $i/$Loops — Complete

| Step | Status  | File(s)          | Description                     | Commit                         |
|------|---------|------------------|---------------------------------|--------------------------------|
| 1    | fixed   | path/to/file.ext | what was fixed                  | fix(todo): message             |
| 2    | skipped | —                | no lint issues found            | —                              |
| ...  | ...     | ...              | ...                             | ...                            |

Status must be one of: fixed, skipped, no-issue.
Include the FULL commit message used (or — if skipped).
Then output the final line:  AUDIT_CYCLE_DONE

After the summary, commit: git add -A && git commit -m "chore(verbose-audit-loop): cycle $i/$Loops"
"@

    if ($DryRun) {
        Write-Host ""
        Write-Host "── Prompt ──" -ForegroundColor DarkGray
        Write-Host $prompt -ForegroundColor DarkGray
        Write-Host "── End Prompt ──" -ForegroundColor DarkGray
        $cycleStats += [PSCustomObject]@{
            Cycle    = $i
            Duration = "0s"
            Commits  = 0
            Status   = "dry-run"
        }
        continue
    }

    Write-Step "prompt" "sending to agent ..."

    # Build the full command
    $agentArgs = @("-p", "--force") + $modelArg + @($prompt)

    $agentOutput = ""
    agent @agentArgs 2>&1 | ForEach-Object {
        $line = $_
        $agentOutput += "$line`n"
        if (-not $Quiet) {
            # Colorize agent output based on narration prefixes
            if     ($line -match "AUDIT_CYCLE_DONE")                       { Write-Host $line -ForegroundColor Green   }
            elseif ($line -match "^##")                                    { Write-Host $line -ForegroundColor Green   }
            elseif ($line -match "^\s*\[step \d+\].*found:")               { Write-Host $line -ForegroundColor Cyan    }
            elseif ($line -match "^\s*\[step \d+\].*fix:")                 { Write-Host $line -ForegroundColor Yellow  }
            elseif ($line -match "^\s*\[step \d+\].*commit:")              { Write-Host $line -ForegroundColor Magenta }
            elseif ($line -match "^\s*\[step \d+\].*skipping")             { Write-Host $line -ForegroundColor DarkGray}
            elseif ($line -match "^\s*\[step \d+\]")                       { Write-Host $line -ForegroundColor White   }
            elseif ($line -match "^\| ")                                   { Write-Host $line -ForegroundColor White   }
            elseif ($line -match "^Step |^fix\(|^chore\(")                 { Write-Host $line -ForegroundColor Yellow  }
            elseif ($line -match "ERROR|FAIL|error")                       { Write-Host $line -ForegroundColor Red     }
            elseif ($line -match "WARNING|WARN|warn")                      { Write-Host $line -ForegroundColor Yellow  }
            else                                                           { Write-Host $line -ForegroundColor Gray    }
        }
    }

    if ($LASTEXITCODE -ne 0) {
        Write-Host ""
        Write-Host "  WARNING: agent exited with code $LASTEXITCODE" -ForegroundColor Yellow
    }

    # Catch any uncommitted leftovers
    git add -A 2>$null
    $staged = git diff --cached --name-only 2>&1
    if (-not [string]::IsNullOrWhiteSpace($staged)) {
        Write-Step "cleanup" "committing remaining staged changes"
        git commit -m "chore(verbose-audit-loop): cycle $i/$Loops" 2>$null
    }

    # ── Cycle stats ──

    $commitsAfter = Get-CommitCount
    $cycleCommits = $commitsAfter - $commitsBefore
    $elapsed      = Get-ElapsedStr $cycleStart

    # Detect completion signal
    $completed = $agentOutput -match "AUDIT_CYCLE_DONE"

    Write-Host ""
    Write-Host "  ── Cycle $i Summary ──" -ForegroundColor Cyan
    Write-Metric "Duration"    $elapsed
    Write-Metric "Commits"     "$cycleCommits"
    Write-Metric "Completed"   $(if ($completed) { "yes" } else { "partial / unknown" })

    # Show recent commits from this cycle
    if ($cycleCommits -gt 0) {
        Write-Host ""
        Write-Host "  Recent commits:" -ForegroundColor DarkGray
        git log --oneline -n $cycleCommits --format="       %C(yellow)%h%C(reset) %s" 2>$null |
            ForEach-Object { Write-Host $_ }
    }

    # Show files changed
    $changedFiles = git diff --name-only HEAD~$cycleCommits HEAD 2>$null
    if ($changedFiles) {
        $fileCount = ($changedFiles | Measure-Object).Count
        Write-Metric "Files changed" "$fileCount"
    }

    $cycleStats += [PSCustomObject]@{
        Cycle    = $i
        Duration = $elapsed
        Commits  = $cycleCommits
        Status   = if ($completed) { "done" } else { "partial" }
    }

    Write-Host ""
}

# ── Final report ─────────────────────────────────────────────────────

$totalElapsed  = Get-ElapsedStr $sessionStart
$totalCommits  = (Get-CommitCount) - $startCommits

Write-Banner "AUDIT COMPLETE" "Green"

Write-Host ""
Write-Host "  ┌────────┬────────────┬─────────┬──────────┐" -ForegroundColor DarkGray
Write-Host "  │ Cycle  │  Duration  │ Commits │  Status  │" -ForegroundColor DarkGray
Write-Host "  ├────────┼────────────┼─────────┼──────────┤" -ForegroundColor DarkGray
foreach ($s in $cycleStats) {
    $c = if ($s.Status -eq "done") { "Green" } elseif ($s.Status -eq "dry-run") { "DarkGray" } else { "Yellow" }
    $row = "  │ {0,-6} │ {1,-10} │ {2,-7} │ {3,-8} │" -f $s.Cycle, $s.Duration, $s.Commits, $s.Status
    Write-Host $row -ForegroundColor $c
}
Write-Host "  └────────┴────────────┴─────────┴──────────┘" -ForegroundColor DarkGray

Write-Host ""
Write-Metric "Total cycles"  "$Loops"
Write-Metric "Total commits" "$totalCommits"
Write-Metric "Total time"    $totalElapsed
Write-Metric "Model"         $(if ($Model) { $Model } else { "default" })
Write-Metric "Branch"        (git branch --show-current 2>$null)
Write-Host ""
