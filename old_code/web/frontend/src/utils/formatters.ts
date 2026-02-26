// Shared formatting utilities — imported by IndexPage, PlayerPage, and any future consumers.
// D-06: extracted from per-page helper functions to eliminate duplication.

export function formatDuration(secs: number): string {
    if (!secs || secs <= 0) return '0:00'
    const h = Math.floor(secs / 3600)
    const m = Math.floor((secs % 3600) / 60)
    const s = Math.floor(secs % 60)
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    return `${m}:${String(s).padStart(2, '0')}`
}

export function formatTitle(name: string): string {
    const withoutExt = name.replace(/\.(mp4|mkv|avi|mov|wmv|flv|webm|m4v|mp3|flac|aac|ogg|wav|opus|m4a|ts|m2ts|vob|rmvb|3gp|asf|divx|xvid)$/i, '')
    const spaced = withoutExt.replace(/[._-]+/g, ' ')
    const normalized = spaced.replace(/\s+/g, ' ').trim()
    return normalized.replace(/\b\w/g, c => c.toUpperCase())
}

// fallback is returned when bytes is 0 or falsy.
// IndexPage uses '0 B' (upload contexts); PlayerPage uses '—' (metadata display).
export function formatFileSize(bytes: number, fallback = '—'): string {
    if (!bytes) return fallback
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`
}
