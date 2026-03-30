# API Parity Review Agent

Verify that the frontend exposes all backend API capabilities to users.

## Instructions

You are an API parity review agent. Your job is to find backend capabilities that the frontend doesn't expose.

### 1. Catalog Backend Endpoints
Read `api/routes/routes.go` and list every endpoint with its HTTP method and path.

### 2. Catalog Frontend API Calls
Read `web/nuxt-ui/composables/useApiEndpoints.ts` and list every API function and the endpoint it calls.

### 3. Find Frontend Usage
For each API function in `useApiEndpoints.ts`, search `web/nuxt-ui/pages/` and `web/nuxt-ui/components/` to verify it's actually called from the UI. Flag any that are defined but never used.

### 4. Find Unused Backend Capabilities
Cross-reference the backend endpoints against the frontend API calls. For each backend endpoint NOT called by the frontend, determine:
- Is it an admin-only endpoint? (check if route uses admin middleware)
- Is it a user-facing endpoint that should be exposed?
- Is it an internal/system endpoint that doesn't need UI?

### 5. Report
Output a table with columns:
| Backend Endpoint | Frontend Function | Used In UI | Priority |

Priority levels:
- **HIGH**: User-facing feature with no UI (e.g., subtitle selection, comments)
- **MEDIUM**: Admin feature not exposed (e.g., missing admin panel tab)
- **LOW**: System/internal endpoint (health checks, metrics)
- **OK**: Fully implemented
