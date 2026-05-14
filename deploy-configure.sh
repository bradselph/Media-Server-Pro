#!/usr/bin/env bash
# deploy-configure.sh — interactive prompt for newly-added forwarded
# config knobs (registry: deploy-knobs.sh). Reads the operator's
# current .deploy.env and only prompts for knobs the file makes no
# mention of yet — knobs the operator has already seen and either
# set or accepted-default-on are NOT re-prompted. This way every
# subsequent ./deploy.sh is silent unless a code release added a
# new option.
#
# Usage:
#   ./deploy-configure.sh                  # prompt only ★ NEW knobs
#   ./deploy-configure.sh --review         # prompt EVERY knob (rare;
#                                          # use when re-auditing config)
#   ./deploy-configure.sh --only KEY       # prompt one specific knob
#   ./deploy-configure.sh --set KEY=VAL    # set one knob without prompt
#                                          # (e.g. --set FEATURE_CLAUDE=true).
#                                          # Repeatable; KEY must be in
#                                          # the registry; VAL may be empty.
#   ./deploy-configure.sh --list           # list every knob with
#                                          # current value, no prompts
#   ./deploy-configure.sh --quiet          # apply defaults for any
#                                          # missing knob, no prompts
#   ./deploy-configure.sh --file PATH      # operate on a non-default
#                                          # env file (test fixture)
#
# How values are applied:
#   - If the file already has `KNOB=value` (uncommented), that line
#     is replaced with the new value.
#   - If the file has a commented-out `# KNOB=…` hint, it stays as
#     documentation; the new uncommented line is appended at the end.
#   - If the file doesn't have the knob at all, it's appended.
#   - VPS coordinates and other non-knob lines (VPS_HOST, KEY_FILE,
#     etc.) are NEVER touched.
#
# Prompt protocol:
#   Enter alone   → keep current value
#   "-"           → clear (set to empty string)
#   anything else → use the typed value verbatim
#   Ctrl-C        → abort without writing
#
# ★ NEW detection:
#   A knob is "new" when no line in the env file mentions it at all
#   (commented or otherwise). The prompter tags those with ★ NEW so
#   options introduced in a release land in front of the operator on
#   the next deploy. Pressing Enter (or setting a value) writes a
#   line, so the tag clears on the next walk.
#
# Sensitive knobs (KNOB_SENSITIVE=true in deploy-knobs.sh):
#   The prompter shows "********" instead of the value and reads input
#   silently (read -s). Storage in .deploy.env is unchanged — masking
#   is display-only.

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[configure]${RESET} $*"; }
success() { echo -e "${GREEN}[configure]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[configure]${RESET} $*"; }
die()     { echo -e "${RED}[configure] ERROR:${RESET} $*" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source the knob registry. This populates KNOB_ORDER, KNOB_DESCRIPTION,
# KNOB_DEFAULT, KNOB_SCOPE, KNOB_SECTION, KNOB_SENSITIVE.
# shellcheck disable=SC1091
source "$SCRIPT_DIR/deploy-knobs.sh"

# ── Flags ─────────────────────────────────────────────────────────────
ENV_FILE="$SCRIPT_DIR/.deploy.env"
MODE="walk"            # walk | review | only | set | list | quiet
ONLY_KEY=""
SET_PAIRS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --review) MODE="review"; shift ;;
    --only)   MODE="only"; ONLY_KEY="$2"; shift 2 ;;
    --set)
      MODE="set"
      [[ "$2" != *=* ]] && die "--set expects KEY=VALUE, got: $2"
      SET_PAIRS+=("$2")
      shift 2
      ;;
    --list)   MODE="list"; shift ;;
    --quiet)  MODE="quiet"; shift ;;
    --file)   ENV_FILE="$2"; shift 2 ;;
    --help|-h)
      sed -n '2,/^set -/p' "$0" | sed -n '/^# /p' | sed 's/^# \?//'
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

# ── File helpers ──────────────────────────────────────────────────────

# read_env_value FILE KEY → echoes the current uncommented value (or
# empty string). Considers only lines matching `^[[:space:]]*KEY=`;
# strips surrounding double quotes.
read_env_value() {
  local file="$1" key="$2"
  [[ ! -f "$file" ]] && { echo ""; return; }
  # Last uncommented assignment wins (matches bash sourcing semantics).
  local val
  val=$(grep -E "^[[:space:]]*${key}=" "$file" 2>/dev/null | tail -n 1 | cut -d= -f2-)
  # Strip optional surrounding double quotes.
  val="${val%\"}"; val="${val#\"}"
  printf '%s' "$val"
}

# is_new_knob FILE KEY — true when the env file makes no mention of
# KEY at all (uncommented assignment AND commented hint both absent).
# Used to flag knobs that landed in deploy-knobs.sh after the
# operator's last walk so they don't slip past unnoticed.
is_new_knob() {
  local file="$1" key="$2"
  [[ ! -f "$file" ]] && return 0
  if grep -qE "(^|[[:space:]#])${key}=" "$file" 2>/dev/null; then
    return 1
  fi
  return 0
}

# mask_value VAL — turn a sensitive value into a fixed-width masked
# string. Uses a constant 8 stars so the length doesn't leak.
mask_value() {
  local val="$1"
  [[ -z "$val" ]] && { printf ''; return; }
  printf '********'
}

# upsert_env_var FILE KEY VALUE — replace or append. Only matches
# uncommented assignments; commented-out hint lines (# KEY=…) are
# preserved as documentation.
upsert_env_var() {
  local file="$1" key="$2" val="$3"
  if [[ ! -f "$file" ]]; then
    : > "$file"
  fi
  local tmp
  tmp="$(mktemp)"
  local found=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" =~ ^[[:space:]]*${key}= ]]; then
      printf '%s=%s\n' "$key" "$val" >> "$tmp"
      found=1
    else
      printf '%s\n' "$line" >> "$tmp"
    fi
  done < "$file"
  if [[ $found -eq 0 ]]; then
    # Append with a leading blank line if the file doesn't end in one,
    # so the appended block doesn't smash against existing content.
    if [[ -s "$tmp" ]] && [[ "$(tail -c 1 "$tmp" | wc -l)" -eq 0 ]]; then
      printf '\n' >> "$tmp"
    fi
    printf '%s=%s\n' "$key" "$val" >> "$tmp"
  fi
  mv "$tmp" "$file"
}

# ── Display helpers ───────────────────────────────────────────────────

# print_knob KEY — pretty-print one knob (description + scope + section
# + current value). Used by both --list and the walk. Adds ★ NEW for
# knobs the operator hasn't seen yet and masks sensitive values.
print_knob() {
  local key="$1"
  local current
  current="$(read_env_value "$ENV_FILE" "$key")"
  local default="${KNOB_DEFAULT[$key]:-}"
  local desc="${KNOB_DESCRIPTION[$key]:-(no description)}"
  local section="${KNOB_SECTION[$key]:-Other}"
  local scope="${KNOB_SCOPE[$key]:-runtime}"
  local sensitive="${KNOB_SENSITIVE[$key]:-}"
  local new_tag=""
  if is_new_knob "$ENV_FILE" "$key"; then
    new_tag=" ${YELLOW}★ NEW${RESET}"
  fi

  echo ""
  echo -e "${BOLD}${key}${RESET} ${DIM}[${section} · ${scope}]${RESET}${new_tag}"
  echo -e "  ${DIM}${desc}${RESET}"
  local display_current="$current"
  local display_default="$default"
  if [[ "$sensitive" == "true" ]]; then
    display_current="$(mask_value "$current")"
    display_default="$(mask_value "$default")"
  fi
  if [[ -n "$current" ]]; then
    echo -e "  current : ${GREEN}${display_current}${RESET}"
  else
    echo -e "  current : ${DIM}(unset)${RESET}"
  fi
  if [[ -n "$default" ]] && [[ "$default" != "$current" ]]; then
    echo -e "  default : ${display_default}"
  fi
}

# prompt_knob KEY — print + interactive prompt. Writes display to
# stdout normally and reports the action taken via the global
# KNOB_PROMPT_RESULT (kept | set | cleared | accepted-default). The
# global is used instead of a stdout return so the caller can run
# this directly without command substitution swallowing the prompt
# itself.
KNOB_PROMPT_RESULT=""
prompt_knob() {
  local key="$1"
  local current
  current="$(read_env_value "$ENV_FILE" "$key")"

  print_knob "$key"

  local hint
  if [[ -n "$current" ]]; then
    hint="Enter = keep, '-' = clear, or new value"
  else
    hint="Enter = leave unset (default), or new value"
  fi
  echo -en "  ${CYAN}>${RESET} ${DIM}(${hint})${RESET} "
  local reply=""
  local sensitive="${KNOB_SENSITIVE[$key]:-}"
  if [[ "$sensitive" == "true" ]]; then
    # Silent read so the secret doesn't echo to the terminal.
    read -r -s reply </dev/tty || true
    echo ""
  else
    read -r reply </dev/tty || true
  fi
  reply="${reply//$'\r'/}"

  if [[ -z "$reply" ]]; then
    # Pressing Enter on a never-seen knob with a default writes the
    # default explicitly so the ★ NEW tag clears next time. Without
    # this, the knob stays "new" forever and nags every deploy.
    if is_new_knob "$ENV_FILE" "$key" && [[ -n "${KNOB_DEFAULT[$key]:-}" ]]; then
      upsert_env_var "$ENV_FILE" "$key" "${KNOB_DEFAULT[$key]}"
      echo -e "    ${DIM}accepted default${RESET}"
      KNOB_PROMPT_RESULT="accepted-default"
      return
    fi
    # Same idea for never-seen knobs with NO default — write an empty
    # line so the registry knows we've seen it.
    if is_new_knob "$ENV_FILE" "$key"; then
      upsert_env_var "$ENV_FILE" "$key" ""
      echo -e "    ${DIM}left empty${RESET}"
      KNOB_PROMPT_RESULT="kept"
      return
    fi
    echo -e "    ${DIM}kept${RESET}"
    KNOB_PROMPT_RESULT="kept"
    return
  fi
  if [[ "$reply" == "-" ]]; then
    upsert_env_var "$ENV_FILE" "$key" ""
    echo -e "    ${YELLOW}cleared${RESET}"
    KNOB_PROMPT_RESULT="cleared"
    return
  fi
  upsert_env_var "$ENV_FILE" "$key" "$reply"
  if [[ "$sensitive" == "true" ]]; then
    echo -e "    ${GREEN}set (value hidden)${RESET}"
  else
    echo -e "    ${GREEN}set to: ${reply}${RESET}"
  fi
  KNOB_PROMPT_RESULT="set"
}

# ── Modes ─────────────────────────────────────────────────────────────

# Bootstrap an empty file from the example so the operator gets the
# documented hint comments on first run. Idempotent.
bootstrap_env_file() {
  if [[ -f "$ENV_FILE" ]]; then return; fi
  local example="$SCRIPT_DIR/.deploy.env.example"
  if [[ -f "$example" ]]; then
    info "No $ENV_FILE yet — seeding from .deploy.env.example."
    cp "$example" "$ENV_FILE"
  else
    : > "$ENV_FILE"
  fi
}

mode_list() {
  echo -e "${BOLD}=== Media Server Pro deploy config — knob inventory ===${RESET}"
  echo "File: $ENV_FILE"
  for key in "${KNOB_ORDER[@]}"; do
    print_knob "$key"
  done
  echo ""
}

mode_only() {
  if [[ -z "${KNOB_DESCRIPTION[$ONLY_KEY]+x}" ]]; then
    die "Unknown knob: $ONLY_KEY (run --list to see all)"
  fi
  bootstrap_env_file
  prompt_knob "$ONLY_KEY"
  success "Updated $ENV_FILE (action: $KNOB_PROMPT_RESULT)"
}

mode_quiet() {
  bootstrap_env_file
  local applied=0
  for key in "${KNOB_ORDER[@]}"; do
    local current
    current="$(read_env_value "$ENV_FILE" "$key")"
    if [[ -z "$current" ]] && [[ -n "${KNOB_DEFAULT[$key]:-}" ]]; then
      upsert_env_var "$ENV_FILE" "$key" "${KNOB_DEFAULT[$key]}"
      applied=$((applied + 1))
    fi
  done
  if [[ $applied -gt 0 ]]; then
    success "Applied $applied default(s) to $ENV_FILE"
  else
    info "No defaults to apply — every knob already set."
  fi
}

# walk_keys filters KNOB_ORDER to only those that should be prompted
# in the current mode:
#   walk   — only knobs is_new_knob says haven't been seen yet.
#   review — every knob in the registry, regardless of state.
walk_keys() {
  local mode="$1" key
  for key in "${KNOB_ORDER[@]}"; do
    case "$mode" in
      review)
        printf '%s\n' "$key"
        ;;
      walk)
        if is_new_knob "$ENV_FILE" "$key"; then
          printf '%s\n' "$key"
        fi
        ;;
    esac
  done
}

mode_walk_inner() {
  local mode="$1"
  if [[ ! -t 0 ]] || [[ ! -t 1 ]]; then
    warn "Not a TTY — falling back to --quiet (apply defaults, no prompts)."
    mode_quiet
    return
  fi
  bootstrap_env_file

  # Resolve the keys we'll prompt for. In default 'walk' mode this is
  # only the never-seen knobs, so subsequent ./deploy.sh runs are
  # silent unless a code release introduced new options.
  local keys=()
  while IFS= read -r k; do keys+=("$k"); done < <(walk_keys "$mode")

  if [[ ${#keys[@]} -eq 0 ]]; then
    if [[ "$mode" == "walk" ]]; then
      success "No new config knobs since the last deploy — nothing to prompt for."
      # Hint at the recovery path. The user's most likely confusion
      # at this point is "but I haven't filled in X yet" — empty values
      # from a previous Enter-through walk look 'seen' to the
      # prompter but functionally aren't configured.
      local empty_count=0
      local key
      for key in "${KNOB_ORDER[@]}"; do
        local cur
        cur="$(read_env_value "$ENV_FILE" "$key")"
        if [[ -z "$cur" ]] && [[ -n "${KNOB_DESCRIPTION[$key]+x}" ]]; then
          empty_count=$((empty_count + 1))
        fi
      done
      if [[ $empty_count -gt 0 ]]; then
        info "$empty_count knob(s) are present but empty. Run ./deploy-configure.sh --review to walk every knob (e.g. fill in NUXT_PUBLIC_GA_ID, HUGGINGFACE_API_KEY), or --only KEY to set one."
      fi
    else
      success "No knobs to walk."
    fi
    return
  fi

  echo -e "${BOLD}=== Media Server Pro deploy config ===${RESET}"
  echo "File: $ENV_FILE"
  if [[ "$mode" == "walk" ]]; then
    echo -e "${YELLOW}${#keys[@]} new knob(s) since last walk — prompting only for these.${RESET}"
    echo -e "${DIM}Run with --review to re-walk every knob.${RESET}"
  else
    echo -e "${YELLOW}--review: prompting for every knob, even ones already set.${RESET}"
  fi
  echo ""
  echo -e "${DIM}Press Enter to keep the current value, type a new value to update,"
  echo -e "or '-' to clear. Ctrl-C aborts without saving.${RESET}"

  # Group by section so the walk feels structured.
  local current_section=""
  local kept=0 set=0 cleared=0 accepted=0
  local key
  for key in "${keys[@]}"; do
    local section="${KNOB_SECTION[$key]:-Other}"
    if [[ "$section" != "$current_section" ]]; then
      echo ""
      echo -e "${BOLD}── ${section} ──────────────────────────────────${RESET}"
      current_section="$section"
    fi
    prompt_knob "$key"
    case "$KNOB_PROMPT_RESULT" in
      kept)              kept=$((kept + 1)) ;;
      set)               set=$((set + 1)) ;;
      cleared)           cleared=$((cleared + 1)) ;;
      accepted-default)  accepted=$((accepted + 1)) ;;
    esac
  done

  echo ""
  success "Done. kept=$kept set=$set cleared=$cleared accepted-default=$accepted → $ENV_FILE"
}

mode_walk()   { mode_walk_inner walk; }
mode_review() { mode_walk_inner review; }

mode_set() {
  bootstrap_env_file
  local pair key val applied=0
  for pair in "${SET_PAIRS[@]}"; do
    key="${pair%%=*}"
    val="${pair#*=}"
    if [[ -z "${KNOB_DESCRIPTION[$key]+x}" ]]; then
      die "Unknown knob: $key (run --list to see all)"
    fi
    upsert_env_var "$ENV_FILE" "$key" "$val"
    applied=$((applied + 1))
    local sensitive="${KNOB_SENSITIVE[$key]:-}"
    if [[ "$sensitive" == "true" ]]; then
      info "$key = (value hidden)"
    else
      info "$key = $val"
    fi
  done
  success "Wrote $applied knob(s) to $ENV_FILE"
}

case "$MODE" in
  walk)   mode_walk ;;
  review) mode_review ;;
  only)   mode_only ;;
  set)    mode_set ;;
  list)   mode_list ;;
  quiet)  mode_quiet ;;
esac
