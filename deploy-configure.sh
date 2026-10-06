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
#                                          # (e.g. --set FEATURE_DOWNLOADER=true).
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

# strip_control_chars VAL → echoes VAL with terminal escape sequences and other
# control characters removed. A plain `read` (no readline) captures arrow keys
# as raw bytes — e.g. pressing → while typing a URL stores https://…\x1b[C\x1b[D…
# which downstream parsers (rclone, curl) reject with "invalid control
# character". Strips ANSI CSI/SS3 sequences first (so the trailing [C/[D letters
# go with their ESC), then any remaining control bytes.
strip_control_chars() {
  local esc
  esc=$(printf '\033')
  printf '%s' "$1" | sed -E "s/${esc}(\[|O)[0-9;?]*[A-Za-z~]//g" | tr -d '[:cntrl:]'
}

# read_env_value FILE KEY → echoes the current uncommented value (or
# empty string). Considers only lines matching `^[[:space:]]*KEY=`;
# strips surrounding double quotes.
read_env_value() {
  local file="$1" key="$2"
  [[ ! -f "$file" ]] && { echo ""; return; }
  # Last uncommented assignment wins (matches bash sourcing semantics).
  local val
  val=$(grep -E "^[[:space:]]*${key}=" "$file" 2>/dev/null | tail -n 1 | cut -d= -f2-)
  # Undo the quoting quote_env_value applies ('…' with '\'' for an embedded
  # single quote), or strip a plain "…" pair. Display-only — the load-bearing
  # value comes from deploy.sh sourcing the file, where bash decodes it.
  if [[ ${#val} -ge 2 && "${val:0:1}" == "'" && "${val: -1}" == "'" ]]; then
    val="${val:1:${#val}-2}"
    val="${val//\'\\\'\'/\'}"
  elif [[ ${#val} -ge 2 && "${val:0:1}" == '"' && "${val: -1}" == '"' ]]; then
    val="${val:1:${#val}-2}"
  fi
  printf '%s' "$val"
}

# quote_env_value KEY VAL — VAL as it must appear after "KEY=" so that
# deploy.sh's `source .deploy.env` reads back exactly VAL. A value made only of
# characters bash takes literally in an assignment is written bare — `$` and
# braces included, so KEY_FILE=$HOME/.ssh/id_ed25519 keeps its intended
# expansion. Anything else (whitespace ; & | < > ( ) ` # quotes [ ] \ …) is
# single-quoted, with embedded single quotes as '\'': unquoted, those split the
# line, run part of it as a command, or — as a typed "fal;se[se" once did —
# make the whole file fail to parse, breaking every later deploy. Sensitive
# values are always single-quoted.
quote_env_value() {
  local key="$1" val="$2"
  local bare_re='^[A-Za-z0-9_./:,@%+=~${}-]+$'
  [[ -z "$val" ]] && return
  if [[ "${KNOB_SENSITIVE[$key]:-}" != "true" ]] && [[ "$val" =~ $bare_re ]]; then
    printf '%s' "$val"
  else
    printf "'%s'" "${val//\'/\'\\\'\'}"
  fi
}

# knob_kind KEY → "bool" or "int" when the registry default says so (true/false,
# or a whole number), "" otherwise.
knob_kind() {
  local def="${KNOB_DEFAULT[$1]:-}"
  case "$def" in
    true|false) echo bool ;;
    *) [[ "$def" =~ ^[0-9]+$ ]] && echo int ;;
  esac
  return 0
}

# knob_value_error KEY VAL → why VAL can't be used for KEY, or nothing when it
# can. Mirrors the server's parsing (internal/config/env_helpers.go: booleans
# true/false/yes/no/on/off/1/0, integers via strconv.Atoi), so a typo is caught
# at the prompt instead of being ignored at startup — or breaking the file.
knob_value_error() {
  local key="$1" val="$2"
  case "$(knob_kind "$key")" in
    bool) [[ "${val,,}" =~ ^(true|false|yes|no|on|off|1|0)$ ]] || echo "expected true or false" ;;
    int)  [[ "$val" =~ ^[+-]?[0-9]+$ ]] || echo "expected a whole number" ;;
  esac
  return 0
}

# normalize_knob_value KEY VAL → the value to store. Booleans become exactly
# true/false: deploy.sh compares vps-scope flags such as HIDRIVE_ENABLED
# literally against "true".
normalize_knob_value() {
  local key="$1" val="$2"
  if [[ "$(knob_kind "$key")" == "bool" ]]; then
    case "${val,,}" in
      true|yes|on|1) val="true" ;;
      *) val="false" ;;
    esac
  fi
  printf '%s' "$val"
}

# check_env_syntax FILE — fail loudly, naming the offending lines (never their
# values), when FILE is not valid shell. deploy.sh `source`s it, so a single bad
# line otherwise stops every deploy with a cryptic parse error.
check_env_syntax() {
  local file="$1" n=0 line key
  [[ -f "$file" ]] || return 0
  bash -n "$file" 2>/dev/null && return 0
  echo -e "${RED}[configure] ERROR:${RESET} $file is not valid shell, so deploy.sh can't load it." >&2
  while IFS= read -r line || [[ -n "$line" ]]; do
    n=$((n + 1))
    [[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
    if ! bash -n <<<"$line" 2>/dev/null; then
      key="${line%%=*}"
      echo "  line $n: ${key:0:60} — fix or quote this value (./deploy-configure.sh --only ${key:0:60})" >&2
    fi
  done < "$file"
  return 1
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

# append_comment_hint FILE KEY VALUE — append a commented hint line so
# is_new_knob() considers KEY "seen" but read_env_value() returns empty
# (commented lines are never read as live assignments). Used when the
# operator pressed Enter on a never-seen knob — we don't forward
# anything, the VPS .env keeps its current value, and the ★ NEW tag
# clears on the next walk. Idempotent: if a commented hint already
# exists it isn't duplicated; live (uncommented) assignments are left
# untouched.
append_comment_hint() {
  local file="$1" key="$2" val="$3"
  if [[ ! -f "$file" ]]; then
    : > "$file"
  fi
  # If any KEY= line already exists (live or commented) we're done.
  if grep -qE "(^|[[:space:]#])${key}=" "$file" 2>/dev/null; then
    return
  fi
  if [[ -s "$file" ]] && [[ "$(tail -c 1 "$file" | wc -l)" -eq 0 ]]; then
    printf '\n' >> "$file"
  fi
  if [[ -n "$val" ]]; then
    printf '# %s=%s\n' "$key" "$val" >> "$file"
  else
    printf '# %s=\n' "$key" >> "$file"
  fi
}

# upsert_env_var FILE KEY VALUE — replace or append. Only matches
# uncommented assignments; commented-out hint lines (# KEY=…) are
# preserved as documentation.
upsert_env_var() {
  local file="$1" key="$2" val="$3"
  if [[ ! -f "$file" ]]; then
    : > "$file"
  fi
  local out
  out="$(quote_env_value "$key" "$val")"
  local tmp
  tmp="$(mktemp)"
  local found=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" =~ ^[[:space:]]*${key}= ]]; then
      printf '%s=%s\n' "$key" "$out" >> "$tmp"
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
    printf '%s=%s\n' "$key" "$out" >> "$tmp"
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

  local what="new value"
  case "$(knob_kind "$key")" in
    bool) what="true/false" ;;
    int)  what="a whole number" ;;
  esac
  local hint
  if [[ -n "$current" ]]; then
    hint="Enter = keep, '-' = clear, or ${what}"
  else
    hint="Enter = skip (VPS .env stays as-is), or ${what} to override"
  fi
  local reply="" problem=""
  local sensitive="${KNOB_SENSITIVE[$key]:-}"
  # Re-ask until the value is usable: a typo written into .deploy.env is at
  # best ignored by the server and at worst breaks loading the file.
  while :; do
    echo -en "  ${CYAN}>${RESET} ${DIM}(${hint})${RESET} "
    reply=""
    if [[ "$sensitive" == "true" ]]; then
      # Silent read so the secret doesn't echo to the terminal.
      read -r -s reply </dev/tty || true
      echo ""
    else
      read -r reply </dev/tty || true
    fi
    reply="${reply//$'\r'/}"
    # Drop arrow-key/escape bytes a non-readline `read` captures during editing.
    reply="$(strip_control_chars "$reply")"
    if [[ "$sensitive" != "true" ]]; then
      # Stray surrounding spaces are never meant (secrets are left untouched).
      reply="${reply#"${reply%%[![:space:]]*}"}"
      reply="${reply%"${reply##*[![:space:]]}"}"
    fi
    [[ -z "$reply" || "$reply" == "-" ]] && break
    problem="$(knob_value_error "$key" "$reply")"
    [[ -z "$problem" ]] && break
    echo -e "    ${RED}✗ ${problem}${RESET} ${DIM}— try again${RESET}"
  done
  if [[ -n "$reply" && "$reply" != "-" ]]; then
    reply="$(normalize_knob_value "$key" "$reply")"
  fi

  if [[ -z "$reply" ]]; then
    # SAFE-BY-DEFAULT: pressing Enter on a never-seen knob marks it as
    # seen WITHOUT writing the default — so the forward step skips it
    # and whatever is already on the VPS .env stays put. Without this
    # rule the knob system would silently clobber hand-edited VPS
    # values with hard-coded registry defaults on the first walk
    # (which is exactly what happened on the first MSP-4 rollout).
    # The line is written as a commented hint so is_new_knob clears
    # next time but read_env_value returns empty.
    if is_new_knob "$ENV_FILE" "$key"; then
      local hint_val="${KNOB_DEFAULT[$key]:-}"
      append_comment_hint "$ENV_FILE" "$key" "$hint_val"
      if [[ -n "$hint_val" ]]; then
        echo -e "    ${DIM}skipped (VPS keeps current value; registry default was ${hint_val})${RESET}"
      else
        echo -e "    ${DIM}skipped (VPS keeps current value)${RESET}"
      fi
      KNOB_PROMPT_RESULT="skipped"
      return
    fi
    echo -e "    ${DIM}kept${RESET}"
    KNOB_PROMPT_RESULT="kept"
    return
  fi
  if [[ "$reply" == "-" ]]; then
    upsert_env_var "$ENV_FILE" "$key" ""
    echo -e "    ${YELLOW}cleared (VPS keeps current value — empty values are not forwarded)${RESET}"
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

# Create the file on first run. The walk itself writes every knob (as a value
# or a commented hint) with its description shown at the prompt, so there is
# no separate template to seed from. Idempotent.
bootstrap_env_file() {
  if [[ -f "$ENV_FILE" ]]; then return; fi
  info "No $ENV_FILE yet — creating it; the walk below fills it in."
  {
    echo "# Media Server Pro deploy configuration — written by ./deploy-configure.sh"
    echo "# (./deploy.sh --configure). Local only, gitignored. See DEPLOY.md."
  } > "$ENV_FILE"
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
  local marked=0
  for key in "${KNOB_ORDER[@]}"; do
    if is_new_knob "$ENV_FILE" "$key"; then
      append_comment_hint "$ENV_FILE" "$key" "${KNOB_DEFAULT[$key]:-}"
      marked=$((marked + 1))
    fi
  done
  if [[ $marked -gt 0 ]]; then
    success "Marked $marked new knob(s) as seen (commented hints) in $ENV_FILE. None are forwarded — VPS .env keeps its current values."
  else
    info "No new knobs since last walk."
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
  echo -e "${DIM}SAFE-BY-DEFAULT: Enter on a never-seen knob marks it 'seen' but does"
  echo -e "NOT push the registry default to the VPS — your VPS .env keeps its"
  echo -e "current value. Type a value to override. '-' clears (also not pushed)."
  echo -e "Ctrl-C aborts without saving.${RESET}"

  if [[ "$mode" == "walk" ]] && [[ ${#keys[@]} -gt 10 ]]; then
    echo ""
    echo -e "${YELLOW}Many new knobs: each section asks first — Enter walks it, 's' skips it,"
    echo -e "'a' skips every remaining section. Skipped knobs are marked seen without"
    echo -e "changing anything on the VPS; walk them later with --review or --only KEY.${RESET}"
  fi

  # Group by section so the walk feels structured.
  local current_section=""
  local kept=0 set=0 cleared=0 skipped=0
  local skip_section=0 skip_all=0
  local key idx=0
  for key in "${keys[@]}"; do
    idx=$((idx + 1))
    local section="${KNOB_SECTION[$key]:-Other}"
    if [[ $skip_all -eq 1 ]]; then
      append_comment_hint "$ENV_FILE" "$key" "${KNOB_DEFAULT[$key]:-}"
      skipped=$((skipped + 1))
      continue
    fi
    if [[ "$section" != "$current_section" ]]; then
      echo ""
      echo -e "${BOLD}── ${section} ──────────────────────────────────${RESET}"
      current_section="$section"
      skip_section=0
      # Offer a per-section skip in walk mode when the section has several
      # new knobs (count the run of same-section keys starting here).
      if [[ "$mode" == "walk" ]]; then
        local n=0 j
        for j in "${keys[@]:$((idx - 1))}"; do
          [[ "${KNOB_SECTION[$j]:-Other}" == "$section" ]] || break
          n=$((n + 1))
        done
        if [[ $n -gt 1 ]]; then
          echo -en "  ${CYAN}>${RESET} ${n} new knob(s) in this section. ${DIM}(Enter = walk, s = skip section, a = skip all remaining)${RESET} "
          local choice=""
          read -r choice </dev/tty || true
          choice="$(strip_control_chars "${choice//$'\r'/}")"
          case "${choice,,}" in
            s) skip_section=1 ;;
            a) skip_all=1 ;;
          esac
        fi
      fi
    fi
    if [[ $skip_section -eq 1 ]] || [[ $skip_all -eq 1 ]]; then
      append_comment_hint "$ENV_FILE" "$key" "${KNOB_DEFAULT[$key]:-}"
      skipped=$((skipped + 1))
      continue
    fi
    prompt_knob "$key"
    case "$KNOB_PROMPT_RESULT" in
      kept)     kept=$((kept + 1)) ;;
      set)      set=$((set + 1)) ;;
      cleared)  cleared=$((cleared + 1)) ;;
      skipped)  skipped=$((skipped + 1)) ;;
    esac
  done

  echo ""
  success "Done. kept=$kept set=$set cleared=$cleared skipped=$skipped → $ENV_FILE"
  if [[ $set -eq 0 ]] && [[ $cleared -eq 0 ]]; then
    info "Nothing was forwarded — every prompt was Enter/skip. VPS .env untouched."
  fi
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
    if [[ -n "$val" ]]; then
      local problem
      problem="$(knob_value_error "$key" "$val")"
      [[ -z "$problem" ]] || die "$key: $problem"
      val="$(normalize_knob_value "$key" "$val")"
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

# deploy.sh sources this file next: point at any line that would stop it
# (including hand edits) now, rather than leave a cryptic parse error.
check_env_syntax "$ENV_FILE" || exit 1
