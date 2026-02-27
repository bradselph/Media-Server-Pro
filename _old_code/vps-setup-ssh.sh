#!/usr/bin/env bash
# vps-setup-ssh.sh — Install your local SSH key on the VPS so all other vps-*.sh
# scripts work without password prompts.
#
# Run this ONCE with the VPS root password when prompted.
# After that, all SSH connections use the key automatically.
#
# Usage:
#   ./vps-setup-ssh.sh

set -euo pipefail

VPS_HOST="${VPS_HOST:-66.179.136.144}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/ED_25519}"

PUB_KEY="${KEY_FILE}.pub"

if [[ ! -f "$PUB_KEY" ]]; then
  echo "ERROR: Public key not found at $PUB_KEY"
  echo "       Generate one with: ssh-keygen -t ed25519 -f $KEY_FILE"
  exit 1
fi

echo "Installing public key on $VPS_USER@$VPS_HOST..."
echo "You will be prompted for the VPS root password once."
echo ""

# Use ssh-copy-id if available, otherwise manual approach
if command -v ssh-copy-id &>/dev/null; then
  ssh-copy-id -i "$PUB_KEY" -p "$VPS_PORT" "$VPS_USER@$VPS_HOST"
else
  # Manual: pipe the key into authorized_keys via password SSH
  PUB_KEY_CONTENT="$(cat "$PUB_KEY")"
  ssh -p "$VPS_PORT" -o StrictHostKeyChecking=accept-new "$VPS_USER@$VPS_HOST" \
    "mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '$PUB_KEY_CONTENT' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && echo 'Key installed.'"
fi

echo ""
echo "Testing key-based login..."
if ssh -i "$KEY_FILE" -p "$VPS_PORT" -o BatchMode=yes -o ConnectTimeout=5 \
      "$VPS_USER@$VPS_HOST" "echo 'KEY_AUTH_OK'" 2>/dev/null; then
  echo "SUCCESS — passwordless SSH is working."
  echo "All vps-*.sh scripts will now connect automatically."
else
  echo "WARNING — key auth test failed. The key may not have installed correctly."
  echo "Check: ssh -i $KEY_FILE $VPS_USER@$VPS_HOST"
fi
