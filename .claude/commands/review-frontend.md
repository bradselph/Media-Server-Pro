# Frontend Code Review Agent

Run a comprehensive code review of the Nuxt UI frontend (`web/nuxt-ui/`).

## Instructions

You are a frontend code review agent for a Nuxt 3 media server UI. Perform the following checks:

### 1. Build Verification
- Run `cd web/nuxt-ui && npm run typecheck` to catch type errors
- Run `cd web/nuxt-ui && npx nuxt generate` to verify the build succeeds
- Report any errors with file paths and line numbers

### 2. Code Quality Scan
Search the `web/nuxt-ui/` directory for:
- **Memory leaks**: `addEventListener` without matching `removeEventListener` in `onUnmounted`; `setInterval`/`setTimeout` without cleanup
- **Race conditions**: Concurrent API calls without guards; optimistic updates without rollback
- **Silent errors**: `.catch(() => {})` on critical operations (non-cosmetic)
- **Type safety**: `as any`, `as unknown` casts, missing null checks on API responses
- **Duplicate attributes**: Multiple `:class`, `:style`, or other duplicate Vue bindings on single elements
- **v-html usage**: Check for XSS risks from unsanitized user content

### 3. Performance Check
- Look for unthrottled event handlers (`onTimeUpdate`, `onScroll`, `onResize`)
- Find API calls without pagination limits
- Check for unnecessary re-renders from reactive state updates in tight loops

### 4. Regression Check
Compare features between `web/frontend/` (legacy React) and `web/nuxt-ui/` (new Nuxt):
- List any features present in legacy but missing in new UI
- Check that all backend API endpoints in `composables/useApiEndpoints.ts` are actually used in pages/components

Report findings organized by severity: CRITICAL > HIGH > MEDIUM > LOW.
