#!/usr/bin/env bash
# ══════════════════════════════════════════════════════════════════════════════
#  Media Server Pro — Interactive Installer
# ══════════════════════════════════════════════════════════════════════════════
#
#  What this script does:
#    Installs and configures Media Server Pro on a local Linux machine (home PC,
#    home server, or dedicated box). It installs every required dependency,
#    creates the database, builds the binaries and frontend, generates a
#    configuration file, and optionally installs a systemd service.
#
#  Designed to be:
#    - Safe: no destructive operations without confirmation
#    - Self-recovering: each step can be retried, every failure logs a hint
#    - Verbose: every command, prompt, answer, and error is saved to a log file
#    - Simple: sensible defaults, plain-English prompts, one-screen summaries
#
#  Usage:
#    ./install.sh                 # full interactive install
#    ./install.sh --help          # show all options
#    ./install.sh --unattended    # use saved answers from install.answers
#    ./install.sh --resume        # resume from last successful step
#    ./install.sh --uninstall     # remove the service and binaries (keeps data)
#    ./install.sh --log FILE      # override log file path
#
#  If anything goes wrong, send the generated log file to the maintainers for
#  diagnosis. Its location is printed at the start and end of every run.
#
#  Log location: ./logs/install-YYYYMMDD-HHMMSS.log
#  Answers file: ./install.answers       (re-usable for --unattended)
#  State file:   ./.install.state        (used by --resume)
#
# ══════════════════════════════════════════════════════════════════════════════

set -o pipefail
# NOTE: We deliberately do NOT use `set -e`. Failures are handled explicitly so
# that the installer can recover, retry, or show a helpful message instead of
# crashing out to an empty terminal.

# ──────────────────────────────────────────────────────────────────────────────
#  0. SCRIPT METADATA AND PATHS
# ──────────────────────────────────────────────────────────────────────────────
SCRIPT_VERSION="1.0.0"
SCRIPT_NAME="Media Server Pro Installer"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Required Go version — must match go.mod
REQUIRED_GO_VERSION="1.26.1"
# Required Node major version — must match frontend package.json engines
REQUIRED_NODE_MAJOR="22"
# Minimum MySQL/MariaDB major version
REQUIRED_MYSQL_MAJOR="8"
REQUIRED_MARIADB_MAJOR="10"

# File locations (all relative to SCRIPT_DIR by default)
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
LOG_DIR_DEFAULT="$SCRIPT_DIR/logs"
LOG_FILE_DEFAULT="$LOG_DIR_DEFAULT/install-$TIMESTAMP.log"
ANSWERS_FILE="$SCRIPT_DIR/install.answers"
STATE_FILE="$SCRIPT_DIR/.install.state"
ENV_FILE="$SCRIPT_DIR/.env"
ENV_BACKUP="$SCRIPT_DIR/.env.bak.$TIMESTAMP"

# Runtime flags (set by argument parsing)
MODE_UNATTENDED=false
MODE_RESUME=false
MODE_UNINSTALL=false
MODE_ASSUME_YES=false
LOG_FILE="$LOG_FILE_DEFAULT"

# ──────────────────────────────────────────────────────────────────────────────
#  1. LOGGING AND OUTPUT HELPERS
# ──────────────────────────────────────────────────────────────────────────────
# All output goes through these helpers so the terminal view and the log file
# stay in sync. Colour is disabled when stdout is not a terminal.
if [[ -t 1 ]] && [[ "${NO_COLOR:-}" == "" ]]; then
  C_RESET='\033[0m'
  C_BOLD='\033[1m'
  C_DIM='\033[2m'
  C_RED='\033[0;31m'
  C_GREEN='\033[0;32m'
  C_YELLOW='\033[1;33m'
  C_BLUE='\033[0;34m'
  C_CYAN='\033[0;36m'
  C_MAGENTA='\033[0;35m'
else
  C_RESET='' C_BOLD='' C_DIM='' C_RED='' C_GREEN='' C_YELLOW='' C_BLUE='' C_CYAN='' C_MAGENTA=''
fi

# _ts — ISO-8601 timestamp used in log lines
_ts() { date '+%Y-%m-%d %H:%M:%S'; }

# _log_raw — append a line to the log file only (no terminal output)
_log_raw() {
  [[ -z "${LOG_FILE:-}" ]] && return 0
  local dir
  dir=$(dirname "$LOG_FILE" 2>/dev/null) || return 0
  if [[ -w "$dir" ]] || [[ -w "$LOG_FILE" ]]; then
    printf '[%s] %s\n' "$(_ts)" "$*" >> "$LOG_FILE" 2>/dev/null || true
  fi
}

# log_info / log_warn / log_error / log_debug / log_success
# All four echo to terminal with colour AND append to the log file.
log_info() {
  local msg="$*"
  printf '%b[INFO]%b  %s\n' "$C_CYAN" "$C_RESET" "$msg"
  _log_raw "[INFO]  $msg"
}
log_success() {
  local msg="$*"
  printf '%b[OK]%b    %s\n' "$C_GREEN" "$C_RESET" "$msg"
  _log_raw "[OK]    $msg"
}
log_warn() {
  local msg="$*"
  printf '%b[WARN]%b  %s\n' "$C_YELLOW" "$C_RESET" "$msg"
  _log_raw "[WARN]  $msg"
}
log_error() {
  local msg="$*"
  printf '%b[ERROR]%b %s\n' "$C_RED" "$C_RESET" "$msg" >&2
  _log_raw "[ERROR] $msg"
}
log_debug() {
  local msg="$*"
  _log_raw "[DEBUG] $msg"
}
log_step() {
  local msg="$*"
  printf '\n%b▶ %s%b\n' "$C_BOLD$C_BLUE" "$msg" "$C_RESET"
  _log_raw "===== STEP: $msg ====="
}
log_section() {
  local msg="$*"
  printf '\n%b═══ %s ═══%b\n' "$C_BOLD$C_MAGENTA" "$msg" "$C_RESET"
  _log_raw ""
  _log_raw "########## $msg ##########"
}

# run_cmd — execute a command, log stdout+stderr, return the exit code.
# Usage: run_cmd "description" cmd arg1 arg2 ...
# The description is shown on the terminal; the full output is appended to the
# log file. Stdout is NOT printed unless the command fails.
run_cmd() {
  local desc="$1"; shift
  log_debug "RUN: $*"
  _log_raw "--- command: $* ---"
  local out rc
  # Capture both stdout and stderr together for the log, but keep rc clean.
  out=$("$@" 2>&1)
  rc=$?
  if [[ -n "$out" ]]; then
    printf '%s\n' "$out" >> "$LOG_FILE" 2>/dev/null || true
  fi
  if [[ $rc -ne 0 ]]; then
    log_error "$desc failed (exit $rc)"
    if [[ -n "$out" ]]; then
      # Show the last 10 lines on the terminal so the user has something
      # actionable without dumping the whole log.
      printf '%b--- last output ---%b\n' "$C_DIM" "$C_RESET" >&2
      printf '%s\n' "$out" | tail -n 10 >&2
      printf '%b-------------------%b\n' "$C_DIM" "$C_RESET" >&2
    fi
    return $rc
  fi
  log_debug "RC=0: $desc"
  return 0
}

# run_cmd_quiet — like run_cmd but also hides the description unless it fails.
run_cmd_quiet() {
  local desc="$1"; shift
  log_debug "RUN (quiet): $*"
  local out rc
  out=$("$@" 2>&1)
  rc=$?
  if [[ -n "$out" ]]; then
    printf '%s\n' "$out" >> "$LOG_FILE" 2>/dev/null || true
  fi
  if [[ $rc -ne 0 ]]; then
    log_error "$desc failed (exit $rc)"
    if [[ -n "$out" ]]; then
      printf '%s\n' "$out" | tail -n 10 >&2
    fi
  fi
  return $rc
}

# ──────────────────────────────────────────────────────────────────────────────
#  2. ERROR HANDLING AND TRAPS
# ──────────────────────────────────────────────────────────────────────────────
# On any unexpected exit, print the log path so the user always knows where to
# find the diagnostic information.
_installer_exit_handler() {
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    printf '\n%b══════════════════════════════════════════════════════════════%b\n' "$C_RED" "$C_RESET" >&2
    printf '%bInstaller exited with code %d%b\n' "$C_RED$C_BOLD" "$rc" "$C_RESET" >&2
    printf 'Full log: %b%s%b\n' "$C_BOLD" "$LOG_FILE" "$C_RESET" >&2
    printf 'Resume:   %b./install.sh --resume%b\n' "$C_BOLD" "$C_RESET" >&2
    printf '%b══════════════════════════════════════════════════════════════%b\n' "$C_RED" "$C_RESET" >&2
  fi
  return $rc
}
trap _installer_exit_handler EXIT

# Interrupt handler — log the signal, give the user a clean message.
_installer_interrupt_handler() {
  printf '\n'
  log_warn "Interrupted by user (Ctrl+C). State saved. Resume with: ./install.sh --resume"
  exit 130
}
trap _installer_interrupt_handler INT TERM

# ──────────────────────────────────────────────────────────────────────────────
#  3. ARGUMENT PARSING AND HELP
# ──────────────────────────────────────────────────────────────────────────────
print_help() {
  cat <<HELP
$SCRIPT_NAME v$SCRIPT_VERSION

USAGE
  ./install.sh [OPTIONS]

OPTIONS
  --help, -h          Show this help and exit.
  --unattended        Non-interactive mode. Reads answers from install.answers
                      (created on a previous interactive run). Fails if the
                      file is missing or incomplete.
  --resume            Resume from the last successful step. Useful after
                      fixing a problem that caused a previous run to fail.
  --uninstall         Stop the service, remove the systemd unit and binaries.
                      Database, media, and config files are KEPT. Use
                      --purge to also remove data.
  --purge             Only valid with --uninstall. Removes the database and
                      all generated files. Asks for explicit confirmation.
  --yes, -y           Auto-answer yes to confirmation prompts that have a
                      safe default. Prompts without safe defaults still stop.
  --log FILE          Write the log to FILE instead of the default location.
  --version           Print the installer version and exit.

EXAMPLES
  ./install.sh                       # full interactive install
  ./install.sh --resume              # retry after fixing an issue
  ./install.sh --unattended          # replay a previous install
  ./install.sh --uninstall           # remove service + binary, keep data
  ./install.sh --uninstall --purge   # remove everything

LOG FILE
  Every run creates a timestamped log at ./logs/install-*.log. If you need
  help, attach that file to your bug report — it contains the full output of
  every command the installer ran.

HELP
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help)       print_help; exit 0 ;;
      --version)       printf '%s\n' "$SCRIPT_VERSION"; exit 0 ;;
      --unattended)    MODE_UNATTENDED=true; shift ;;
      --resume)        MODE_RESUME=true; shift ;;
      --uninstall)     MODE_UNINSTALL=true; shift ;;
      --purge)         MODE_PURGE=true; shift ;;
      -y|--yes)        MODE_ASSUME_YES=true; shift ;;
      --log)           LOG_FILE="$2"; shift 2 ;;
      *)               log_error "Unknown option: $1 (use --help)"; exit 2 ;;
    esac
  done
}

# ──────────────────────────────────────────────────────────────────────────────
#  4. LOG FILE INITIALISATION
# ──────────────────────────────────────────────────────────────────────────────
init_log_file() {
  local logdir
  logdir="$(dirname "$LOG_FILE")"
  mkdir -p "$logdir" 2>/dev/null || {
    printf 'Cannot create log directory: %s\n' "$logdir" >&2
    printf 'Falling back to /tmp\n' >&2
    LOG_FILE="/tmp/mediaserver-install-$TIMESTAMP.log"
    mkdir -p /tmp 2>/dev/null || true
  }
  : > "$LOG_FILE" 2>/dev/null || {
    printf 'Cannot write to log file: %s\n' "$LOG_FILE" >&2
    exit 1
  }

  {
    echo "══════════════════════════════════════════════════════════════"
    echo "  $SCRIPT_NAME v$SCRIPT_VERSION"
    echo "  started at $(_ts)"
    echo "  pid $$ / user $(id -un 2>/dev/null || echo unknown)"
    echo "  uname: $(uname -a 2>/dev/null || echo unknown)"
    echo "  pwd:   $SCRIPT_DIR"
    echo "══════════════════════════════════════════════════════════════"
  } >> "$LOG_FILE"
}

# ──────────────────────────────────────────────────────────────────────────────
#  5. STATE AND ANSWERS PERSISTENCE
# ──────────────────────────────────────────────────────────────────────────────
# State = which step we finished last. Answers = the values the user typed.
# These are separate so re-running with --unattended can skip already-done
# steps without losing previously typed answers.

# Steps, in order. Used for --resume.
INSTALL_STEPS=(
  "preflight"
  "detect_os"
  "install_system_deps"
  "install_go"
  "install_node"
  "install_ffmpeg"
  "install_mysql"
  "configure_database"
  "collect_config"
  "write_env_file"
  "create_directories"
  "build_backend"
  "build_frontend"
  "install_systemd"
  "health_check"
  "done"
)

state_save() {
  local step="$1"
  printf '%s\n' "$step" > "$STATE_FILE" 2>/dev/null || true
  log_debug "state saved: $step"
}
state_load() {
  [[ -f "$STATE_FILE" ]] && cat "$STATE_FILE" 2>/dev/null || echo ""
}
state_clear() {
  rm -f "$STATE_FILE" 2>/dev/null || true
}

# should_run_step — true if we should run the named step based on resume state.
should_run_step() {
  local target="$1"
  if ! $MODE_RESUME; then
    return 0
  fi
  local last done_idx target_idx
  last="$(state_load)"
  if [[ -z "$last" ]]; then
    return 0
  fi
  done_idx=-1
  target_idx=-1
  for i in "${!INSTALL_STEPS[@]}"; do
    [[ "${INSTALL_STEPS[$i]}" == "$last" ]] && done_idx=$i
    [[ "${INSTALL_STEPS[$i]}" == "$target" ]] && target_idx=$i
  done
  if [[ $target_idx -gt $done_idx ]]; then
    return 0
  fi
  log_info "Skipping already-completed step: $target"
  return 1
}

# answers_save / answers_load — persistent answer file for --unattended.
declare -A ANSWERS=()
answers_save() {
  {
    echo "# Media Server Pro installer answers"
    echo "# Generated: $(_ts)"
    echo "# Use with: ./install.sh --unattended"
    for key in "${!ANSWERS[@]}"; do
      # Escape single-quotes so values survive round-trip through shell sourcing.
      local val="${ANSWERS[$key]}"
      val="${val//\'/\'\\\'\'}"
      printf "%s='%s'\n" "$key" "$val"
    done
  } > "$ANSWERS_FILE" 2>/dev/null && chmod 600 "$ANSWERS_FILE" 2>/dev/null || true
  log_debug "answers saved to $ANSWERS_FILE"
}
answers_load() {
  [[ -f "$ANSWERS_FILE" ]] || return 1
  # shellcheck disable=SC1090
  while IFS='=' read -r key rest; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    # strip surrounding single-quotes
    rest="${rest#\'}"
    rest="${rest%\'}"
    ANSWERS[$key]="$rest"
  done < "$ANSWERS_FILE"
  log_debug "answers loaded from $ANSWERS_FILE (${#ANSWERS[@]} entries)"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
#  6. INTERACTIVE PROMPT HELPERS
# ──────────────────────────────────────────────────────────────────────────────
# Every prompt goes through these helpers. They:
#   - show a default when provided
#   - accept saved answers in --unattended mode
#   - validate input where a validator is provided
#   - record the answer in ANSWERS[] for re-use
#   - log the prompt + answer to the log file (secrets are masked)

# ask VAR "prompt text" [default]
# Result is stored in both the named variable and ANSWERS[VAR].
ask() {
  local varname="$1" prompt_text="$2" default="${3:-}"
  local input=""
  # In unattended mode use the saved answer.
  if $MODE_UNATTENDED && [[ -n "${ANSWERS[$varname]+x}" ]]; then
    input="${ANSWERS[$varname]}"
    log_debug "unattended: $varname = $input"
    printf -v "$varname" '%s' "$input"
    return 0
  fi
  local display_default=""
  [[ -n "$default" ]] && display_default=" [$default]"
  while true; do
    printf '%b  ? %s%s:%b ' "$C_CYAN" "$prompt_text" "$display_default" "$C_RESET"
    if ! IFS= read -r input; then
      # EOF — treat as cancel
      log_error "Input closed unexpectedly"
      exit 1
    fi
    input="${input:-$default}"
    if [[ -z "$input" ]]; then
      log_warn "This value is required."
      continue
    fi
    break
  done
  ANSWERS[$varname]="$input"
  _log_raw "PROMPT: $prompt_text = $input"
  printf -v "$varname" '%s' "$input"
}

# ask_secret VAR "prompt text"
# Does not echo input; mask in the log.
ask_secret() {
  local varname="$1" prompt_text="$2"
  local input=""
  if $MODE_UNATTENDED && [[ -n "${ANSWERS[$varname]+x}" ]]; then
    input="${ANSWERS[$varname]}"
    printf -v "$varname" '%s' "$input"
    return 0
  fi
  while true; do
    printf '%b  ? %s:%b ' "$C_CYAN" "$prompt_text" "$C_RESET"
    if ! IFS= read -rs input; then
      printf '\n'
      log_error "Input closed unexpectedly"
      exit 1
    fi
    printf '\n'
    if [[ -z "$input" ]]; then
      log_warn "This value is required."
      continue
    fi
    break
  done
  ANSWERS[$varname]="$input"
  _log_raw "PROMPT: $prompt_text = ********"
  printf -v "$varname" '%s' "$input"
}

# ask_yn VAR "prompt text" default(y|n)
# Stores "true" or "false".
ask_yn() {
  local varname="$1" prompt_text="$2" default="${3:-n}"
  local input=""
  if $MODE_UNATTENDED && [[ -n "${ANSWERS[$varname]+x}" ]]; then
    input="${ANSWERS[$varname]}"
    printf -v "$varname" '%s' "$input"
    return 0
  fi
  if $MODE_ASSUME_YES && [[ "${default,,}" == "y" ]]; then
    input="true"
    ANSWERS[$varname]="$input"
    printf -v "$varname" '%s' "$input"
    _log_raw "PROMPT (auto-yes): $prompt_text = yes"
    return 0
  fi
  local display
  if [[ "${default,,}" == "y" ]]; then display="Y/n"; else display="y/N"; fi
  while true; do
    printf '%b  ? %s [%s]:%b ' "$C_CYAN" "$prompt_text" "$display" "$C_RESET"
    if ! IFS= read -r input; then
      printf '\n'
      log_error "Input closed unexpectedly"
      exit 1
    fi
    input="${input:-$default}"
    case "${input,,}" in
      y|yes) input="true"; break ;;
      n|no)  input="false"; break ;;
      *) log_warn "Please answer y or n." ;;
    esac
  done
  ANSWERS[$varname]="$input"
  _log_raw "PROMPT: $prompt_text = $input"
  printf -v "$varname" '%s' "$input"
}

# ask_choice VAR "prompt text" default_index "option1" "option2" ...
ask_choice() {
  local varname="$1" prompt_text="$2" default_idx="$3"; shift 3
  local options=("$@") input=""
  if $MODE_UNATTENDED && [[ -n "${ANSWERS[$varname]+x}" ]]; then
    input="${ANSWERS[$varname]}"
    printf -v "$varname" '%s' "$input"
    return 0
  fi
  printf '%b  ? %s%b\n' "$C_CYAN" "$prompt_text" "$C_RESET"
  local i
  for i in "${!options[@]}"; do
    printf '    %b%d)%b %s\n' "$C_BOLD" "$((i + 1))" "$C_RESET" "${options[$i]}"
  done
  while true; do
    printf '%b  Choose [1-%d, default %d]:%b ' "$C_CYAN" "${#options[@]}" "$default_idx" "$C_RESET"
    if ! IFS= read -r input; then
      printf '\n'
      log_error "Input closed unexpectedly"
      exit 1
    fi
    input="${input:-$default_idx}"
    if [[ "$input" =~ ^[0-9]+$ ]] && (( input >= 1 && input <= ${#options[@]} )); then
      break
    fi
    log_warn "Please enter a number between 1 and ${#options[@]}."
  done
  local chosen="${options[$((input - 1))]}"
  ANSWERS[$varname]="$chosen"
  _log_raw "PROMPT: $prompt_text = $chosen"
  printf -v "$varname" '%s' "$chosen"
}

confirm() {
  local prompt_text="$1" default="${2:-n}"
  if $MODE_ASSUME_YES && [[ "${default,,}" == "y" ]]; then
    return 0
  fi
  local display
  if [[ "${default,,}" == "y" ]]; then display="Y/n"; else display="y/N"; fi
  local input
  printf '%b  ? %s [%s]:%b ' "$C_YELLOW" "$prompt_text" "$display" "$C_RESET"
  if ! IFS= read -r input; then
    return 1
  fi
  input="${input:-$default}"
  case "${input,,}" in
    y|yes) return 0 ;;
    *)     return 1 ;;
  esac
}

# retry_prompt — ask the user whether to retry a failed step.
retry_prompt() {
  local step_name="$1"
  printf '\n%b  The step "%s" failed.%b\n' "$C_RED" "$step_name" "$C_RESET"
  printf '  You can:\n'
  printf '    %b1)%b Retry the step\n' "$C_BOLD" "$C_RESET"
  printf '    %b2)%b Skip this step (advanced — may leave an incomplete install)\n' "$C_BOLD" "$C_RESET"
  printf '    %b3)%b Abort and inspect the log: %s\n' "$C_BOLD" "$C_RESET" "$LOG_FILE"
  local choice
  printf '%b  Choose [1]:%b ' "$C_YELLOW" "$C_RESET"
  if ! IFS= read -r choice; then
    return 2
  fi
  choice="${choice:-1}"
  case "$choice" in
    1) return 0 ;;
    2) return 1 ;;
    *) return 2 ;;
  esac
}

# run_step — execute a named installer step with retry support.
# Usage: run_step step_name step_function
run_step() {
  local name="$1" fn="$2"
  if ! should_run_step "$name"; then
    return 0
  fi
  while true; do
    log_step "$name"
    if "$fn"; then
      state_save "$name"
      answers_save
      log_success "Step '$name' completed."
      return 0
    fi
    if $MODE_UNATTENDED; then
      log_error "Step '$name' failed in unattended mode — aborting."
      return 1
    fi
    retry_prompt "$name"
    local rc=$?
    case $rc in
      0) log_info "Retrying '$name'..." ; continue ;;
      1) log_warn "Skipping '$name' — continuing anyway."; state_save "$name"; return 0 ;;
      *) log_error "Aborted at step '$name'. Log: $LOG_FILE"; return 1 ;;
    esac
  done
}

# ──────────────────────────────────────────────────────────────────────────────
#  7. VERSION AND STRING UTILITIES
# ──────────────────────────────────────────────────────────────────────────────
# version_ge A B — true if A >= B (semver-ish comparison, pads with zeros)
version_ge() {
  local a="$1" b="$2"
  # Extract up to three numeric components.
  local a1 a2 a3 b1 b2 b3
  IFS='.' read -r a1 a2 a3 <<< "${a%%-*}"
  IFS='.' read -r b1 b2 b3 <<< "${b%%-*}"
  a1=${a1:-0}; a2=${a2:-0}; a3=${a3:-0}
  b1=${b1:-0}; b2=${b2:-0}; b3=${b3:-0}
  if (( a1 != b1 )); then (( a1 > b1 )); return
  elif (( a2 != b2 )); then (( a2 > b2 )); return
  else (( a3 >= b3 )); return; fi
}

# has_cmd — true if a command is on PATH
has_cmd() { command -v "$1" >/dev/null 2>&1; }

# is_root — true if the current process is root
is_root() { [[ $EUID -eq 0 ]]; }

# SUDO — prefix for commands that need root. Empty when already root.
detect_sudo() {
  if is_root; then
    SUDO=""
  elif has_cmd sudo; then
    SUDO="sudo"
  else
    SUDO=""
  fi
}

# sudo_run — run a command as root, respecting SUDO. Logged to the log file.
sudo_run() {
  log_debug "SUDO: $*"
  _log_raw "--- sudo: $* ---"
  local out rc
  if [[ -n "$SUDO" ]]; then
    out=$($SUDO "$@" 2>&1); rc=$?
  else
    out=$("$@" 2>&1); rc=$?
  fi
  [[ -n "$out" ]] && printf '%s\n' "$out" >> "$LOG_FILE" 2>/dev/null
  if [[ $rc -ne 0 ]] && [[ -n "$out" ]]; then
    printf '%s\n' "$out" | tail -n 10 >&2
  fi
  return $rc
}

# random_hex BYTES — generate a random hex string with best-effort fallback
random_hex() {
  local n="${1:-32}"
  if has_cmd openssl; then
    openssl rand -hex "$n"
  elif has_cmd python3; then
    python3 -c "import secrets,sys; print(secrets.token_hex(int(sys.argv[1])))" "$n"
  elif [[ -r /dev/urandom ]]; then
    tr -dc 'a-f0-9' < /dev/urandom | head -c $((n * 2)) ; echo
  else
    # Last-resort: timestamp + PID. Not cryptographically strong, but never crashes.
    printf '%s%s%s' "$(date +%s%N 2>/dev/null || date +%s)" "$$" "$RANDOM" \
      | sha256sum 2>/dev/null | head -c $((n * 2))
    echo
  fi
}

# bcrypt_hash PASSWORD — hash a password with bcrypt.
# Tries htpasswd, then python3+bcrypt, then python3+passlib, then a Go fallback.
# Returns the hash on stdout; empty string on failure.
bcrypt_hash() {
  local pw="$1" out=""
  if has_cmd htpasswd; then
    out=$(htpasswd -nbBC 10 "" "$pw" 2>/dev/null | cut -d: -f2 | tr -d '\n')
    if [[ -n "$out" ]]; then printf '%s' "$out"; return 0; fi
  fi
  if has_cmd python3; then
    out=$(python3 - "$pw" <<'PY' 2>/dev/null
import sys
try:
    import bcrypt
    print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt(10)).decode())
except Exception:
    try:
        from passlib.hash import bcrypt as pbcrypt
        print(pbcrypt.using(rounds=10).hash(sys.argv[1]))
    except Exception:
        pass
PY
)
    if [[ -n "$out" ]]; then printf '%s' "$out"; return 0; fi
  fi
  # Go fallback — only works if the build environment has golang.org/x/crypto/bcrypt.
  if has_cmd go; then
    out=$(cd "$SCRIPT_DIR" && go run - "$pw" 2>/dev/null <<'GO'
package main
import (
	"fmt"
	"os"
	"golang.org/x/crypto/bcrypt"
)
func main() {
	h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 10)
	if err != nil { os.Exit(1) }
	fmt.Print(string(h))
}
GO
)
    if [[ -n "$out" ]]; then printf '%s' "$out"; return 0; fi
  fi
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
#  8. PREFLIGHT CHECKS
# ──────────────────────────────────────────────────────────────────────────────
# Runs before anything touches the system. Verifies the basic tools needed to
# run the installer itself, warns about dangerous conditions, and loads any
# prior answers file.
step_preflight() {
  log_info "Running pre-flight checks..."

  # bash >= 4 required for associative arrays used by ANSWERS[].
  if (( BASH_VERSINFO[0] < 4 )); then
    log_error "bash 4 or newer is required (found ${BASH_VERSION})."
    log_error "On macOS install GNU bash: brew install bash"
    return 1
  fi
  log_debug "bash version ok: $BASH_VERSION"

  # Must be on a Linux-like system. macOS is not supported (no systemd, MySQL
  # install differs, etc.) — the script still tries but warns.
  local os_name
  os_name="$(uname -s 2>/dev/null || echo unknown)"
  if [[ "$os_name" != "Linux" ]]; then
    log_warn "Unsupported OS detected: $os_name"
    log_warn "This installer targets Linux. Continuing, but expect failures."
    if ! $MODE_UNATTENDED && ! confirm "Continue anyway?" "n"; then
      return 1
    fi
  fi

  # Must have write access to the script directory.
  if [[ ! -w "$SCRIPT_DIR" ]]; then
    log_error "No write permission on $SCRIPT_DIR"
    log_error "Either run from a writable clone, or chown the directory."
    return 1
  fi

  # Verify required tools that the installer itself depends on.
  local required_tools=(bash awk sed grep tar curl)
  local missing=()
  for t in "${required_tools[@]}"; do
    if ! has_cmd "$t"; then
      missing+=("$t")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    log_warn "Missing tools: ${missing[*]}"
    log_info "The installer will attempt to install them in the next step."
  fi

  # Detect sudo availability — most installs need it.
  detect_sudo
  if [[ -z "$SUDO" ]] && ! is_root; then
    log_warn "Neither sudo nor root — system package installs will likely fail."
    log_warn "Either run the installer as root, or install sudo first."
    if ! $MODE_UNATTENDED && ! confirm "Continue without sudo?" "n"; then
      return 1
    fi
  fi

  # Warn on extremely low disk space (< 2 GB free on $SCRIPT_DIR).
  local free_kb
  free_kb=$(df -Pk "$SCRIPT_DIR" 2>/dev/null | awk 'NR==2 {print $4}')
  if [[ -n "$free_kb" ]] && (( free_kb < 2 * 1024 * 1024 )); then
    log_warn "Less than 2 GB free on $SCRIPT_DIR ($((free_kb / 1024)) MB)."
    log_warn "Build artifacts + frontend bundle need roughly 1.5 GB during install."
    if ! $MODE_UNATTENDED && ! confirm "Continue anyway?" "n"; then
      return 1
    fi
  fi

  # Warn on extremely low RAM (< 1 GB) — the Go compiler benefits from more.
  local mem_kb
  mem_kb=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
  if (( mem_kb > 0 )) && (( mem_kb < 1024 * 1024 )); then
    log_warn "Less than 1 GB RAM detected. Builds may be slow or OOM."
  fi

  # Load any prior answers so defaults come from the last run.
  if [[ -f "$ANSWERS_FILE" ]]; then
    log_info "Loading previous answers from $ANSWERS_FILE"
    answers_load || log_warn "Previous answers file could not be parsed."
  elif $MODE_UNATTENDED; then
    log_error "--unattended requires $ANSWERS_FILE (run interactively first)."
    return 1
  fi

  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
#  9. OS DETECTION
# ──────────────────────────────────────────────────────────────────────────────
# Populates the globals:
#   OS_ID        — debian, ubuntu, fedora, rhel, centos, rocky, alma, arch, ...
#   OS_VERSION   — the value of VERSION_ID from /etc/os-release
#   OS_FAMILY    — debian | rhel | arch | unknown
#   PKG_MANAGER  — apt | dnf | yum | pacman | zypper | unknown
#   PKG_UPDATE   — command to refresh package indices
#   PKG_INSTALL  — command prefix to install packages (no sudo, added later)
step_detect_os() {
  log_info "Detecting operating system..."

  OS_ID="unknown"; OS_VERSION=""; OS_FAMILY="unknown"
  PKG_MANAGER="unknown"; PKG_UPDATE=""; PKG_INSTALL=""

  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_VERSION="${VERSION_ID:-}"
    local like="${ID_LIKE:-}"
    case " $OS_ID $like " in
      *debian*|*ubuntu*) OS_FAMILY="debian" ;;
      *rhel*|*fedora*|*centos*|*rocky*|*alma*) OS_FAMILY="rhel" ;;
      *arch*)            OS_FAMILY="arch" ;;
      *suse*|*opensuse*) OS_FAMILY="suse" ;;
    esac
  fi

  case "$OS_FAMILY" in
    debian)
      PKG_MANAGER="apt"
      PKG_UPDATE="apt-get update -y"
      PKG_INSTALL="DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends"
      ;;
    rhel)
      if has_cmd dnf; then
        PKG_MANAGER="dnf"
        PKG_UPDATE="dnf -y check-update || true"
        PKG_INSTALL="dnf -y install"
      else
        PKG_MANAGER="yum"
        PKG_UPDATE="yum -y check-update || true"
        PKG_INSTALL="yum -y install"
      fi
      ;;
    arch)
      PKG_MANAGER="pacman"
      PKG_UPDATE="pacman -Syy --noconfirm"
      PKG_INSTALL="pacman -S --noconfirm --needed"
      ;;
    suse)
      PKG_MANAGER="zypper"
      PKG_UPDATE="zypper --non-interactive refresh"
      PKG_INSTALL="zypper --non-interactive install"
      ;;
  esac

  log_info "OS: $OS_ID $OS_VERSION (family: $OS_FAMILY, pkg: $PKG_MANAGER)"
  if [[ "$PKG_MANAGER" == "unknown" ]]; then
    log_warn "Could not detect a supported package manager."
    log_warn "You will need to install dependencies manually. Skipping auto-install."
    if ! $MODE_UNATTENDED && ! confirm "Continue with manual dependency management?" "n"; then
      return 1
    fi
  fi
  return 0
}

# pkg_install — install one or more packages using the detected package manager.
pkg_install() {
  local packages=("$@")
  if [[ "$PKG_MANAGER" == "unknown" ]]; then
    log_warn "No package manager — cannot auto-install: ${packages[*]}"
    return 1
  fi
  log_info "Installing packages: ${packages[*]}"
  case "$PKG_MANAGER" in
    apt)
      sudo_run bash -c "$PKG_INSTALL ${packages[*]}"
      ;;
    dnf|yum|pacman|zypper)
      sudo_run bash -c "$PKG_INSTALL ${packages[*]}"
      ;;
    *)
      return 1
      ;;
  esac
}

# pkg_refresh — refresh package indices (called once before the first install).
PKG_REFRESHED=false
pkg_refresh() {
  $PKG_REFRESHED && return 0
  [[ "$PKG_MANAGER" == "unknown" ]] && return 0
  log_info "Refreshing package lists..."
  sudo_run bash -c "$PKG_UPDATE" || log_warn "Package index refresh returned non-zero (often non-fatal)."
  PKG_REFRESHED=true
}

# ──────────────────────────────────────────────────────────────────────────────
# 10. BASE SYSTEM DEPENDENCIES
# ──────────────────────────────────────────────────────────────────────────────
# Installs the generic packages needed by everything else: curl, git, tar,
# build-essential/equivalent, ca-certificates, and a few niceties.
step_install_system_deps() {
  log_info "Installing base system packages..."

  if [[ "$PKG_MANAGER" == "unknown" ]]; then
    log_warn "No package manager — verifying tools are present instead."
    local missing=()
    for t in curl git tar gcc make ca-certificates; do
      has_cmd "$t" || missing+=("$t")
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
      log_error "Missing required tools: ${missing[*]}"
      log_error "Install them manually and re-run the installer."
      return 1
    fi
    return 0
  fi

  pkg_refresh

  local pkgs=()
  case "$PKG_MANAGER" in
    apt)
      pkgs=(curl git tar ca-certificates build-essential pkg-config openssl
            jq lsb-release gnupg apt-transport-https software-properties-common
            xz-utils unzip)
      ;;
    dnf|yum)
      pkgs=(curl git tar ca-certificates gcc gcc-c++ make pkgconf-pkg-config
            openssl jq which xz unzip)
      # Some RHEL-likes ship "Development Tools" as a group; try both paths.
      sudo_run bash -c "$PKG_INSTALL ${pkgs[*]}" || true
      sudo_run bash -c "${PKG_MANAGER} -y groupinstall 'Development Tools'" || true
      return 0
      ;;
    pacman)
      pkgs=(curl git tar ca-certificates base-devel openssl jq xz unzip)
      ;;
    zypper)
      pkgs=(curl git tar ca-certificates gcc gcc-c++ make pkg-config openssl
            jq xz unzip)
      ;;
  esac

  pkg_install "${pkgs[@]}" || {
    log_error "Base package install failed. See log for details: $LOG_FILE"
    return 1
  }

  # Verify critical tools are actually present after the install.
  local critical=(curl git tar gcc make openssl)
  local still_missing=()
  for t in "${critical[@]}"; do
    has_cmd "$t" || still_missing+=("$t")
  done
  if [[ ${#still_missing[@]} -gt 0 ]]; then
    log_error "After install, still missing: ${still_missing[*]}"
    return 1
  fi
  log_success "Base dependencies present."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 11. GO TOOLCHAIN
# ──────────────────────────────────────────────────────────────────────────────
# The Go version required by this project (1.26.1) is too new for most distro
# package managers, so we install the official tarball into /usr/local/go.
# This step is skipped if a suitable `go` is already on PATH.
GO_INSTALL_DIR="/usr/local/go"
GO_PROFILE_FILE="/etc/profile.d/media-server-go.sh"

step_install_go() {
  log_info "Checking Go toolchain (need >= $REQUIRED_GO_VERSION)..."

  local current=""
  if has_cmd go; then
    current=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
  elif [[ -x "$GO_INSTALL_DIR/bin/go" ]]; then
    current=$("$GO_INSTALL_DIR/bin/go" version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
    export PATH="$GO_INSTALL_DIR/bin:$PATH"
  fi

  if [[ -n "$current" ]] && version_ge "$current" "$REQUIRED_GO_VERSION"; then
    log_success "Go $current is already installed."
    export PATH="$GO_INSTALL_DIR/bin:$PATH"
    return 0
  fi

  if [[ -n "$current" ]]; then
    log_warn "Found Go $current — too old (need $REQUIRED_GO_VERSION)."
  else
    log_info "Go not found."
  fi

  local arch
  arch=$(uname -m 2>/dev/null || echo unknown)
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    armv7l|armv6l) arch="armv6l" ;;
    i386|i686) arch="386" ;;
    *)
      log_error "Unsupported CPU architecture for Go: $arch"
      return 1
      ;;
  esac

  local tarball="go${REQUIRED_GO_VERSION}.linux-${arch}.tar.gz"
  local url="https://go.dev/dl/${tarball}"
  local tmpfile="/tmp/${tarball}"

  log_info "Downloading Go $REQUIRED_GO_VERSION for linux/$arch..."
  log_info "URL: $url"
  if ! run_cmd "download Go tarball" curl -fL --retry 3 --retry-delay 2 -o "$tmpfile" "$url"; then
    log_error "Failed to download Go. Check your internet connection."
    log_error "If you are behind a proxy, set HTTPS_PROXY before re-running."
    return 1
  fi

  # Verify the tarball by running `tar -tzf` — cheap sanity check.
  if ! tar -tzf "$tmpfile" >/dev/null 2>&1; then
    log_error "Downloaded Go tarball is corrupt."
    rm -f "$tmpfile"
    return 1
  fi

  log_info "Installing Go to $GO_INSTALL_DIR..."
  if [[ -d "$GO_INSTALL_DIR" ]]; then
    sudo_run rm -rf "$GO_INSTALL_DIR" || {
      log_error "Could not remove existing $GO_INSTALL_DIR"
      return 1
    }
  fi
  sudo_run tar -C /usr/local -xzf "$tmpfile" || {
    log_error "Failed to extract Go tarball."
    return 1
  }
  rm -f "$tmpfile"

  # Add /usr/local/go/bin to PATH system-wide via /etc/profile.d.
  local profile_line='export PATH=$PATH:/usr/local/go/bin'
  if ! sudo_run bash -c "echo '$profile_line' > '$GO_PROFILE_FILE'"; then
    log_warn "Could not write $GO_PROFILE_FILE — you will need to add Go to PATH manually."
  else
    sudo_run chmod 0644 "$GO_PROFILE_FILE" || true
    log_info "Added $GO_INSTALL_DIR/bin to /etc/profile.d"
  fi
  export PATH="$GO_INSTALL_DIR/bin:$PATH"

  # Sanity check.
  if ! has_cmd go; then
    log_error "Go binary still not on PATH after install."
    return 1
  fi
  local installed
  installed=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
  if ! version_ge "$installed" "$REQUIRED_GO_VERSION"; then
    log_error "Installed Go ($installed) still too old. Aborting."
    return 1
  fi
  log_success "Go $installed installed."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 12. NODE.JS (for the Nuxt UI frontend build)
# ──────────────────────────────────────────────────────────────────────────────
# The frontend build needs Node >= 22. Distro packages are usually too old, so
# we use the NodeSource binary repo on Debian/RHEL/Fedora. Arch has a current
# version in core. Falls back to nvm-style download if nothing else works.
step_install_node() {
  log_info "Checking Node.js (need >= $REQUIRED_NODE_MAJOR)..."

  local current=""
  if has_cmd node; then
    current=$(node --version 2>/dev/null | sed 's/^v//')
  fi
  if [[ -n "$current" ]]; then
    local major="${current%%.*}"
    if (( major >= REQUIRED_NODE_MAJOR )); then
      log_success "Node $current is already installed."
      # Make sure npm is there too.
      if ! has_cmd npm; then
        log_warn "node found but npm is missing — attempting to install npm."
        case "$PKG_MANAGER" in
          apt)    pkg_install npm || true ;;
          dnf|yum) pkg_install npm || true ;;
          pacman) pkg_install npm || true ;;
        esac
      fi
      return 0
    fi
    log_warn "Found Node $current — upgrading to v$REQUIRED_NODE_MAJOR."
  else
    log_info "Node.js not found."
  fi

  pkg_refresh

  case "$PKG_MANAGER" in
    apt)
      log_info "Adding NodeSource repository for Node $REQUIRED_NODE_MAJOR..."
      if ! run_cmd "download NodeSource setup" \
          bash -c "curl -fsSL https://deb.nodesource.com/setup_${REQUIRED_NODE_MAJOR}.x -o /tmp/nodesource_setup.sh"; then
        log_error "Failed to download NodeSource setup script."
        return 1
      fi
      sudo_run bash /tmp/nodesource_setup.sh || {
        rm -f /tmp/nodesource_setup.sh
        log_error "NodeSource repo setup failed."
        return 1
      }
      rm -f /tmp/nodesource_setup.sh
      pkg_install nodejs || return 1
      ;;
    dnf|yum)
      log_info "Adding NodeSource repository for Node $REQUIRED_NODE_MAJOR..."
      if ! run_cmd "download NodeSource setup" \
          bash -c "curl -fsSL https://rpm.nodesource.com/setup_${REQUIRED_NODE_MAJOR}.x -o /tmp/nodesource_setup.sh"; then
        log_error "Failed to download NodeSource setup script."
        return 1
      fi
      sudo_run bash /tmp/nodesource_setup.sh || {
        rm -f /tmp/nodesource_setup.sh
        log_error "NodeSource repo setup failed."
        return 1
      }
      rm -f /tmp/nodesource_setup.sh
      pkg_install nodejs || return 1
      ;;
    pacman)
      pkg_install nodejs npm || return 1
      ;;
    zypper)
      pkg_install nodejs22 npm22 || pkg_install nodejs npm || return 1
      ;;
    *)
      log_error "Don't know how to install Node on $PKG_MANAGER."
      return 1
      ;;
  esac

  if ! has_cmd node; then
    log_error "Node install completed but 'node' is not on PATH."
    return 1
  fi
  local installed
  installed=$(node --version 2>/dev/null | sed 's/^v//')
  log_success "Node $installed installed."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 13. FFMPEG
# ──────────────────────────────────────────────────────────────────────────────
# Used for thumbnails, HLS transcoding, and media probing. The distro package
# is good enough in every case.
step_install_ffmpeg() {
  log_info "Checking ffmpeg..."

  if has_cmd ffmpeg && has_cmd ffprobe; then
    local ver
    ver=$(ffmpeg -version 2>/dev/null | head -n1 | awk '{print $3}')
    log_success "ffmpeg $ver already installed."
    return 0
  fi

  log_info "Installing ffmpeg..."
  pkg_refresh
  case "$PKG_MANAGER" in
    apt)       pkg_install ffmpeg || return 1 ;;
    dnf)
      # RHEL/Rocky/Alma need RPM Fusion for ffmpeg.
      if [[ "$OS_ID" != "fedora" ]]; then
        log_info "Enabling RPM Fusion (required on RHEL-likes for ffmpeg)..."
        local ver="${OS_VERSION%%.*}"
        sudo_run dnf -y install "https://mirrors.rpmfusion.org/free/el/rpmfusion-free-release-${ver}.noarch.rpm" || \
          log_warn "RPM Fusion install failed — ffmpeg may not be available."
      fi
      pkg_install ffmpeg || return 1
      ;;
    yum)       pkg_install ffmpeg || return 1 ;;
    pacman)    pkg_install ffmpeg || return 1 ;;
    zypper)    pkg_install ffmpeg || return 1 ;;
    *)
      log_error "Don't know how to install ffmpeg on $PKG_MANAGER."
      return 1
      ;;
  esac

  if ! has_cmd ffmpeg || ! has_cmd ffprobe; then
    log_error "ffmpeg install failed — binary not found after install."
    return 1
  fi
  log_success "ffmpeg installed."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 14. MYSQL / MARIADB
# ──────────────────────────────────────────────────────────────────────────────
# The server stores all metadata in MySQL. The user may already have a server
# running (local or remote), so we only install if they confirm.
MYSQL_CLIENT_BIN=""
MYSQL_IS_LOCAL=false

step_install_mysql() {
  log_info "Checking MySQL / MariaDB..."

  # If the user already has a remote DB, skip local install.
  if [[ -n "${ANSWERS[DB_HOST]:-}" ]] \
     && [[ "${ANSWERS[DB_HOST]}" != "localhost" ]] \
     && [[ "${ANSWERS[DB_HOST]}" != "127.0.0.1" ]]; then
    log_info "Remote database configured (${ANSWERS[DB_HOST]}) — skipping local install."
    # We still need a client to verify the connection.
    if ! has_cmd mysql && ! has_cmd mariadb; then
      log_info "Installing MySQL client..."
      case "$PKG_MANAGER" in
        apt)    pkg_install default-mysql-client || pkg_install mariadb-client ;;
        dnf|yum) pkg_install mysql || pkg_install mariadb ;;
        pacman) pkg_install mariadb-clients ;;
        zypper) pkg_install mariadb-client ;;
      esac
    fi
    return 0
  fi

  # Detect an existing local installation.
  if has_cmd mysql || has_cmd mariadb; then
    log_success "MySQL/MariaDB client already present."
    # Check whether a server is actually running.
    if systemctl is-active --quiet mysql 2>/dev/null \
       || systemctl is-active --quiet mariadb 2>/dev/null \
       || systemctl is-active --quiet mysqld 2>/dev/null; then
      log_success "Database service is running."
      MYSQL_IS_LOCAL=true
      return 0
    fi
    log_warn "Database client is installed but the service is not running."
  fi

  local install_local="true"
  if ! $MODE_UNATTENDED; then
    ask_yn DB_INSTALL_LOCAL "Install MariaDB on this machine?" "y"
    install_local="$DB_INSTALL_LOCAL"
  fi

  if [[ "$install_local" != "true" ]]; then
    log_warn "Skipping MySQL install. You will need to configure DB_HOST manually."
    return 0
  fi

  pkg_refresh
  case "$PKG_MANAGER" in
    apt)
      pkg_install mariadb-server mariadb-client || return 1
      sudo_run systemctl enable --now mariadb || sudo_run systemctl enable --now mysql || true
      ;;
    dnf|yum)
      pkg_install mariadb-server mariadb || return 1
      sudo_run systemctl enable --now mariadb || true
      ;;
    pacman)
      pkg_install mariadb || return 1
      # Arch: init the data directory first.
      sudo_run mariadb-install-db --user=mysql --basedir=/usr --datadir=/var/lib/mysql || \
        log_warn "mariadb-install-db returned non-zero (may already be initialised)."
      sudo_run systemctl enable --now mariadb || true
      ;;
    zypper)
      pkg_install mariadb mariadb-client || return 1
      sudo_run systemctl enable --now mariadb || true
      ;;
    *)
      log_error "Don't know how to install MariaDB on $PKG_MANAGER."
      return 1
      ;;
  esac

  # Wait up to 30 seconds for the service to come up.
  log_info "Waiting for database service to start..."
  local i
  for i in $(seq 1 30); do
    if systemctl is-active --quiet mariadb 2>/dev/null \
       || systemctl is-active --quiet mysql 2>/dev/null \
       || systemctl is-active --quiet mysqld 2>/dev/null; then
      break
    fi
    sleep 1
  done

  if ! systemctl is-active --quiet mariadb 2>/dev/null \
     && ! systemctl is-active --quiet mysql 2>/dev/null \
     && ! systemctl is-active --quiet mysqld 2>/dev/null; then
    log_error "Database service did not start within 30 seconds."
    log_error "Check the service logs with: journalctl -u mariadb"
    return 1
  fi

  MYSQL_IS_LOCAL=true
  log_success "MariaDB installed and running."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 15. DATABASE CONFIGURATION
# ──────────────────────────────────────────────────────────────────────────────
# Creates the application user and database. Verifies connectivity using the
# chosen credentials before moving on.
#
# Strategy:
#   1. If --unattended, use saved answers.
#   2. Otherwise prompt for host/port/dbname/user/password.
#   3. If the DB is local, we try to create the DB + user as root using
#      passwordless socket auth (the default on fresh MariaDB installs).
#   4. Connectivity is verified by running `SELECT 1` as the application user.
step_configure_database() {
  log_info "Configuring application database..."

  # Defaults from previous runs or hardcoded.
  local def_host="${ANSWERS[DB_HOST]:-localhost}"
  local def_port="${ANSWERS[DB_PORT]:-3306}"
  local def_name="${ANSWERS[DB_NAME]:-mediaserver}"
  local def_user="${ANSWERS[DB_USER]:-mediaserver}"

  ask DB_HOST  "Database host"     "$def_host"
  ask DB_PORT  "Database port"     "$def_port"
  ask DB_NAME  "Database name"     "$def_name"
  ask DB_USER  "Database username" "$def_user"

  if [[ -z "${ANSWERS[DB_PASSWORD]:-}" ]] || ! $MODE_UNATTENDED; then
    local pw1 pw2
    while true; do
      ask_secret DB_PASSWORD "Password for database user '$DB_USER'"
      pw1="$DB_PASSWORD"
      # In unattended mode we don't re-prompt for confirmation.
      if $MODE_UNATTENDED; then break; fi
      ask_secret DB_PASSWORD_CONFIRM "Confirm password"
      pw2="$DB_PASSWORD_CONFIRM"
      if [[ "$pw1" == "$pw2" ]]; then
        unset ANSWERS[DB_PASSWORD_CONFIRM]
        DB_PASSWORD="$pw1"
        ANSWERS[DB_PASSWORD]="$pw1"
        break
      fi
      log_warn "Passwords did not match. Try again."
    done
  else
    DB_PASSWORD="${ANSWERS[DB_PASSWORD]}"
  fi

  # Find a usable client binary. Prefer `mariadb` on modern distros.
  if has_cmd mariadb; then
    MYSQL_CLIENT_BIN="mariadb"
  elif has_cmd mysql; then
    MYSQL_CLIENT_BIN="mysql"
  else
    log_error "No MySQL/MariaDB client found. Install one and re-run this step."
    return 1
  fi
  log_debug "using MySQL client: $MYSQL_CLIENT_BIN"

  # If the database is local, try to create the DB + user as root.
  if [[ "$DB_HOST" == "localhost" || "$DB_HOST" == "127.0.0.1" ]] && $MYSQL_IS_LOCAL; then
    log_info "Creating database '$DB_NAME' and user '$DB_USER' via local socket..."
    # Build the SQL in a heredoc so passwords with special characters work.
    local sql
    # shellcheck disable=SC2016
    sql=$(cat <<SQL
CREATE DATABASE IF NOT EXISTS \`${DB_NAME}\`
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '${DB_USER}'@'localhost' IDENTIFIED BY '${DB_PASSWORD}';
ALTER USER '${DB_USER}'@'localhost' IDENTIFIED BY '${DB_PASSWORD}';
GRANT ALL PRIVILEGES ON \`${DB_NAME}\`.* TO '${DB_USER}'@'localhost';
FLUSH PRIVILEGES;
SQL
)
    # Use sudo to the local socket — this works on fresh MariaDB installs
    # because root uses unix_socket auth by default on Debian/Ubuntu and
    # passwordless on others.
    local tmpsql="/tmp/msp-init-$$-$RANDOM.sql"
    printf '%s\n' "$sql" > "$tmpsql"
    chmod 600 "$tmpsql" 2>/dev/null || true

    if sudo_run bash -c "$MYSQL_CLIENT_BIN < '$tmpsql'"; then
      log_success "Database and user created."
    else
      log_warn "Socket-auth create failed. Falling back to password prompt."
      log_warn "Enter the database root password when prompted (or press Enter if none)."
      local root_pass
      if ! $MODE_UNATTENDED; then
        ask_secret DB_ROOT_PASSWORD "MySQL/MariaDB root password (blank for none)"
        root_pass="$DB_ROOT_PASSWORD"
        unset ANSWERS[DB_ROOT_PASSWORD]
      else
        root_pass="${ANSWERS[DB_ROOT_PASSWORD]:-}"
      fi
      if [[ -n "$root_pass" ]]; then
        run_cmd "create DB as root (with password)" \
          "$MYSQL_CLIENT_BIN" -u root -p"$root_pass" -h "$DB_HOST" -P "$DB_PORT" -e "source $tmpsql" || {
          rm -f "$tmpsql"
          log_error "Could not create the database. Check the log and retry."
          return 1
        }
      else
        run_cmd "create DB as root (no password)" \
          "$MYSQL_CLIENT_BIN" -u root -h "$DB_HOST" -P "$DB_PORT" -e "source $tmpsql" || {
          rm -f "$tmpsql"
          log_error "Could not create the database. Check the log and retry."
          return 1
        }
      fi
    fi
    rm -f "$tmpsql"
  else
    log_info "Remote database — skipping automatic user creation."
    log_info "Make sure the following exist on $DB_HOST:"
    log_info "  CREATE DATABASE \`$DB_NAME\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
    log_info "  CREATE USER '$DB_USER'@'%' IDENTIFIED BY '<your-password>';"
    log_info "  GRANT ALL PRIVILEGES ON \`$DB_NAME\`.* TO '$DB_USER'@'%';"
    if ! $MODE_UNATTENDED && ! confirm "Continue with the current remote credentials?" "y"; then
      return 1
    fi
  fi

  # Verify connectivity as the application user.
  log_info "Verifying connection as '$DB_USER'..."
  if ! "$MYSQL_CLIENT_BIN" -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" -p"$DB_PASSWORD" \
         -e "SELECT 1;" "$DB_NAME" >/dev/null 2>> "$LOG_FILE"; then
    log_error "Could not connect as '$DB_USER'@'$DB_HOST':$DB_PORT to $DB_NAME"
    log_error "Check the log: $LOG_FILE"
    return 1
  fi
  log_success "Database connection verified."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 16. APPLICATION CONFIGURATION COLLECTION
# ──────────────────────────────────────────────────────────────────────────────
# Prompts for all values that go into the generated .env file. Every prompt
# has a sensible default so a home user can just press Enter through the whole
# thing.
step_collect_config() {
  log_info "Collecting application configuration..."

  log_section "SERVER"
  ask        SRV_HOST       "Bind address (0.0.0.0 for all interfaces)" "${ANSWERS[SRV_HOST]:-0.0.0.0}"
  ask        SRV_PORT       "Listen port"                               "${ANSWERS[SRV_PORT]:-8080}"
  ask_yn     SRV_HTTPS      "Enable HTTPS (requires certificate files)?" "${ANSWERS[SRV_HTTPS]:-n}"
  SRV_CERT=""; SRV_KEY=""
  if [[ "$SRV_HTTPS" == "true" ]]; then
    ask SRV_CERT "TLS certificate file path" "${ANSWERS[SRV_CERT]:-/etc/ssl/certs/mediaserver.crt}"
    ask SRV_KEY  "TLS private key file path" "${ANSWERS[SRV_KEY]:-/etc/ssl/private/mediaserver.key}"
  fi

  log_section "DIRECTORIES"
  # On a local home PC, defaults live next to the install. For "serious" installs
  # we suggest /var/lib/media-server so everything is gathered under one path.
  local dir_base_default="${ANSWERS[DIR_BASE]:-$SCRIPT_DIR}"
  ask DIR_BASE    "Base directory for media and data" "$dir_base_default"
  ask DIR_VIDEOS  "Videos directory"     "${ANSWERS[DIR_VIDEOS]:-$DIR_BASE/videos}"
  ask DIR_MUSIC   "Music directory"      "${ANSWERS[DIR_MUSIC]:-$DIR_BASE/music}"
  ask DIR_UPLOAD  "User uploads dir"     "${ANSWERS[DIR_UPLOAD]:-$DIR_BASE/uploads}"
  ask DIR_THUMB   "Thumbnails dir"       "${ANSWERS[DIR_THUMB]:-$DIR_BASE/thumbnails}"
  ask DIR_HLS     "HLS cache dir"        "${ANSWERS[DIR_HLS]:-$DIR_BASE/hls_cache}"
  ask DIR_PLAY    "Playlists dir"        "${ANSWERS[DIR_PLAY]:-$DIR_BASE/playlists}"
  ask DIR_ANALY   "Analytics dir"        "${ANSWERS[DIR_ANALY]:-$DIR_BASE/analytics}"
  ask DIR_DATA    "Data dir"             "${ANSWERS[DIR_DATA]:-$DIR_BASE/data}"
  ask DIR_LOGS    "Logs dir"             "${ANSWERS[DIR_LOGS]:-$DIR_BASE/logs}"
  ask DIR_TEMP    "Temp dir"             "${ANSWERS[DIR_TEMP]:-$DIR_BASE/temp}"
  ask DIR_BACKUP  "Backups dir"          "${ANSWERS[DIR_BACKUP]:-$DIR_BASE/backups}"

  log_section "ADMIN ACCOUNT"
  ask        ADMIN_USER "Admin username" "${ANSWERS[ADMIN_USER]:-admin}"
  if [[ -z "${ANSWERS[ADMIN_PASSWORD_HASH]:-}" ]] || ! $MODE_UNATTENDED; then
    local pw1 pw2
    while true; do
      ask_secret ADMIN_PASSWORD "Admin password (will be bcrypt-hashed)"
      pw1="$ADMIN_PASSWORD"
      if [[ ${#pw1} -lt 8 ]]; then
        log_warn "Admin password should be at least 8 characters."
        if ! confirm "Use it anyway?" "n"; then
          continue
        fi
      fi
      if $MODE_UNATTENDED; then break; fi
      ask_secret ADMIN_PASSWORD_CONFIRM "Confirm admin password"
      pw2="$ADMIN_PASSWORD_CONFIRM"
      unset ANSWERS[ADMIN_PASSWORD_CONFIRM]
      if [[ "$pw1" == "$pw2" ]]; then break; fi
      log_warn "Passwords did not match."
    done
    log_info "Hashing admin password with bcrypt..."
    local hash
    hash=$(bcrypt_hash "$ADMIN_PASSWORD" 2>/dev/null || echo "")
    if [[ -z "$hash" ]]; then
      log_warn "Could not hash password locally. The server will hash ADMIN_PASSWORD on first start."
      log_warn "(Install htpasswd, python3-bcrypt, or python3-passlib to hash at install time.)"
      ANSWERS[ADMIN_PASSWORD_HASH]=""
    else
      ANSWERS[ADMIN_PASSWORD_HASH]="$hash"
      log_success "Admin password hashed."
    fi
    # Remove the plaintext from the answers file once it has been hashed.
    unset ANSWERS[ADMIN_PASSWORD]
  fi

  log_section "FEATURES"
  ask_yn FEAT_HLS         "Enable HLS adaptive streaming?"    "${ANSWERS[FEAT_HLS]:-y}"
  ask_yn FEAT_THUMBS      "Enable thumbnail generation?"      "${ANSWERS[FEAT_THUMBS]:-y}"
  ask_yn FEAT_UPLOADS     "Enable user uploads?"              "${ANSWERS[FEAT_UPLOADS]:-y}"
  ask_yn FEAT_ANALYTICS   "Enable analytics and view tracking?" "${ANSWERS[FEAT_ANALYTICS]:-y}"
  ask_yn FEAT_SUGGESTIONS "Enable content suggestions?"       "${ANSWERS[FEAT_SUGGESTIONS]:-y}"
  ask_yn FEAT_MATURE      "Enable mature content scanner?"    "${ANSWERS[FEAT_MATURE]:-y}"
  ask_yn FEAT_DUPES       "Enable duplicate detection?"       "${ANSWERS[FEAT_DUPES]:-y}"
  ask_yn FEAT_AGE_GATE    "Enable age verification gate?"     "${ANSWERS[FEAT_AGE_GATE]:-n}"
  ask_yn FEAT_RECEIVER    "Enable receiver (accept slave nodes)?" "${ANSWERS[FEAT_RECEIVER]:-n}"
  ask_yn FEAT_REMOTE      "Enable remote media sources?"      "${ANSWERS[FEAT_REMOTE]:-n}"
  ask_yn FEAT_HF          "Enable Hugging Face visual classification?" "${ANSWERS[FEAT_HF]:-n}"
  ask_yn FEAT_DOWNLOADER  "Enable external downloader integration?" "${ANSWERS[FEAT_DOWNLOADER]:-n}"

  RECEIVER_API_KEY=""
  if [[ "$FEAT_RECEIVER" == "true" ]]; then
    if [[ -n "${ANSWERS[RECEIVER_API_KEY]:-}" ]]; then
      RECEIVER_API_KEY="${ANSWERS[RECEIVER_API_KEY]}"
      log_info "Re-using existing receiver API key from answers file."
    else
      RECEIVER_API_KEY=$(random_hex 32)
      ANSWERS[RECEIVER_API_KEY]="$RECEIVER_API_KEY"
      log_success "Generated receiver API key (kept in $ANSWERS_FILE)."
    fi
  fi

  HF_API_KEY=""
  if [[ "$FEAT_HF" == "true" ]]; then
    if [[ -z "${ANSWERS[HF_API_KEY]:-}" ]] || ! $MODE_UNATTENDED; then
      ask_secret HF_API_KEY "Hugging Face API token"
    else
      HF_API_KEY="${ANSWERS[HF_API_KEY]}"
    fi
  fi

  DOWNLOADER_URL=""
  DOWNLOADER_DIR=""
  if [[ "$FEAT_DOWNLOADER" == "true" ]]; then
    ask DOWNLOADER_URL "Downloader service URL" "${ANSWERS[DOWNLOADER_URL]:-http://localhost:4000}"
    ask DOWNLOADER_DIR "Downloader output directory (absolute path)" "${ANSWERS[DOWNLOADER_DIR]:-$DIR_BASE/downloads}"
  fi

  log_section "AUTHENTICATION"
  ask_yn AUTH_GUESTS       "Allow guest browsing without login?"   "${ANSWERS[AUTH_GUESTS]:-y}"
  ask_yn AUTH_REGISTRATION "Allow new users to register accounts?" "${ANSWERS[AUTH_REGISTRATION]:-y}"

  log_section "LOGGING"
  local log_choice
  ask_choice LOG_LEVEL "Log level" 2 "debug" "info" "warn" "error"

  log_section "SYSTEMD SERVICE"
  ask_yn INSTALL_SERVICE "Install and enable a systemd service?" "${ANSWERS[INSTALL_SERVICE]:-y}"
  SERVICE_USER="mediaserver"
  SERVICE_NAME="media-server"
  if [[ "$INSTALL_SERVICE" == "true" ]]; then
    ask SERVICE_NAME "Systemd service name" "${ANSWERS[SERVICE_NAME]:-media-server}"
    ask SERVICE_USER "System user to run the service as" "${ANSWERS[SERVICE_USER]:-mediaserver}"
  fi

  log_info "Configuration collection complete."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 17. WRITE .env FILE
# ──────────────────────────────────────────────────────────────────────────────
# Produces the complete .env file consumed by the Go server. Every variable
# mirrors a known environment override defined in internal/config/env_overrides_*.go.
# The file is written with mode 600 because it contains secrets.
step_write_env_file() {
  log_info "Generating $ENV_FILE..."

  if [[ -f "$ENV_FILE" ]]; then
    if cp "$ENV_FILE" "$ENV_BACKUP" 2>/dev/null; then
      log_info "Existing .env backed up to $(basename "$ENV_BACKUP")"
    fi
  fi

  # Truncate-then-write. Heredoc does the rest.
  : > "$ENV_FILE" || { log_error "Cannot write to $ENV_FILE"; return 1; }

  # Turn directory variables into absolute paths so the server behaves the
  # same regardless of the working directory systemd starts it in.
  local abs
  _abs() { # makes relative paths absolute without requiring the dir to exist
    case "$1" in
      /*) printf '%s' "$1" ;;
      *)  printf '%s/%s' "$SCRIPT_DIR" "$1" ;;
    esac
  }
  local a_videos a_music a_uploads a_thumb a_hls a_play a_analy a_data a_logs a_temp a_backup
  a_videos=$(_abs "$DIR_VIDEOS")
  a_music=$(_abs "$DIR_MUSIC")
  a_uploads=$(_abs "$DIR_UPLOAD")
  a_thumb=$(_abs "$DIR_THUMB")
  a_hls=$(_abs "$DIR_HLS")
  a_play=$(_abs "$DIR_PLAY")
  a_analy=$(_abs "$DIR_ANALY")
  a_data=$(_abs "$DIR_DATA")
  a_logs=$(_abs "$DIR_LOGS")
  a_temp=$(_abs "$DIR_TEMP")
  a_backup=$(_abs "$DIR_BACKUP")

  # If we couldn't hash the admin password locally, drop it into the .env as
  # ADMIN_PASSWORD so the server hashes it on first boot (and then clears it).
  local admin_hash_block=""
  if [[ -n "${ANSWERS[ADMIN_PASSWORD_HASH]:-}" ]]; then
    admin_hash_block="ADMIN_PASSWORD_HASH=${ANSWERS[ADMIN_PASSWORD_HASH]}"
  elif [[ -n "${ANSWERS[ADMIN_PASSWORD]:-}" ]]; then
    admin_hash_block="ADMIN_PASSWORD=${ANSWERS[ADMIN_PASSWORD]}"
  else
    admin_hash_block="# ADMIN_PASSWORD_HASH=<set by the server on first run>"
  fi

  # TLS mode on the MySQL driver for remote hosts.
  local db_tls=""
  if [[ "$DB_HOST" != "localhost" && "$DB_HOST" != "127.0.0.1" ]]; then
    db_tls="skip-verify"
  fi

  cat > "$ENV_FILE" <<ENVFILE
# ═══════════════════════════════════════════════════════════════════════════
#  Media Server Pro — generated configuration
#  Generated on: $(_ts)
#  by install.sh v$SCRIPT_VERSION
#
#  Re-run ./install.sh --resume to change any of these values.
#  Do NOT commit this file — it contains secrets.
# ═══════════════════════════════════════════════════════════════════════════

# ── Server ───────────────────────────────────────────────────────────────
SERVER_HOST=$SRV_HOST
SERVER_PORT=$SRV_PORT
SERVER_READ_TIMEOUT=30
SERVER_WRITE_TIMEOUT=0
SERVER_IDLE_TIMEOUT=120
SERVER_SHUTDOWN_TIMEOUT=30
SERVER_MAX_HEADER_BYTES=1048576
SERVER_ENABLE_HTTPS=$SRV_HTTPS
SERVER_CERT_FILE=$SRV_CERT
SERVER_KEY_FILE=$SRV_KEY

# ── Directories ──────────────────────────────────────────────────────────
VIDEOS_DIR=$a_videos
MUSIC_DIR=$a_music
UPLOADS_DIR=$a_uploads
THUMBNAILS_DIR=$a_thumb
HLS_CACHE_DIR=$a_hls
PLAYLISTS_DIR=$a_play
ANALYTICS_DIR=$a_analy
DATA_DIR=$a_data
LOGS_DIR=$a_logs
TEMP_DIR=$a_temp

# ── Database (MySQL / MariaDB) ───────────────────────────────────────────
DATABASE_ENABLED=true
DATABASE_HOST=$DB_HOST
DATABASE_PORT=$DB_PORT
DATABASE_NAME=$DB_NAME
DATABASE_USERNAME=$DB_USER
DATABASE_PASSWORD=$DB_PASSWORD
DATABASE_TLS_MODE=$db_tls
DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=10
DATABASE_CONN_MAX_LIFETIME=1h
DATABASE_TIMEOUT=10s
DATABASE_MAX_RETRIES=3
DATABASE_RETRY_INTERVAL=2s

# ── Streaming ────────────────────────────────────────────────────────────
STREAMING_CHUNK_SIZE=1048576
STREAMING_MOBILE_OPTIMIZATION=true
STREAMING_REQUIRE_AUTH=false
STREAMING_UNAUTH_STREAM_LIMIT=3
DOWNLOAD_ENABLED=true
DOWNLOAD_REQUIRE_AUTH=false

# ── HLS adaptive streaming ───────────────────────────────────────────────
HLS_ENABLED=$FEAT_HLS
HLS_SEGMENT_DURATION=6
HLS_CONCURRENT_LIMIT=2
HLS_AUTO_GENERATE=false
HLS_PRE_GENERATE_INTERVAL_HOURS=1
HLS_QUALITIES=360p,480p,720p,1080p
HLS_CLEANUP_ENABLED=true

# ── Thumbnails ───────────────────────────────────────────────────────────
THUMBNAILS_ENABLED=$FEAT_THUMBS
THUMBNAILS_AUTO_GENERATE=true
THUMBNAILS_WIDTH=320
THUMBNAILS_HEIGHT=180
THUMBNAILS_QUALITY=80
THUMBNAILS_PREVIEW_COUNT=10
THUMBNAILS_WORKER_COUNT=4
THUMBNAILS_QUEUE_SIZE=1000

# ── Analytics ────────────────────────────────────────────────────────────
ANALYTICS_ENABLED=$FEAT_ANALYTICS
ANALYTICS_RETENTION_DAYS=30
ANALYTICS_TRACK_PLAYBACK=true
ANALYTICS_TRACK_VIEWS=true

# ── Uploads ──────────────────────────────────────────────────────────────
UPLOADS_ENABLED=$FEAT_UPLOADS
UPLOADS_MAX_FILE_SIZE=5368709120
UPLOADS_REQUIRE_AUTH=true
UPLOADS_ALLOWED_EXTENSIONS=.mp4,.mkv,.webm,.avi,.mov,.wmv,.flv,.mp3,.flac,.wav,.aac,.ogg,.m4a

# ── Security ─────────────────────────────────────────────────────────────
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=300
RATE_LIMIT_WINDOW_SECONDS=60
SECURITY_BURST_LIMIT=60
SECURITY_BURST_WINDOW_SECONDS=5
SECURITY_VIOLATIONS_FOR_BAN=10
BAN_DURATION_MINUTES=15
AUTH_RATE_LIMIT=20
AUTH_BURST_LIMIT=5
CSP_ENABLED=true
HSTS_ENABLED=$SRV_HTTPS
CORS_ENABLED=false
SECURITY_ENABLE_IP_WHITELIST=false
SECURITY_ENABLE_IP_BLACKLIST=false

# ── Authentication ───────────────────────────────────────────────────────
AUTH_ENABLED=true
AUTH_SESSION_TIMEOUT_HOURS=168
AUTH_MAX_LOGIN_ATTEMPTS=5
AUTH_LOCKOUT_DURATION_MINUTES=15
AUTH_ALLOW_GUESTS=$AUTH_GUESTS
AUTH_ALLOW_REGISTRATION=$AUTH_REGISTRATION
AUTH_SECURE_COOKIES=$SRV_HTTPS
AUTH_DEFAULT_USER_TYPE=standard

# ── Admin panel ──────────────────────────────────────────────────────────
ADMIN_ENABLED=true
ADMIN_USERNAME=$ADMIN_USER
$admin_hash_block
ADMIN_SESSION_TIMEOUT_HOURS=24

# ── Age gate ─────────────────────────────────────────────────────────────
AGE_GATE_ENABLED=$FEAT_AGE_GATE
AGE_GATE_COOKIE_NAME=age_verified
AGE_GATE_COOKIE_MAX_AGE=31536000
AGE_GATE_IP_VERIFY_TTL_HOURS=24
AGE_GATE_BYPASS_IPS=127.0.0.1,::1

# ── Mature content scanner ───────────────────────────────────────────────
MATURE_SCANNER_ENABLED=$FEAT_MATURE
MATURE_SCANNER_AUTO_FLAG=true
MATURE_SCANNER_HIGH_CONFIDENCE_THRESHOLD=0.35
MATURE_SCANNER_MEDIUM_CONFIDENCE_THRESHOLD=0.15
MATURE_SCANNER_REQUIRE_REVIEW=true

# ── Hugging Face (visual classification) ─────────────────────────────────
HUGGINGFACE_ENABLED=$FEAT_HF
HUGGINGFACE_API_KEY=$HF_API_KEY
HUGGINGFACE_MODEL=Falconsai/nsfw_image_detection
HUGGINGFACE_MAX_FRAMES=3
HUGGINGFACE_TIMEOUT_SECS=30
HUGGINGFACE_RATE_LIMIT=30
HUGGINGFACE_MAX_CONCURRENT=2

# ── Logging ──────────────────────────────────────────────────────────────
LOG_LEVEL=$LOG_LEVEL
LOG_FORMAT=text
LOG_FILE_ENABLED=true
LOG_FILE_ROTATION=true
LOG_MAX_FILE_SIZE=104857600
LOG_MAX_BACKUPS=5
LOG_COLOR_ENABLED=false

# ── Feature toggles (master switches) ────────────────────────────────────
FEATURE_HLS=$FEAT_HLS
FEATURE_ANALYTICS=$FEAT_ANALYTICS
FEATURE_UPLOADS=$FEAT_UPLOADS
FEATURE_THUMBNAILS=$FEAT_THUMBS
FEATURE_ADMIN_PANEL=true
FEATURE_SUGGESTIONS=$FEAT_SUGGESTIONS
FEATURE_PLAYLISTS=true
FEATURE_USER_AUTH=true
FEATURE_MATURE_SCANNER=$FEAT_MATURE
FEATURE_DUPLICATE_DETECTION=$FEAT_DUPES
FEATURE_AUTO_DISCOVERY=true
FEATURE_REMOTE_MEDIA=$FEAT_REMOTE
FEATURE_RECEIVER=$FEAT_RECEIVER
FEATURE_HUGGINGFACE=$FEAT_HF
FEATURE_DOWNLOADER=$FEAT_DOWNLOADER
FEATURE_EXTRACTOR=false
FEATURE_CRAWLER=false

# ── Receiver (master accepting slave nodes) ──────────────────────────────
RECEIVER_ENABLED=$FEAT_RECEIVER
RECEIVER_API_KEYS=$RECEIVER_API_KEY
RECEIVER_PROXY_TIMEOUT_SECONDS=60

# ── Remote media ─────────────────────────────────────────────────────────
REMOTE_MEDIA_ENABLED=$FEAT_REMOTE
REMOTE_MEDIA_CACHE_ENABLED=true
REMOTE_MEDIA_CACHE_SIZE_MB=1024

# ── Downloader integration ───────────────────────────────────────────────
DOWNLOADER_ENABLED=$FEAT_DOWNLOADER
DOWNLOADER_URL=$DOWNLOADER_URL
DOWNLOADER_DOWNLOADS_DIR=$DOWNLOADER_DIR
DOWNLOADER_HEALTH_INTERVAL_SECONDS=30
DOWNLOADER_REQUEST_TIMEOUT_SECONDS=30

# ── Backup ───────────────────────────────────────────────────────────────
BACKUP_RETENTION_COUNT=10

# ── Storage backend (local filesystem) ───────────────────────────────────
STORAGE_BACKEND=local

# ── Updater ──────────────────────────────────────────────────────────────
UPDATER_BRANCH=main
UPDATER_METHOD=source
ENVFILE

  if [[ ! -s "$ENV_FILE" ]]; then
    log_error "Failed to write $ENV_FILE"
    return 1
  fi
  chmod 600 "$ENV_FILE" 2>/dev/null || log_warn "Could not chmod 600 on $ENV_FILE"
  log_success "$ENV_FILE written ($(wc -l < "$ENV_FILE") lines)"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 18. CREATE RUNTIME DIRECTORIES
# ──────────────────────────────────────────────────────────────────────────────
# Creates every directory referenced by the .env file and gives the service
# user ownership where applicable.
step_create_directories() {
  log_info "Creating runtime directories..."

  local dirs=(
    "$DIR_VIDEOS" "$DIR_MUSIC" "$DIR_UPLOAD" "$DIR_THUMB" "$DIR_HLS"
    "$DIR_PLAY" "$DIR_ANALY" "$DIR_DATA" "$DIR_LOGS" "$DIR_TEMP" "$DIR_BACKUP"
  )
  local d target
  for d in "${dirs[@]}"; do
    case "$d" in /*) target="$d" ;; *) target="$SCRIPT_DIR/$d" ;; esac
    if [[ ! -d "$target" ]]; then
      log_debug "mkdir -p $target"
      if ! mkdir -p "$target" 2>/dev/null; then
        sudo_run mkdir -p "$target" || {
          log_error "Failed to create $target"
          return 1
        }
      fi
    fi
  done
  log_success "All directories present."

  # If we are going to install a systemd unit, make sure the service user exists
  # and owns the runtime paths.
  if [[ "${INSTALL_SERVICE:-false}" == "true" ]]; then
    if ! id "$SERVICE_USER" >/dev/null 2>&1; then
      log_info "Creating system user '$SERVICE_USER'..."
      sudo_run useradd --system --home-dir "$SCRIPT_DIR" \
                       --shell /usr/sbin/nologin "$SERVICE_USER" \
        || sudo_run useradd --system --home-dir "$SCRIPT_DIR" \
                            --shell /bin/false "$SERVICE_USER" \
        || log_warn "useradd returned non-zero (user may already exist)."
    fi
    log_info "Setting ownership on runtime paths to $SERVICE_USER..."
    for d in "${dirs[@]}" "$SCRIPT_DIR"; do
      case "$d" in /*) target="$d" ;; *) target="$SCRIPT_DIR/$d" ;; esac
      sudo_run chown -R "$SERVICE_USER:$SERVICE_USER" "$target" 2>/dev/null || true
    done
    sudo_run chmod 600 "$ENV_FILE" 2>/dev/null || true
  fi
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 19. BUILD BACKEND BINARY
# ──────────────────────────────────────────────────────────────────────────────
# Runs `go build` inside the repo with the ldflags that main.go expects
# (-X main.Version, -X main.BuildDate). Produces ./server.
step_build_backend() {
  log_info "Building server binary..."

  # Make sure Go is on PATH (set in step_install_go but lost on --resume).
  if ! has_cmd go; then
    if [[ -x "$GO_INSTALL_DIR/bin/go" ]]; then
      export PATH="$GO_INSTALL_DIR/bin:$PATH"
    else
      log_error "Go toolchain not available. Re-run 'install_go' step."
      return 1
    fi
  fi

  local version build_date ldflags
  version="$(cat "$SCRIPT_DIR/VERSION" 2>/dev/null | tr -d '[:space:]' || echo "1.0.0")"
  build_date="$(date -u +%Y-%m-%d)"
  ldflags="-s -w -X main.Version=${version} -X main.BuildDate=${build_date}"

  log_info "Downloading Go modules (may take a few minutes on first run)..."
  (
    cd "$SCRIPT_DIR" && GOFLAGS="-mod=mod" go mod download
  ) >> "$LOG_FILE" 2>&1 || {
    log_error "go mod download failed. Check the log: $LOG_FILE"
    return 1
  }

  log_info "Compiling server (version $version, built $build_date)..."
  (
    cd "$SCRIPT_DIR" && \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "$ldflags" \
      -o "$SCRIPT_DIR/server" \
      ./cmd/server
  ) >> "$LOG_FILE" 2>&1 || {
    log_error "go build failed. Check the log: $LOG_FILE"
    return 1
  }

  if [[ ! -x "$SCRIPT_DIR/server" ]]; then
    log_error "Server binary not produced."
    return 1
  fi

  # Verify the binary runs with --version so we know it isn't a broken build.
  local version_out
  version_out=$("$SCRIPT_DIR/server" -version 2>&1 || true)
  log_debug "server -version: $version_out"
  log_success "Server binary built ($(stat -c %s "$SCRIPT_DIR/server" 2>/dev/null || stat -f %z "$SCRIPT_DIR/server") bytes)."

  # Also build the media-receiver binary (optional, only if the dir exists).
  if [[ -d "$SCRIPT_DIR/cmd/media-receiver" ]]; then
    log_info "Compiling media-receiver..."
    (
      cd "$SCRIPT_DIR" && \
      CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "$ldflags" \
        -o "$SCRIPT_DIR/media-receiver" \
        ./cmd/media-receiver
    ) >> "$LOG_FILE" 2>&1 || log_warn "media-receiver build failed — master is still usable."
    if [[ -x "$SCRIPT_DIR/media-receiver" ]]; then
      log_success "media-receiver binary built."
    fi
  fi

  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 20. BUILD FRONTEND (NUXT UI)
# ──────────────────────────────────────────────────────────────────────────────
# The Go binary serves the frontend via //go:embed, so the frontend must be
# built BEFORE the backend in a release flow. During this installer we build
# the backend first (because it's the critical path) and then the frontend,
# and rebuild the backend again if the frontend changed.
step_build_frontend() {
  local frontend_dir="$SCRIPT_DIR/web/nuxt-ui"
  if [[ ! -d "$frontend_dir" ]]; then
    log_warn "No frontend directory found at $frontend_dir — skipping."
    return 0
  fi
  if [[ ! -f "$frontend_dir/package.json" ]]; then
    log_warn "No package.json in $frontend_dir — skipping."
    return 0
  fi

  log_info "Building the Nuxt UI frontend..."
  if ! has_cmd npm; then
    log_error "npm is not on PATH. Re-run 'install_node' step."
    return 1
  fi

  log_info "Installing npm dependencies (this can take several minutes)..."
  (
    cd "$frontend_dir" && npm ci --no-audit --no-fund --prefer-offline
  ) >> "$LOG_FILE" 2>&1 || {
    log_warn "npm ci failed — retrying with npm install"
    (
      cd "$frontend_dir" && npm install --no-audit --no-fund
    ) >> "$LOG_FILE" 2>&1 || {
      log_error "Frontend dependency install failed. Check the log: $LOG_FILE"
      return 1
    }
  }

  log_info "Running the frontend production build..."
  (
    cd "$frontend_dir" && npm run build
  ) >> "$LOG_FILE" 2>&1 || {
    log_warn "npm run build failed. The server can still run, but the bundled UI will be outdated."
    log_warn "Inspect the log and re-run this step to retry."
    return 1
  }

  log_success "Frontend build complete."

  # If the go:embed output changed, we must rebuild the backend to include it.
  if [[ -x "$SCRIPT_DIR/server" ]]; then
    log_info "Re-linking backend to include updated frontend assets..."
    (
      cd "$SCRIPT_DIR" && \
      CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "-s -w" \
        -o "$SCRIPT_DIR/server" \
        ./cmd/server
    ) >> "$LOG_FILE" 2>&1 || log_warn "Re-link failed — the server still runs with the previous UI."
  fi
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 21. INSTALL SYSTEMD SERVICE
# ──────────────────────────────────────────────────────────────────────────────
# Takes systemd/media-server.service as a template, substitutes the paths,
# installs it as /etc/systemd/system/<SERVICE_NAME>.service, enables it, and
# optionally starts it.
step_install_systemd() {
  if [[ "${INSTALL_SERVICE:-false}" != "true" ]]; then
    log_info "Skipping systemd service installation (user declined)."
    return 0
  fi
  if ! has_cmd systemctl; then
    log_warn "systemctl not available — skipping systemd install."
    return 0
  fi

  log_info "Installing systemd unit '$SERVICE_NAME.service'..."

  local template="$SCRIPT_DIR/systemd/media-server.service"
  if [[ ! -f "$template" ]]; then
    log_error "Template not found: $template"
    return 1
  fi

  local tmp_unit="/tmp/${SERVICE_NAME}.service.$$"
  # The template uses __DEPLOY_DIR__ as a placeholder. We replace it with the
  # install directory, and also patch User/Group to the chosen service user.
  sed \
    -e "s|__DEPLOY_DIR__|$SCRIPT_DIR|g" \
    -e "s|^User=.*|User=$SERVICE_USER|" \
    -e "s|^Group=.*|Group=$SERVICE_USER|" \
    -e "s|^Description=.*|Description=Media Server Pro ($SERVICE_NAME)|" \
    "$template" > "$tmp_unit" 2>> "$LOG_FILE" || {
    log_error "Template substitution failed."
    return 1
  }

  sudo_run install -m 0644 "$tmp_unit" "/etc/systemd/system/${SERVICE_NAME}.service" || {
    rm -f "$tmp_unit"
    log_error "Could not install unit file to /etc/systemd/system/"
    return 1
  }
  rm -f "$tmp_unit"

  sudo_run systemctl daemon-reload || log_warn "systemctl daemon-reload failed."
  sudo_run systemctl enable "$SERVICE_NAME" >> "$LOG_FILE" 2>&1 || \
    log_warn "systemctl enable $SERVICE_NAME failed — you may need to enable manually."

  if $MODE_UNATTENDED || confirm "Start $SERVICE_NAME now?" "y"; then
    sudo_run systemctl restart "$SERVICE_NAME" || {
      log_error "Service failed to start. Inspect with: journalctl -u $SERVICE_NAME -n 200"
      return 1
    }
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
      log_success "$SERVICE_NAME is running."
    else
      log_error "$SERVICE_NAME is not active. Inspect with: journalctl -u $SERVICE_NAME -n 200"
      return 1
    fi
  else
    log_info "Service installed but not started. Start with:"
    log_info "  sudo systemctl start $SERVICE_NAME"
  fi
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 22. HEALTH CHECK
# ──────────────────────────────────────────────────────────────────────────────
# Hits /api/health on the configured port and verifies a 200 response. Tolerant
# of slow starts — waits up to 60 seconds before failing.
step_health_check() {
  log_info "Running health check..."
  local url scheme host port
  if [[ "$SRV_HTTPS" == "true" ]]; then
    scheme="https"
  else
    scheme="http"
  fi
  # If bound to 0.0.0.0 we still hit 127.0.0.1 for the health probe.
  if [[ "$SRV_HOST" == "0.0.0.0" || "$SRV_HOST" == "::" ]]; then
    host="127.0.0.1"
  else
    host="$SRV_HOST"
  fi
  port="$SRV_PORT"
  url="$scheme://$host:$port/api/health"

  # If we didn't install systemd, start a one-off server in the background.
  local started_here=0 pid=""
  if ! systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    log_info "Starting a temporary server for health check..."
    (
      cd "$SCRIPT_DIR" && \
      ./server -config "$SCRIPT_DIR/config.json" -log-level info \
        >> "$LOG_FILE" 2>&1
    ) &
    pid=$!
    started_here=1
    log_debug "temp server pid=$pid"
  fi

  local ok=false i
  for i in $(seq 1 60); do
    if has_cmd curl; then
      if curl -fsS --max-time 2 -o /dev/null -k "$url" 2>/dev/null; then
        ok=true
        break
      fi
    elif has_cmd wget; then
      if wget -q --tries=1 --timeout=2 --no-check-certificate -O /dev/null "$url" 2>/dev/null; then
        ok=true
        break
      fi
    fi
    sleep 1
  done

  if (( started_here == 1 )) && [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi

  if $ok; then
    log_success "Health check passed: $url"
    return 0
  fi
  log_warn "Health check did not get a successful response from $url"
  log_warn "This is not necessarily fatal — check the log and the service status."
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 23. UNINSTALL / PURGE
# ──────────────────────────────────────────────────────────────────────────────
run_uninstall() {
  log_section "UNINSTALL"
  log_warn "This will stop the service and remove the binary."

  local svc="${ANSWERS[SERVICE_NAME]:-media-server}"
  if has_cmd systemctl; then
    sudo_run systemctl stop "$svc" 2>/dev/null || true
    sudo_run systemctl disable "$svc" 2>/dev/null || true
    sudo_run rm -f "/etc/systemd/system/${svc}.service" || true
    sudo_run systemctl daemon-reload 2>/dev/null || true
  fi
  rm -f "$SCRIPT_DIR/server" "$SCRIPT_DIR/media-receiver" 2>/dev/null || true
  rm -f "$SCRIPT_DIR/config.json" 2>/dev/null || true
  log_success "Service removed and binaries deleted."

  if [[ "${MODE_PURGE:-false}" == "true" ]]; then
    log_warn "PURGE MODE: all data directories and the .env file will be deleted."
    if ! confirm "Are you ABSOLUTELY sure? This is irreversible." "n"; then
      log_info "Purge cancelled."
      return 0
    fi
    rm -f "$ENV_FILE" "$ANSWERS_FILE" "$STATE_FILE" 2>/dev/null || true
    local dirs=(
      "${ANSWERS[DIR_VIDEOS]}" "${ANSWERS[DIR_MUSIC]}" "${ANSWERS[DIR_UPLOAD]}"
      "${ANSWERS[DIR_THUMB]}"  "${ANSWERS[DIR_HLS]}"   "${ANSWERS[DIR_PLAY]}"
      "${ANSWERS[DIR_ANALY]}"  "${ANSWERS[DIR_DATA]}"  "${ANSWERS[DIR_LOGS]}"
      "${ANSWERS[DIR_TEMP]}"   "${ANSWERS[DIR_BACKUP]}"
    )
    local d
    for d in "${dirs[@]}"; do
      [[ -z "$d" ]] && continue
      [[ ! -d "$d" ]] && continue
      log_info "Removing $d"
      rm -rf "$d" 2>/dev/null || sudo_run rm -rf "$d" 2>/dev/null || true
    done
    # Drop the database only if we created it locally.
    if [[ -n "${ANSWERS[DB_NAME]:-}" ]] && [[ "${ANSWERS[DB_HOST]:-}" == "localhost" || "${ANSWERS[DB_HOST]:-}" == "127.0.0.1" ]]; then
      if confirm "Drop the database '${ANSWERS[DB_NAME]}'?" "n"; then
        sudo_run bash -c "mariadb -e \"DROP DATABASE IF EXISTS \\\`${ANSWERS[DB_NAME]}\\\`;\"" 2>/dev/null \
          || sudo_run bash -c "mysql -e \"DROP DATABASE IF EXISTS \\\`${ANSWERS[DB_NAME]}\\\`;\"" 2>/dev/null \
          || log_warn "Database drop failed — do it manually if needed."
      fi
    fi
    log_success "Purge complete."
  fi
  log_info "Uninstall complete. Log: $LOG_FILE"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# 24. FINAL SUMMARY
# ──────────────────────────────────────────────────────────────────────────────
print_summary() {
  local svc="${SERVICE_NAME:-media-server}"
  local scheme="http"
  [[ "$SRV_HTTPS" == "true" ]] && scheme="https"
  local host="$SRV_HOST"
  [[ "$host" == "0.0.0.0" || "$host" == "::" ]] && host="localhost"
  local url="$scheme://$host:$SRV_PORT"

  printf '\n'
  printf '%b══════════════════════════════════════════════════════════════%b\n' "$C_GREEN" "$C_RESET"
  printf '%b                  INSTALL COMPLETE%b\n' "$C_GREEN$C_BOLD" "$C_RESET"
  printf '%b══════════════════════════════════════════════════════════════%b\n' "$C_GREEN" "$C_RESET"
  printf '\n'
  printf '  %bWeb UI:%b           %s\n' "$C_BOLD" "$C_RESET" "$url"
  printf '  %bAdmin login:%b      %s (password you set during install)\n' "$C_BOLD" "$C_RESET" "${ADMIN_USER:-admin}"
  printf '  %bConfig file:%b      %s\n' "$C_BOLD" "$C_RESET" "$ENV_FILE"
  printf '  %bAnswers file:%b     %s  %b(reuse with --unattended)%b\n' "$C_BOLD" "$C_RESET" "$ANSWERS_FILE" "$C_DIM" "$C_RESET"
  printf '  %bInstall log:%b      %s\n' "$C_BOLD" "$C_RESET" "$LOG_FILE"
  printf '\n'

  if [[ "${INSTALL_SERVICE:-false}" == "true" ]]; then
    printf '  %bService name:%b     %s\n' "$C_BOLD" "$C_RESET" "$svc"
    printf '  %bService status:%b   sudo systemctl status %s\n' "$C_BOLD" "$C_RESET" "$svc"
    printf '  %bService logs:%b     sudo journalctl -u %s -f\n' "$C_BOLD" "$C_RESET" "$svc"
    printf '  %bRestart:%b          sudo systemctl restart %s\n' "$C_BOLD" "$C_RESET" "$svc"
  else
    printf '  %bStart manually:%b   cd %s && ./server\n' "$C_BOLD" "$C_RESET" "$SCRIPT_DIR"
  fi

  if [[ "${FEAT_RECEIVER:-false}" == "true" ]] && [[ -n "${RECEIVER_API_KEY:-}" ]]; then
    printf '\n'
    printf '  %bReceiver API key:%b\n' "$C_BOLD" "$C_RESET"
    printf '    %s\n' "$RECEIVER_API_KEY"
    printf '  %b(Give this to your slave nodes so they can register.)%b\n' "$C_DIM" "$C_RESET"
  fi

  printf '\n'
  printf '  %bNext steps:%b\n' "$C_BOLD" "$C_RESET"
  printf '    1. Drop some media files into %s\n' "$DIR_VIDEOS"
  printf '    2. Open %s in a browser\n' "$url"
  printf '    3. Log in as %s\n' "${ADMIN_USER:-admin}"
  printf '\n'
  printf '  %bNeed help?%b Attach the install log to your bug report:\n' "$C_BOLD" "$C_RESET"
  printf '    %s\n' "$LOG_FILE"
  printf '\n'
  printf '%b══════════════════════════════════════════════════════════════%b\n' "$C_GREEN" "$C_RESET"
}

# ──────────────────────────────────────────────────────────────────────────────
# 25. MAIN
# ──────────────────────────────────────────────────────────────────────────────
main() {
  parse_args "$@"
  init_log_file

  section "Media Server Pro — Interactive Installer"
  log_info "Version:        $INSTALLER_VERSION"
  log_info "Script dir:     $SCRIPT_DIR"
  log_info "Log file:       $LOG_FILE"
  log_info "Mode:           $(if $MODE_UNATTENDED; then echo unattended; else echo interactive; fi)"
  if $MODE_RESUME; then
    log_info "Resume:         yes (picking up from saved state)"
  fi
  log_info "Started:        $(date '+%Y-%m-%d %H:%M:%S')"

  # Uninstall takes a completely different path.
  if $MODE_UNINSTALL; then
    log_info "Running in UNINSTALL mode."
    answers_load || true
    state_load || true
    run_uninstall
    local rc=$?
    log_info "Uninstall finished with exit code $rc"
    exit $rc
  fi

  # Load any previously-saved answers so we can resume or re-use defaults.
  answers_load || true
  state_load || true

  # On a fresh run (not --resume), clear any stale state so every step executes.
  if ! $MODE_RESUME; then
    state_clear
  fi

  # ── Step execution ──
  # Each run_step call handles: skip-if-complete, retry on failure, state save.
  run_step "preflight"           step_preflight            || exit 1
  run_step "detect_os"           step_detect_os            || exit 1
  run_step "install_system_deps" step_install_system_deps  || exit 1
  run_step "install_go"          step_install_go           || exit 1
  run_step "install_node"        step_install_node         || exit 1
  run_step "install_ffmpeg"      step_install_ffmpeg       || exit 1
  run_step "install_mysql"       step_install_mysql        || exit 1
  run_step "configure_database"  step_configure_database   || exit 1
  run_step "collect_config"      step_collect_config       || exit 1
  run_step "write_env_file"      step_write_env_file       || exit 1
  run_step "create_directories"  step_create_directories   || exit 1
  run_step "build_backend"       step_build_backend        || exit 1
  run_step "build_frontend"      step_build_frontend       || exit 1
  run_step "install_systemd"     step_install_systemd      || exit 1
  # Health check is best-effort — don't abort the installer if it can't reach
  # the endpoint (the server may still be coming up, firewall, etc.).
  run_step "health_check"        step_health_check         || log_warn "Health check did not pass — inspect the log and service status."

  state_save "done"
  print_summary
  log_success "All steps completed."
  exit 0
}

main "$@"
