import {formatFileSize} from '@/utils/formatters'

export function errMsg(err: unknown): string {
    return err instanceof Error ? err.message : String(err)
}

export function formatBytes(bytes: number): string {
    return formatFileSize({ bytes }, '0 B')
}

export function formatUptime(secs: number): string {
    const d = Math.floor(secs / 86400)
    const h = Math.floor((secs % 86400) / 3600)
    const m = Math.floor((secs % 3600) / 60)
    if (d > 0) return `${d}d ${h}h ${m}m`
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
}
