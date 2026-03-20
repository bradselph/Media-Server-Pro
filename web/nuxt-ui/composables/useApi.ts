/**
 * Typed API client for the Go backend.
 *
 * Mirrors the React API client pattern. All Go API responses use the envelope
 * `{ success: boolean, data?: T, error?: string }`. This composable unwraps
 * the envelope and throws on errors so callers get clean typed data or an exception.
 */

/** Custom error class for API errors with HTTP status */
export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

/** Go backend JSON envelope shape */
interface ApiEnvelope<T> {
  success: boolean
  data?: T
  error?: string
}

/**
 * Core fetch wrapper that unwraps the Go API envelope.
 * Returns the unwrapped `data` field on success, throws `ApiError` on failure.
 */
async function apiRequest<T>(
  url: string,
  options?: RequestInit,
): Promise<T> {
  const isFormData = options?.body instanceof FormData
  const response = await fetch(url, {
    ...options,
    credentials: 'include', // Always send session cookies
    headers: isFormData
      ? (options?.headers ?? {})
      : {
          'Content-Type': 'application/json',
          ...options?.headers,
        },
  })

  // 204 No Content
  if (response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  let raw: ApiEnvelope<T>
  try {
    raw = JSON.parse(text) as ApiEnvelope<T>
  } catch {
    const preview = text.slice(0, 100).replace(/\s+/g, ' ')
    throw new ApiError(
      `Invalid JSON (${response.status}): ${preview}${text.length > 100 ? '...' : ''}`,
      response.status,
    )
  }

  // Unwrap Go backend envelope
  if (raw && typeof raw === 'object' && raw.success !== undefined) {
    if (!raw.success) {
      throw new ApiError(
        raw.error || 'Unknown error',
        response.status,
      )
    }
    return raw.data as T
  }

  // Non-envelope response (shouldn't happen with Go backend)
  return raw as unknown as T
}

/** Convenience API methods matching the React client */
export const api = {
  get: <T>(url: string, options?: Pick<RequestInit, 'signal'>) =>
    apiRequest<T>(url, options),

  post: <T>(url: string, body?: unknown, options?: Pick<RequestInit, 'signal'>) =>
    apiRequest<T>(url, {
      method: 'POST',
      body: body !== undefined ? JSON.stringify(body) : undefined,
      ...options,
    }),

  put: <T>(url: string, body?: unknown, options?: Pick<RequestInit, 'signal'>) =>
    apiRequest<T>(url, {
      method: 'PUT',
      body: body !== undefined ? JSON.stringify(body) : undefined,
      ...options,
    }),

  delete: <T>(url: string, body?: unknown, options?: Pick<RequestInit, 'signal'>) =>
    apiRequest<T>(url, {
      method: 'DELETE',
      body: body !== undefined ? JSON.stringify(body) : undefined,
      ...options,
    }),

  /** POST with FormData (for file uploads). Omits Content-Type to let browser set boundary. */
  upload: <T>(url: string, formData: FormData, options?: Pick<RequestInit, 'signal'>) =>
    apiRequest<T>(url, {
      method: 'POST',
      body: formData,
      headers: {},
      ...options,
    }),
}

/**
 * Fetches a blob (e.g. export CSV/file). Uses credentials and throws ApiError
 * on failure for consistent error handling.
 */
export async function fetchBlob(url: string, options?: Pick<RequestInit, 'signal'>): Promise<Blob> {
  const response = await fetch(url, { credentials: 'include', ...options })
  if (!response.ok) {
    const text = await response.text()
    let message = text || `Export failed (${response.status})`
    try {
      const raw = JSON.parse(text) as { error?: string }
      if (raw?.error) message = raw.error
    } catch {
      if (text.length > 0 && text.length < 200) message = text
    }
    throw new ApiError(message, response.status)
  }
  return response.blob()
}

/**
 * Composable wrapper to expose the API client within Vue components.
 * Usage: const { api, fetchBlob, ApiError } = useApi()
 */
export function useApi() {
  return { api, fetchBlob, ApiError }
}
