/**
 * Typed API client for the Go backend.
 *
 * Replaces the vanilla JS `api-compat.js` fetch interceptor. All Go API
 * responses use the envelope `{ success: boolean, data?: T, error?: string }`.
 * This client unwraps the envelope and throws on errors so callers get clean
 * typed data or an exception.
 */

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

interface ApiEnvelope<T> {
    success: boolean
    data?: T
    error?: string
}

/**
 * Core fetch wrapper that unwraps the Go API envelope.
 * Returns the unwrapped `data` field on success, throws `ApiError` on failure.
 *
 * Pass `options.signal` to support request cancellation via AbortController.
 * TanStack Query propagates its own AbortSignal automatically when you
 * forward `signal` from the queryFn argument:
 *
 *   useQuery({ queryFn: ({ signal }) => api.get<T>(url, { signal }) })
 */
export async function apiRequest<T>(
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

    // 204 No Content → undefined (callers of 204 endpoints should use void or T | undefined).
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
            `Invalid JSON (${response.status}): ${preview}${text.length > 100 ? '…' : ''}`,
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

    // Non-envelope response (shouldn't happen with Go backend, but handle gracefully)
    return raw as unknown as T
}

/**
 * Convenience wrappers for common HTTP methods.
 *
 * All methods accept an optional `options` parameter (a subset of `RequestInit`)
 * to allow passing an `AbortSignal` for request cancellation. This integrates
 * with TanStack Query's automatic signal propagation:
 *
 *   useQuery({ queryFn: ({ signal }) => api.get<T>(url, { signal }) })
 *   useMutation({ mutationFn: (body) => api.post<T>(url, body, { signal }) })
 */
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

    /**
     * POST with FormData (for file uploads). Does NOT set Content-Type header
     * so the browser can set the multipart boundary automatically.
     */
    upload: <T>(url: string, formData: FormData, options?: Pick<RequestInit, 'signal'>) =>
        apiRequest<T>(url, {
            method: 'POST',
            body: formData,
            headers: {}, // Override Content-Type to let browser set multipart boundary
            ...options,
        }),
}
