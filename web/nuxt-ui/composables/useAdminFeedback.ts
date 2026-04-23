import type { ModuleHealth } from '~/types/api'

type BulkResult = { success: number; failed: number }

/**
 * Shared toast/notification helpers for admin panel components.
 * Consolidates the `toast.add({ title, color, icon })` boilerplate that was
 * duplicated 200+ times across admin tabs.
 */
export function useAdminFeedback() {
    const toast = useToast()

    /**
     * Show an error toast. Accepts either:
     *  - a caught exception plus a fallback message (typical `catch (e)` pattern), or
     *  - a plain string for literal error messages (e.g. validation errors).
     */
    function notifyError(errOrMsg: unknown, fallback = 'Error') {
        const title =
            typeof errOrMsg === 'string' ? errOrMsg
            : errOrMsg instanceof Error ? errOrMsg.message
            : fallback
        toast.add({ title, color: 'error', icon: 'i-lucide-x' })
    }

    /** Show a success toast. */
    function notifySuccess(title: string) {
        toast.add({ title, color: 'success', icon: 'i-lucide-check' })
    }

    /** Show a warning toast. */
    function notifyWarning(title: string) {
        toast.add({ title, color: 'warning', icon: 'i-lucide-alert-triangle' })
    }

    /** Show an info toast. */
    function notifyInfo(title: string) {
        toast.add({ title, color: 'info', icon: 'i-lucide-info' })
    }

    /**
     * Summarize a bulk operation result. Uses warning color when any failures
     * occurred, success otherwise. Matches the `{verb}: X succeeded, Y failed`
     * format used across admin bulk-action flows.
     */
    function notifyBulkResult(res: BulkResult, verb: string) {
        toast.add({
            title: `${verb}: ${res.success} succeeded, ${res.failed} failed`,
            color: res.failed > 0 ? 'warning' : 'success',
            icon: res.failed > 0 ? 'i-lucide-alert-triangle' : 'i-lucide-check',
        })
    }

    return { notifyError, notifySuccess, notifyWarning, notifyInfo, notifyBulkResult }
}

/**
 * Map a ModuleHealth status to a badge color. Shared by DashboardTab and SystemStatusPanel.
 */
export function moduleStatusColor(status: ModuleHealth['status']): 'success' | 'warning' | 'error' {
    if (status === 'healthy') return 'success'
    if (status === 'degraded') return 'warning'
    return 'error'
}
