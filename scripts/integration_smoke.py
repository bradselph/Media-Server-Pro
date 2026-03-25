#!/usr/bin/env python3
"""Start the reference backend briefly and GET /health (Loop 3 smoke).

Supports:
- Go + Gin (this repo): ``go run ./cmd/server`` with SERVER_PORT / MEDIA_SERVER_PORT.
- Legacy Python ASGI: ``python -m uvicorn <module>`` when integration_app looks like a module path.

Requires a reachable MySQL when the server expects DATABASE_* (e.g. GitHub Actions service container).
Install FFmpeg on the runner if thumbnails are enabled (critical module).
"""

from __future__ import annotations

import json
import os
import shutil
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from subprocess import DEVNULL, Popen, TimeoutExpired

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from _synthesis_config import reference_example, resolved_backend_dir  # noqa: E402

REPO_ROOT = Path(__file__).resolve().parents[1]


def _is_go_integration(ref: dict) -> bool:
    app = str(ref.get("integration_app") or "")
    if app.endswith(".go"):
        return True
    return (REPO_ROOT / "go.mod").is_file()


def _go_run_cmd(ref: dict) -> list[str]:
    app = str(ref.get("integration_app") or "")
    if app.endswith("/main.go") or app.endswith("\\main.go"):
        pkg = Path(app).parent.as_posix()
        return ["go", "run", "./" + pkg]
    return ["go", "run", "./cmd/server"]


def _uvicorn_cmd(ref: dict) -> list[str]:
    app = str(ref.get("integration_app") or "app.main:app")
    return [sys.executable, "-m", "uvicorn", app, "--host", "127.0.0.1"]


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
    cwd = str(backend.resolve())

    env = os.environ.copy()
    env["SERVER_PORT"] = str(port)
    env["MEDIA_SERVER_PORT"] = str(port)
    env.setdefault("GIN_MODE", "release")

    if _is_go_integration(ref):
        if shutil.which("go") is None:
            print("go executable not found; cannot run integration smoke.", file=sys.stderr)
            sys.exit(1)
        cmd = _go_run_cmd(ref)
        proc = Popen(cmd, cwd=cwd, env=env, stdout=DEVNULL, stderr=DEVNULL)
    else:
        cmd = _uvicorn_cmd(ref) + ["--port", str(port)]
        prev = env.get("PYTHONPATH", "")
        env["PYTHONPATH"] = str(backend) + (os.pathsep + prev if prev else "")
        proc = Popen(cmd, cwd=cwd, env=env, stdout=DEVNULL, stderr=DEVNULL)

    try:
        url = f"http://127.0.0.1:{port}/health"
        # Server + DB + initial media scan can exceed 10s on cold CI.
        for attempt in range(200):
            try:
                with urllib.request.urlopen(url, timeout=1.0) as r:
                    body = r.read().decode()
                if r.status != 200:
                    time.sleep(0.25)
                    continue
                try:
                    data = json.loads(body)
                except json.JSONDecodeError:
                    time.sleep(0.25)
                    continue
                if data.get("status") == "ok":
                    print("Integration smoke passed (GET /health).")
                    return
                time.sleep(0.25)
            except (OSError, urllib.error.HTTPError, urllib.error.URLError):
                if attempt == 0 and proc.poll() is not None:
                    print(
                        "Backend process exited before /health responded "
                        f"(cmd={cmd!r}, cwd={cwd!r}).",
                        file=sys.stderr,
                    )
                    sys.exit(1)
                time.sleep(0.25)
        print("Timeout waiting for healthy GET /health", file=sys.stderr)
        sys.exit(1)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except TimeoutExpired:
            proc.kill()


if __name__ == "__main__":
    main()
