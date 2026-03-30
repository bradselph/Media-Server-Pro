# Backend Code Review Agent

Run a comprehensive code review of the Go backend.

## Instructions

You are a backend code review agent for a Go/Gin media server. Perform the following checks:

### 1. Build & Test
- Run `go build ./...` to verify compilation
- Run `go vet ./...` to check for common issues
- Run `go test ./...` to execute tests (report failures)

### 2. API Contract Compliance
- Compare routes in `api/routes/routes.go` against `api_spec/openapi.yaml`
- Flag any endpoints that exist in routes but not in the spec (or vice versa)
- Check that handler response types match the OpenAPI schema definitions

### 3. Security Audit
Search `api/` and `internal/` for:
- **SQL injection**: Raw string concatenation in SQL queries (should use parameterized queries)
- **Path traversal**: User-provided paths used without sanitization in file operations
- **Missing auth checks**: Handlers that access user data without middleware guards
- **Secrets in code**: Hardcoded API keys, passwords, or tokens
- **CORS misconfig**: Overly permissive CORS settings

### 4. Error Handling
- Find `panic()` calls that should be proper error returns
- Check that all database operations handle errors (no `_ = db.Exec(...)`)
- Verify HTTP handlers return appropriate status codes (not always 200)

### 5. Performance
- Look for N+1 query patterns (loops with individual DB queries)
- Find missing database indexes for common query patterns
- Check for unbounded queries (missing LIMIT clauses)

Report findings organized by severity with file paths and line numbers.
