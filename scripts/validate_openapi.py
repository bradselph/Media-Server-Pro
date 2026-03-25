#!/usr/bin/env python3
"""Validate the OpenAPI spec path from synthesis/partitions.json (reference_example)."""

from __future__ import annotations

import sys
from pathlib import Path

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

import yaml
from openapi_spec_validator import validate_spec

from _synthesis_config import resolved_openapi_spec


def main() -> None:
    spec_path = resolved_openapi_spec()
    if not spec_path.is_file():
        print(f"Missing OpenAPI spec: {spec_path}", file=sys.stderr)
        sys.exit(1)
    with open(spec_path, encoding="utf-8") as f:
        spec = yaml.safe_load(f)
    validate_spec(spec)
    print(f"OpenAPI spec valid: {spec_path}")


if __name__ == "__main__":
    main()
