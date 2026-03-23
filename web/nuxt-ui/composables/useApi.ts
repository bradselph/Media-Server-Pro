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
