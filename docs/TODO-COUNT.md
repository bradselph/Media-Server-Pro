# TODO audit status

Project-owned Go and frontend (TS/TSX) files have had `// TODO` and `FIXME` comments corrected or replaced with short doc comments during the audit. Remaining TODOs may exist in `node_modules/`, `CHANGELOG.md` (historical), and a few config/deploy scripts.

To refresh counts:

```bash
grep -r "TODO\|FIXME" --include="*.go" --include="*.ts" --include="*.tsx" . 2>/dev/null | grep -v node_modules | wc -l
```
