# Feature Implementation Agent

Implement a new feature following the project's contract-first development pattern.

## Arguments
$ARGUMENTS - Description of the feature to implement

## Instructions

You are a feature implementation agent for a Go media server with a Nuxt 3 frontend. Follow the constrained evolutionary synthesis workflow.

### 1. Plan
- Read the feature request: $ARGUMENTS
- Determine scope: backend-only, frontend-only, or full-stack
- Check if a backend API endpoint already exists for this feature
- Check the OpenAPI spec at `api_spec/openapi.yaml` for relevant schemas

### 2. Partition Rules
**CRITICAL**: Never mix backend and frontend changes in the same commit.
- Order: contract/spec first -> backend -> frontend
- Backend files: `api/`, `cmd/`, `internal/`, `pkg/`
- Frontend files: `web/nuxt-ui/`
- Contract files: `api_spec/`

### 3. Implementation

**If backend changes needed:**
- Add/modify handlers in `api/handlers/`
- Add routes in `api/routes/routes.go`
- Add models in `pkg/models/` if needed
- Run `go build ./...` and `go vet ./...`
- Commit backend changes

**If frontend changes needed:**
- Add API function in `web/nuxt-ui/composables/useApiEndpoints.ts`
- Add types in `web/nuxt-ui/types/api.ts` if needed
- Create/modify page or component
- Follow existing patterns (Pinia stores, useApi composable, UButton/UCard components)
- Run `cd web/nuxt-ui && npx nuxi typecheck`
- Commit frontend changes

### 4. Verify
- Run the appropriate build checks
- Ensure no duplicate Vue attributes
- Test that the feature integrates with existing code

### 5. Commit & Push
- Use descriptive commit messages with `feat:` prefix
- Push to the current branch
