#!/usr/bin/env bash
set -euo pipefail

# Delete failed GitHub Actions workflow runs from this repository.
#
# Requirements:
#   - gh CLI installed and authenticated (`gh auth login`)
#   - Run from inside a clone of the target repo, or pass --repo owner/name
#
# Usage:
#   ./clear-failed-actions.sh                       # delete all failed runs
#   ./clear-failed-actions.sh --dry-run             # list what would be deleted
#   ./clear-failed-actions.sh --repo owner/name     # target a specific repo
#   ./clear-failed-actions.sh --workflow "CI"       # limit to one workflow
#   ./clear-failed-actions.sh --status cancelled    # delete cancelled (or any other) status
#   ./clear-failed-actions.sh --status all-bad      # delete failure, cancelled, timed_out, action_required, startup_failure

REPO=""
WORKFLOW=""
STATUS="failure"
DRY_RUN=0
LIMIT=1000

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)      REPO="$2"; shift 2 ;;
    --workflow)  WORKFLOW="$2"; shift 2 ;;
    --status)    STATUS="$2"; shift 2 ;;
    --limit)     LIMIT="$2"; shift 2 ;;
    --dry-run)   DRY_RUN=1; shift ;;
    -h|--help)
      sed -n '3,18p' "$0"; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if ! command -v gh >/dev/null 2>&1; then
  echo "error: gh CLI is not installed. See https://cli.github.com/" >&2
  exit 1
fi

if ! gh auth status >/dev/null 2>&1; then
  echo "error: gh is not authenticated. Run 'gh auth login' first." >&2
  exit 1
fi

REPO_FLAG=()
if [[ -n "$REPO" ]]; then
  REPO_FLAG=(--repo "$REPO")
fi

# Resolve the set of statuses to clear.
STATUSES=()
case "$STATUS" in
  all-bad)
    STATUSES=(failure cancelled timed_out action_required startup_failure)
    ;;
  *)
    STATUSES=("$STATUS")
    ;;
esac

collect_runs() {
  local status="$1"
  local args=(run list --status "$status" --limit "$LIMIT" --json databaseId,name,displayTitle,headBranch,conclusion)
  if [[ -n "$WORKFLOW" ]]; then
    args+=(--workflow "$WORKFLOW")
  fi
  gh "${REPO_FLAG[@]}" "${args[@]}"
}

TOTAL=0
DELETED=0
FAILED=0

for status in "${STATUSES[@]}"; do
  echo "==> Fetching runs with status '$status'..."
  runs_json="$(collect_runs "$status")"
  count="$(echo "$runs_json" | jq 'length')"
  TOTAL=$(( TOTAL + count ))
  echo "    found $count run(s)"

  if [[ "$count" -eq 0 ]]; then
    continue
  fi

  while IFS=$'\t' read -r id name branch title; do
    [[ -z "$id" ]] && continue
    if [[ "$DRY_RUN" -eq 1 ]]; then
      printf '    [dry-run] would delete %s  %s @ %s  %s\n' "$id" "$name" "$branch" "$title"
      continue
    fi
    printf '    deleting %s  %s @ %s ... ' "$id" "$name" "$branch"
    if gh "${REPO_FLAG[@]}" run delete "$id" >/dev/null 2>&1; then
      echo "ok"
      DELETED=$(( DELETED + 1 ))
    else
      echo "FAILED"
      FAILED=$(( FAILED + 1 ))
    fi
  done < <(echo "$runs_json" | jq -r '.[] | [.databaseId, .name, .headBranch, .displayTitle] | @tsv')
done

echo
echo "Summary:"
echo "  matched: $TOTAL"
if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "  (dry-run, nothing deleted)"
else
  echo "  deleted: $DELETED"
  echo "  errors:  $FAILED"
fi

# If we hit the per-status limit, more runs may remain.
for status in "${STATUSES[@]}"; do
  if [[ "$TOTAL" -ge "$LIMIT" ]]; then
    echo
    echo "note: page size capped at $LIMIT. Re-run the script to clear remaining runs."
    break
  fi
done
