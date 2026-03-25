"""Load synthesis paths from project_settings (when initialized) or partitions.json."""

from __future__ import annotations

import json
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
MANIFEST = REPO_ROOT / "synthesis" / "partitions.json"
SETTINGS = REPO_ROOT / "synthesis" / "project_settings.json"


def load_manifest() -> dict:
    with open(MANIFEST, encoding="utf-8") as f:
        return json.load(f)


def load_project_settings() -> dict | None:
    if not SETTINGS.is_file():
        return None
    with open(SETTINGS, encoding="utf-8") as f:
        data = json.load(f)
    if not data.get("initialized"):
        return None
    return data


def _as_posix_rel(p: str | Path) -> str:
    s = str(p).replace("\\", "/").strip()
    if s.startswith("./"):
        s = s[2:]
    return s


def _with_slash(dir_path: str) -> str:
    s = dir_path.rstrip("/")
    return s + "/"


def effective_partition_prefixes(data: dict | None = None) -> dict[str, list[str]]:
    """
    Backend/frontend/contract path prefixes (with trailing /) for partition checks.
    When project_settings is initialized, those paths win; else manifest partitions.
    """
    ps = load_project_settings()
    if ps:
        paths = ps["paths"]
        out: dict[str, list[str]] = {
            "contract": [_with_slash(paths["contract_dir"])],
            "backend": [_with_slash(paths["backend_dir"])],
            "frontend": (
                [_with_slash(paths["frontend_dir"])] if paths.get("has_frontend") else []
            ),
        }
        return out
    data = data or load_manifest()
    result: dict[str, list[str]] = {}
    for name, part in data["partitions"].items():
        result[name] = [_with_slash(p) for p in part["paths"]]
    return result


def reference_example(data: dict | None = None) -> dict:
    ps = load_project_settings()
    if ps:
        paths = ps["paths"]
        integ = ps.get("integration", {})
        ci = ps.get("ci", {})
        return {
            "openapi_spec": Path(_as_posix_rel(paths["openapi_spec"])),
            "backend_dir": Path(_as_posix_rel(paths["backend_dir"])),
            "frontend_dir": Path(_as_posix_rel(paths["frontend_dir"])),
            "integration_app": integ.get("app_module", "app.main:app"),
            "integration_port": int(integ.get("port", 8765)),
            "partition_check_base": ps.get("git", {}).get("partition_check_base", "origin/main"),
            "ci": ci,
        }
    data = data or load_manifest()
    ref = data.get("reference_example") or {}
    return {
        "openapi_spec": Path(ref.get("openapi_spec", "api_spec/openapi.yaml")),
        "backend_dir": Path(ref.get("backend_dir", "server")),
        "frontend_dir": Path(ref.get("frontend_dir", "client")),
        "integration_app": ref.get("integration_app", "app.main:app"),
        "integration_port": int(ref.get("integration_port", 8765)),
        "partition_check_base": "origin/main",
        "ci": {},
    }


def resolved_openapi_spec() -> Path:
    p = reference_example()["openapi_spec"]
    return REPO_ROOT / p if not p.is_absolute() else p


def resolved_backend_dir() -> Path:
    p = reference_example()["backend_dir"]
    return REPO_ROOT / p if not p.is_absolute() else p


def resolved_frontend_dir() -> Path:
    p = reference_example()["frontend_dir"]
    return REPO_ROOT / p if not p.is_absolute() else p


def partition_check_base() -> str:
    return str(reference_example().get("partition_check_base") or "origin/main")


def reference_example_prefixes_for_demo() -> list[str]:
    ps = load_project_settings()
    if ps:
        layout = ps.get("layout") or {}
        return list(layout.get("reference_example_prefixes") or [])
    return list(load_manifest().get("reference_example_prefixes") or [])
