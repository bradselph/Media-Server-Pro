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

/** Strip common media extensions from a filename. */
function stripExtension(name: string): string {
    return name.replace(/\.(mp4|mkv|avi|mov|wmv|flv|webm|m4v|mp3|flac|aac|ogg|wav|opus|m4a|ts|m2ts|vob|rmvb|3gp|asf|divx|xvid|wma|aiff|alac)$/i, '')
}

/**
 * Format a media filename into a readable title. Handles:
 * - Separators (._-) → spaces
 * - CamelCase/PascalCase (MyCoolVideo → My Cool Video)
 * - Letter-number boundaries (Video2 → Video 2)
 * - UPPER_SNAKE (MY_COOL_VIDEO → My Cool Video)
 * - Mixed styles (myCool_Video-01 → My Cool Video 01)
 */
export function formatTitle(name: string): string {
    if (!name || typeof name !== 'string') return ''
    const withoutExt = stripExtension(name.trim())
    if (!withoutExt) return ''

    // Replace separators with spaces
    let spaced = withoutExt.replace(/[._-]+/g, ' ')

    // Split CamelCase / PascalCase: MyCoolVideo → My Cool Video
    spaced = spaced.replace(/([a-z])([A-Z])/g, '$1 $2')
    // Split multiple caps before cap+lower: HTMLLoader → HTML Loader, HDVideo → HD Video
    spaced = spaced.replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    // Split letter-number boundaries: Video2 → Video 2, Episode01 → Episode 01
    spaced = spaced.replace(/([a-zA-Z])(\d+)/g, '$1 $2').replace(/(\d+)([a-zA-Z])/g, '$1 $2')

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
