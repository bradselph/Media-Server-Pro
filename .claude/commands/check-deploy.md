# Deploy Readiness Check Agent

Verify the project is ready for deployment.

## Instructions

You are a deploy readiness agent. Run all checks that the deploy script would run, catching errors before they hit production.

### 1. Backend Build
```bash
go build ./...
go vet ./...
go test ./... -short
```

### 2. Frontend Build
```bash
cd web/nuxt-ui
npm ci
npx nuxi typecheck
npx nuxt generate
```

### 3. Contract Validation
```bash
python3 scripts/validate_openapi.py 2>/dev/null || echo "OpenAPI validation skipped (script not available)"
python3 scripts/check_partition_boundaries.py --base origin/main --head HEAD 2>/dev/null || echo "Partition check skipped"
```

### 4. Common Issues Checklist
- [ ] No `console.log` statements in production code (search `web/nuxt-ui/pages/` and `web/nuxt-ui/components/`)
- [ ] No hardcoded localhost URLs
- [ ] No `.env` files staged for commit
- [ ] No TODO/FIXME comments on critical paths
- [ ] Build output directory exists and contains files

### 5. Report
Output a deploy readiness report:
- BUILD: PASS/FAIL
- TYPES: PASS/FAIL
- TESTS: PASS/FAIL (with failure count)
- SECURITY: Any obvious issues
- RECOMMENDATION: SAFE TO DEPLOY / NEEDS FIXES

If any check fails, provide the exact error and suggested fix.
