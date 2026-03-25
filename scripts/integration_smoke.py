#!/usr/bin/env python3
"""Start reference backend briefly and GET /health (Loop 3 smoke)."""

from __future__ import annotations

import os
import sys
import time
import urllib.request
from pathlib import Path
from subprocess import DEVNULL, Popen, TimeoutExpired

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from _synthesis_config import reference_example, resolved_backend_dir  # noqa: E402


def main() -> None:
    ref = reference_example()
    ci = ref.get("ci") or {}
    if ci.get("skip_integration_smoke") or os.environ.get(
        "SYNTHESIS_SKIP_INTEGRATION_SMOKE", ""
    ).lower() in ("1", "true", "yes"):
        print("Integration smoke skipped (project settings or env).")
        return

    backend = resolved_backend_dir()
    if not backend.is_dir():
        print(f"Backend directory missing: {backend}", file=sys.stderr)
        sys.exit(1)

    port = int(os.environ.get("INTEGRATION_PORT", ref["integration_port"]))
    app = os.environ.get("INTEGRATION_APP", ref["integration_app"])

    env = os.environ.copy()
    prev = env.get("PYTHONPATH", "")
    env["PYTHONPATH"] = str(backend) + (os.pathsep + prev if prev else "")

    cmd = [sys.executable, "-m", "uvicorn", app, "--host", "127.0.0.1", "--port", str(port)]
    proc = Popen(cmd, cwd=str(backend), env=env, stdout=DEVNULL, stderr=DEVNULL)
    try:
        url = f"http://127.0.0.1:{port}/health"
        for _ in range(50):
            try:
                with urllib.request.urlopen(url, timeout=0.5) as r:
                    body = r.read().decode()
                if '"status"' in body and "ok" in body:
                    print("Integration smoke passed (GET /health).")
                    return
            except OSError:
                time.sleep(0.2)
        print("Timeout waiting for /health", file=sys.stderr)
        sys.exit(1)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except TimeoutExpired:
            proc.kill()


if __name__ == "__main__":
    main()
