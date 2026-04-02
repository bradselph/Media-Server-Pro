/**
 * Shared formatting utilities — imported explicitly to avoid Nuxt auto-import TDZ issues.
 */

/**
 * Format a byte count into a human-readable string.
 * @param bytes  Raw byte value (optional / undefined)
 * @param fallback  String to return when bytes is falsy (default '—')
 */
export function formatBytes(bytes?: number, fallback = '—'): string {
  if (!bytes) return fallback
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

/**
 * Format seconds into HH:MM:SS / MM:SS clock display.
 * Used in media cards, player seek bar, admin media tables.
 * @param secs  Duration in seconds (optional / undefined)
 * @param fallback  String to return when secs is falsy (default '')
 */
export function formatDuration(secs?: number, fallback = ''): string {
  if (!secs) return fallback
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  const s = Math.floor(secs % 60)
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`
}

/**
 * Format seconds into a human-readable watch time string (e.g. "2h 34m").
 * Used in analytics, profile watch stats, discovery tab.
 * @param secs  Duration in seconds (optional / undefined)
 * @param fallback  String to return when secs is falsy (default '—')
 */
export function formatWatchTime(secs?: number, fallback = '—'): string {
  if (!secs) return fallback
  if (secs < 60) return `${Math.round(secs)}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m`
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

/**
 * Format seconds into a concise uptime string (e.g. "3d 2h 15m").
 * Used in admin dashboard and downloader health displays.
 * @param secs  Uptime in seconds
 * @param fallback  String to return when secs is falsy (default '—')
 */
export function formatUptime(secs?: number, fallback = '—'): string {
  if (!secs) return fallback
  const d = Math.floor(secs / 86400)
  const h = Math.floor((secs % 86400) / 3600)
  const m = Math.floor((secs % 3600) / 60)
  return d > 0 ? `${d}d ${h}h ${m}m` : h > 0 ? `${h}h ${m}m` : `${m}m`
}
