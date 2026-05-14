#!/usr/bin/env python3
"""Merge a KEY=VALUE payload into an existing .env file.

Used by deploy.sh to forward FORWARDED_RUNTIME knobs from the operator's
local .deploy.env into $DEPLOY_DIR/.env on the VPS.

Rules:
  - For each KEY=VALUE pair in the payload, replace the first uncommented
    line in the target that starts with `KEY=`. Commented hint lines
    (`# KEY=...`) are preserved as documentation.
  - If the target has no matching uncommented line, append `KEY=VALUE` at
    the end.
  - Values are taken verbatim from the payload — single quotes, spaces,
    equals signs in the value are all fine.
  - Empty values are written as `KEY=` (the operator explicitly wants
    the field to exist but be empty). deploy.sh decides upstream whether
    to emit a knob at all.

Usage:
  python3 knobs-merge.py PAYLOAD_PATH TARGET_PATH

Atomic write: writes to TARGET.tmp then renames over TARGET so a crash
mid-write can never leave a half-merged .env.
"""

import os
import sys


def parse_payload(path):
    """Return [(key, value), ...] in payload order. Later entries for the
    same key win — the last assignment is the only one that ends up in
    the target."""
    pairs = []
    seen = {}
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n").rstrip("\r")
            if not line or line.lstrip().startswith("#"):
                continue
            if "=" not in line:
                continue
            key, _, value = line.partition("=")
            key = key.strip()
            if not key:
                continue
            if key in seen:
                pairs[seen[key]] = (key, value)
            else:
                seen[key] = len(pairs)
                pairs.append((key, value))
    return pairs


def merge(payload_path, target_path):
    pairs = parse_payload(payload_path)
    if not pairs:
        return 0
    updates = dict(pairs)
    order = [k for k, _ in pairs]

    existing = []
    if os.path.exists(target_path):
        with open(target_path, "r", encoding="utf-8") as f:
            existing = f.read().splitlines()

    out = []
    seen = set()
    for line in existing:
        stripped = line.lstrip()
        if stripped and not stripped.startswith("#") and "=" in stripped:
            k = stripped.split("=", 1)[0].strip()
            if k in updates and k not in seen:
                out.append("{}={}".format(k, updates[k]))
                seen.add(k)
                continue
        out.append(line)

    for k in order:
        if k not in seen:
            out.append("{}={}".format(k, updates[k]))
            seen.add(k)

    tmp = target_path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        f.write("\n".join(out))
        if out and not out[-1].endswith("\n"):
            f.write("\n")
    os.replace(tmp, target_path)
    return len(pairs)


def main():
    if len(sys.argv) != 3:
        print("usage: knobs-merge.py PAYLOAD TARGET", file=sys.stderr)
        sys.exit(2)
    payload_path, target_path = sys.argv[1], sys.argv[2]
    if not os.path.exists(payload_path):
        print("payload not found: {}".format(payload_path), file=sys.stderr)
        sys.exit(2)
    n = merge(payload_path, target_path)
    print("[knobs-merge] {} knob(s) merged into {}".format(n, target_path))


if __name__ == "__main__":
    main()
