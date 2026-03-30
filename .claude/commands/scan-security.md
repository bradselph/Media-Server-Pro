# Security Scan Agent

Perform a security audit of the entire codebase.

## Instructions

You are a security audit agent for a media server that handles user authentication, file streaming, and admin operations.

### 1. Authentication & Authorization
- Check all route handlers have appropriate auth middleware
- Verify admin-only routes check for admin role
- Look for privilege escalation paths (viewer accessing admin endpoints)
- Check session/token handling for fixation or replay vulnerabilities
- Verify password hashing uses bcrypt/argon2 (not MD5/SHA1)

### 2. Input Validation
- Search for SQL injection: string concatenation in database queries
- Search for path traversal: user input used in `os.Open`, `filepath.Join`, etc.
- Search for command injection: user input passed to `exec.Command`
- Search for XSS: `v-html` in Vue templates with user data
- Check file upload validation (type, size, filename sanitization)

### 3. Data Exposure
- Check API responses don't leak file system paths, internal IPs, or stack traces
- Verify error messages don't expose implementation details
- Check that user data is scoped (users can't access other users' data)
- Look for sensitive data in logs (passwords, tokens, API keys)

### 4. Infrastructure
- Check CORS configuration for overly permissive origins
- Verify rate limiting is applied to auth endpoints
- Check for missing security headers (CSP, HSTS, X-Frame-Options)
- Review cookie attributes (HttpOnly, Secure, SameSite)

### 5. Dependencies
- Run `go list -m all` and check for known vulnerable packages
- Check `package.json` for outdated packages with known CVEs

Report findings with:
- Severity (CRITICAL/HIGH/MEDIUM/LOW)
- OWASP category
- File path and line number
- Recommended fix
