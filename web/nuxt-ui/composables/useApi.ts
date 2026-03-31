/**
 * Typed API client that unwraps the Go JSON envelope { success, data, message }.
 */

interface GoEnvelope<T> {
  success: boolean
  data?: T
  message?: string
  error?: string
}

class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public body?: unknown,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

let _redirecting = false

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const opts: RequestInit = {
    method,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  }
  if (body !== undefined) {
    opts.body = JSON.stringify(body)
  }

  const res = await fetch(url, opts)

  // Handle non-JSON responses
  const contentType = res.headers.get('content-type') ?? ''
  if (!contentType.includes('application/json')) {
    if (!res.ok) {
      throw new ApiError(`HTTP ${res.status}`, res.status)
    }
    return undefined as T
  }

  const envelope = await res.json() as GoEnvelope<T>

  if (!res.ok || envelope.success === false) {
    // On 401, redirect to login so stale sessions are cleared automatically.
    // Preserve current URL as redirect param so user returns after re-auth.
    // NOTE: use window.location.replace (not navigateTo) — useApi is imported at module
    // level in useApiEndpoints.ts so it must not reference Nuxt composables which require
    // the Nuxt app context; doing so creates a TDZ error in the production bundle.
    if (res.status === 401 && import.meta.client && !_redirecting) {
      _redirecting = true
      const redirect = window.location.pathname + window.location.search
      const isAuthPage = ['/login', '/signup', '/admin-login'].some(p => redirect.startsWith(p))
      const target = redirect && !isAuthPage
        ? `/login?redirect=${encodeURIComponent(redirect)}`
        : '/login'
      window.location.replace(target)
    }
    throw new ApiError(
      envelope.message ?? envelope.error ?? `HTTP ${res.status}`,
      res.status,
      envelope,
    )
  }

  return (envelope.data ?? envelope) as T
}

export function useApi() {
  return {
    get: <T>(url: string) => request<T>('GET', url),
    post: <T>(url: string, body?: unknown) => request<T>('POST', url, body),
    put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body),
    patch: <T>(url: string, body?: unknown) => request<T>('PATCH', url, body),
    delete: <T>(url: string, body?: unknown) => request<T>('DELETE', url, body),
  }
}

export { ApiError }
