/**
 * Upload status helpers — imported explicitly (like utils/format.ts) so they
 * stay unit-testable in isolation and avoid Nuxt auto-import TDZ issues.
 *
 * The backend's upload.UploadStatus enum (internal/upload/upload.go) only
 * ever emits "uploading", "completed", or "failed" — it never emits "error".
 */

/** Whether the poll loop in pages/upload.vue should stop for this status. */
export function isTerminalUploadStatus(status: string): boolean {
    return status === 'completed' || status === 'failed'
}

/** UBadge color to render for a given upload status. */
export function uploadStatusColor(status: string): 'success' | 'error' | 'warning' {
    if (status === 'completed') return 'success'
    if (status === 'failed') return 'error'
    return 'warning'
}
