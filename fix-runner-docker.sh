#!/usr/bin/env bash
# ══════════════════════════════════════════════════════════════════════════════
#  Fix self-hosted GitHub runner so it can talk to Docker
# ══════════════════════════════════════════════════════════════════════════════
#  Run on the runner host as root:
#     sudo bash fix-runner-docker.sh [--runner-dir /opt/github-runner]
#                                    [--service github-runner.service]
#
#  Steps:
#    1. Install Docker if missing (idempotent get.docker.com).
#    2. Find the runner user (owner of $RUNNER_DIR).
#    3. Add the runner user to the docker group.
#    4. Drop a systemd override pinning SupplementaryGroups=docker so it
#       survives runner reinstalls / package upgrades.
#    5. Force-kill any leftover Runner.Listener processes (systemctl
#       restart often leaves orphans that retain the old group set).
#    6. Restart the service and verify Docker access works in a fresh
#       runner session.
# ══════════════════════════════════════════════════════════════════════════════

set -u
RUNNER_DIR="/opt/github-runner"
SERVICE="github-runner.service"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --runner-dir)  RUNNER_DIR="$2"; shift 2 ;;
    --service)     SERVICE="$2"; shift 2 ;;
    -h|--help)     sed -n '2,20p' "$0"; exit 0 ;;
    *)             echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

red()    { printf '\033[0;31m%s\033[0m\n' "$*"; }
green()  { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[1;33m%s\033[0m\n' "$*"; }
cyan()   { printf '\033[0;36m%s\033[0m\n' "$*"; }
section(){ printf '\n\033[1;34m== %s ==\033[0m\n' "$*"; }

[[ $EUID -eq 0 ]] || { red "Run as root: sudo bash $0"; exit 1; }
[[ -d "$RUNNER_DIR" ]] || { red "Runner dir not found: $RUNNER_DIR"; exit 1; }
systemctl list-units --type=service --no-legend "$SERVICE" >/dev/null 2>&1 \
  || { red "Service not found: $SERVICE"; exit 1; }

# ── 1. Install Docker if missing ────────────────────────────────────────────
section "1. Docker engine"
if command -v docker >/dev/null 2>&1; then
  green "docker already installed: $(docker --version)"
else
  cyan "Installing Docker via get.docker.com…"
  curl -fsSL https://get.docker.com | sh
fi
systemctl is-active --quiet docker || systemctl enable --now docker
green "docker daemon: $(systemctl is-active docker)"

# ── 2. Resolve runner user ──────────────────────────────────────────────────
section "2. Runner user"
RUNNER_USER="$(stat -c '%U' "$RUNNER_DIR")"
[[ -n "$RUNNER_USER" && "$RUNNER_USER" != "UNKNOWN" ]] \
  || { red "Could not determine owner of $RUNNER_DIR"; exit 1; }
green "Runner user: $RUNNER_USER (owns $RUNNER_DIR)"

# ── 3. Add to docker group ──────────────────────────────────────────────────
section "3. docker group membership"
if id -nG "$RUNNER_USER" | tr ' ' '\n' | grep -qx docker; then
  green "$RUNNER_USER is already in the docker group"
else
  cyan "usermod -aG docker $RUNNER_USER"
  usermod -aG docker "$RUNNER_USER"
  green "Added $RUNNER_USER to docker group"
fi
echo "Groups for $RUNNER_USER:"
id "$RUNNER_USER"

# ── 4. systemd override (pin SupplementaryGroups=docker) ───────────────────
section "4. systemd drop-in"
DROPIN_DIR="/etc/systemd/system/${SERVICE}.d"
DROPIN_FILE="${DROPIN_DIR}/docker-group.conf"
mkdir -p "$DROPIN_DIR"
cat > "$DROPIN_FILE" <<EOF
# Force the runner's systemd session to include the docker supplementary group
# regardless of what the upstream unit does. Survives runner reinstalls.
[Service]
SupplementaryGroups=docker
EOF
green "Wrote $DROPIN_FILE"
systemctl daemon-reload

# ── 5. Hard-restart: kill leftovers, then start fresh ──────────────────────
section "5. Hard-restart runner"
cyan "Stopping $SERVICE…"
systemctl stop "$SERVICE" || true
sleep 1

cyan "Killing any orphan Runner.Listener / run.sh under $RUNNER_DIR…"
pkill -9 -f "$RUNNER_DIR/" 2>/dev/null || true
sleep 1

# Belt-and-braces: anything still holding the runner's working directory
LEFTOVER="$(pgrep -u "$RUNNER_USER" -f Runner.Listener || true)"
if [[ -n "$LEFTOVER" ]]; then
  yellow "Stubborn PIDs still alive: $LEFTOVER — sending SIGKILL"
  kill -9 $LEFTOVER 2>/dev/null || true
  sleep 1
fi

cyan "Starting $SERVICE…"
systemctl start "$SERVICE"
sleep 3

# ── 6. Verify ──────────────────────────────────────────────────────────────
section "6. Verify"
NEW_PID="$(pgrep -u "$RUNNER_USER" -f Runner.Listener | head -1)"
if [[ -z "$NEW_PID" ]]; then
  red "Runner.Listener didn't come up. Last 30 lines of journal:"
  journalctl -u "$SERVICE" -n 30 --no-pager
  exit 2
fi

echo "New Runner.Listener PID: $NEW_PID"
ps -o pid,lstart,user,cmd -p "$NEW_PID"
echo

DOCKER_GID="$(getent group docker | cut -d: -f3)"
RUNNER_GROUPS_LINE="$(grep '^Groups:' /proc/"$NEW_PID"/status || true)"
echo "Process supplementary groups: $RUNNER_GROUPS_LINE"
echo "docker GID: $DOCKER_GID"

if echo "$RUNNER_GROUPS_LINE" | tr ' ' '\n' | grep -qx "$DOCKER_GID"; then
  green "✓ Runner process has docker GID ($DOCKER_GID) in its supplementary groups"
else
  red "✗ Runner process is missing docker GID — systemd override didn't take effect"
  exit 3
fi

echo
cyan "Smoke test: docker info as $RUNNER_USER…"
if sudo -u "$RUNNER_USER" -H docker info >/dev/null 2>&1; then
  green "✓ $RUNNER_USER can talk to the Docker daemon"
  sudo -u "$RUNNER_USER" -H docker version | head -10
else
  red "✗ Daemon access still failing. Socket perms:"
  ls -la /var/run/docker.sock
  exit 4
fi

echo
green "All set. Re-trigger the workflow: GitHub → Actions → Docker Publish → Run workflow"
