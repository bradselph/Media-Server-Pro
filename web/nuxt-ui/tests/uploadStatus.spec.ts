import {describe, expect, it} from 'vitest'
import {isTerminalUploadStatus, uploadStatusColor} from '~/utils/uploadStatus'

// The backend's upload.UploadStatus enum (internal/upload/upload.go) only ever
// emits "uploading", "completed", or "failed" — it never emits "error". The
// upload progress poll previously checked for "error" instead of "failed", so
// a failed upload polled forever (up to the full attempt budget) and rendered
// with the "warning" badge color instead of "error". These tests pin the fix
// to the exact strings the backend produces.
describe('isTerminalUploadStatus', () => {
    it('stops polling once the backend reports "failed"', () => {
        expect(isTerminalUploadStatus('failed')).toBe(true)
    })

    it('stops polling once the backend reports "completed"', () => {
        expect(isTerminalUploadStatus('completed')).toBe(true)
    })

    it('keeps polling while the backend reports "uploading"', () => {
        expect(isTerminalUploadStatus('uploading')).toBe(false)
    })

    it('does not treat the non-existent "error" status as terminal', () => {
        expect(isTerminalUploadStatus('error')).toBe(false)
    })
})

describe('uploadStatusColor', () => {
    it('renders "failed" as the error color, not warning', () => {
        expect(uploadStatusColor('failed')).toBe('error')
    })

    it('renders "completed" as the success color', () => {
        expect(uploadStatusColor('completed')).toBe('success')
    })

    it('renders in-progress "uploading" as the warning color', () => {
        expect(uploadStatusColor('uploading')).toBe('warning')
    })
})
