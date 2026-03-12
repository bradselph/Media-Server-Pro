export function errMsg(err: unknown): string {
    return err instanceof Error ? err.message : String(err)
}

// TODO: Duplicate — this `formatBytes` is functionally identical to `formatFileSize`
// in `@/utils/formatters.ts`, except formatters.ts uses the FileSize value object
// pattern and stops at 'GB' while this one includes 'TB'.
// WHY: Two near-identical byte-formatting functions create inconsistency and
// maintenance burden. If one is updated (e.g., to fix rounding), the other won't be.
// FIX: Add 'TB' to the formatters.ts version and replace all usages of this function
// with `formatFileSize({ bytes })` from `@/utils/formatters.ts`.
export function formatBytes(bytes: number): string {
    if (!bytes) return '0 B'
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`
}

export function formatUptime(secs: number): string {
    const d = Math.floor(secs / 86400)
    const h = Math.floor((secs % 86400) / 3600)
    const m = Math.floor((secs % 3600) / 60)
    if (d > 0) return `${d}d ${h}h ${m}m`
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
}
