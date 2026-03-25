#!/usr/bin/env bash
# Wrapper: run Python integration smoke (paths from synthesis/partitions.json).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "${ROOT}/scripts/integration_smoke.py"
