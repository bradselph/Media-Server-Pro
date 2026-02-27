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
 */
export async function apiRequest<T>(
    url: string,
    options?: RequestInit,
): Promise<T> {
    const response = await fetch(url, {
        ...options,
        credentials: 'include', // Always send session cookies
        headers: {
            'Content-Type': 'application/json',
            ...options?.headers,
        },
    })

    // Handle non-JSON responses (e.g. 204 No Content)
    if (response.status === 204) {
        return undefined as T
    }

    const raw: ApiEnvelope<T> = await response.json()

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
 */
export const api = {
    get: <T>(url: string) => apiRequest<T>(url),

    post: <T>(url: string, body?: unknown) =>
        apiRequest<T>(url, {
            method: 'POST',
            body: body !== undefined ? JSON.stringify(body) : undefined,
        }),

    put: <T>(url: string, body?: unknown) =>
        apiRequest<T>(url, {
            method: 'PUT',
            body: body !== undefined ? JSON.stringify(body) : undefined,
        }),

    delete: <T>(url: string, body?: unknown) =>
        apiRequest<T>(url, {
            method: 'DELETE',
            body: body !== undefined ? JSON.stringify(body) : undefined,
        }),

    /**
     * POST with FormData (for file uploads). Does NOT set Content-Type header
     * so the browser can set the multipart boundary automatically.
     */
    upload: <T>(url: string, formData: FormData) =>
        apiRequest<T>(url, {
            method: 'POST',
            body: formData,
            headers: {}, // Override Content-Type to let browser set multipart boundary
        }),
}
