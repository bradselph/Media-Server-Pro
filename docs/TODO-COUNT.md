# TODO audit status

Project-owned Go and frontend (TS/TSX) files have had `// TODO` and `FIXME` comments corrected or replaced with short doc comments. Scripts (deploy.sh, setup.sh), .gitignore, systemd units, and package metadata have been audited in follow-up loops.

To refresh counts:

```bash
grep -r "TODO\|FIXME" --include="*.go" --include="*.ts" --include="*.tsx" . 2>/dev/null | grep -v node_modules | wc -l
```
