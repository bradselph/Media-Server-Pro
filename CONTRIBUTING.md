# Contributing to this framework

The **reconciled operational model** (contract-first pipeline + constrained evolutionary synthesis) is in *
*`docs/CONSOLIDATED_MODEL.md`**. Changes to rules or workflows should stay consistent with that document.

## Partition discipline

- One **commit** must not mix backend and frontend partition roots (see `scripts/check_partition_boundaries.py`).
- Contract changes must not ship in the **same commit** as edits to **both** backend and frontend partitions.
- Optional demo or vendor subtrees may be exempt via `reference_example_prefixes` in `synthesis/partitions.json`; keep
  empty if you only use root partitions.

## Before opening a PR

Run the checks in `README.md` or rely on `.github/workflows/synthesis-ci.yml`.

## Adopting in another repo

See `docs/ADOPTION.md`.
