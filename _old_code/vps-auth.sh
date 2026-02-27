#!/usr/bin/env bash
# vps-auth.sh — Shared SSH key setup helper.
# Source this file (do not run directly) from other vps-*.sh scripts:
#   source "$(dirname "$0")/vps-auth.sh"
#
# Expects these variables to already be set:
#   VPS_HOST  VPS_USER  VPS_PORT  KEY_FILE
#
# After sourcing, SSH_OPTS array and vps() function are ready to use.

# ── 1. Generate key if missing (no passphrase — needed for automation) ────────
if [[ ! -f "$KEY_FILE" ]]; then
  echo "--- Generating SSH key at $KEY_FILE ---"
  mkdir -p "$(dirname "$KEY_FILE")"
  ssh-keygen -t ed25519 -f "$KEY_FILE" -N "" -C "mediaserver-deploy"
  echo ""
fi

# ── 2. Remove passphrase if the key has one ───────────────────────────────────
# BatchMode=yes (used for all deploys) cannot prompt for a key passphrase.
# We check by trying to export the public key with an empty passphrase.
if ! ssh-keygen -y -P "" -f "$KEY_FILE" &>/dev/null; then
  echo "--- SSH key has a passphrase — removing it for automated deploys ---"
  echo "    Enter the CURRENT key passphrase when prompted:"
  ssh-keygen -p -f "$KEY_FILE" -N ""
  echo "  Passphrase removed — key will work without prompts from now on."
  echo ""
fi

# ── 3. Convert POSIX path to Windows path for Windows OpenSSH ─────────────────
# Git Bash exposes HOME as /c/Users/... but Windows ssh.exe needs C:/Users/...
KEY_FILE_SSH="$KEY_FILE"
if command -v cygpath &>/dev/null 2>&1; then
  KEY_FILE_SSH="$(cygpath -m "$KEY_FILE" 2>/dev/null || echo "$KEY_FILE")"
fi

SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$VPS_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8)
vps() { ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" -- "$@"; }

# ── 4. Test key auth; install on VPS if needed (one-time password prompt) ─────
if ! ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "exit 0" 2>/dev/null; then
  echo "--- Key not authorized on VPS — installing it now ---"
  echo "    Enter the VPS root password when prompted (one time only)."
  echo ""

  PUB_KEY="$(cat "${KEY_FILE}.pub")"

  if command -v ssh-copy-id &>/dev/null; then
    ssh-copy-id -i "${KEY_FILE}.pub" -p "$VPS_PORT" "$VPS_USER@$VPS_HOST"
  else
    ssh -p "$VPS_PORT" \
        -o StrictHostKeyChecking=accept-new \
        -o ConnectTimeout=10 \
        "$VPS_USER@$VPS_HOST" \
        "mkdir -p ~/.ssh && chmod 700 ~/.ssh && \
         echo '$PUB_KEY' >> ~/.ssh/authorized_keys && \
         chmod 600 ~/.ssh/authorized_keys && \
         echo 'Key installed OK.'"
  fi

  # Verify
  if ! ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "exit 0" 2>/dev/null; then
    echo ""
    echo "ERROR: SSH key auth still failing after install."
    echo "  Try manually: ssh -i \"$KEY_FILE_SSH\" $VPS_USER@$VPS_HOST"
    exit 1
  fi

  echo ""
  echo "  SSH key installed — all vps-*.sh scripts now connect without a password."
  echo ""
fi
