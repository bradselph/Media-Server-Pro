// Shared formatting utilities — imported by IndexPage, PlayerPage, and any future consumers.
// D-06: extracted from per-page helper functions to eliminate duplication.
// Domain types below avoid primitive obsession: raw string/number are wrapped with semantic types.

/** Value object: duration in seconds (e.g. media length, playback position). */
export interface Duration {
    readonly seconds: number
}

/** Value object: file size in bytes. */
export interface FileSize {
    readonly bytes: number
}

/** Value object: media filename or display name (path segment or title). */
export interface MediaFileName {
    readonly value: string
}

/** Domain type: a single character (used in boundary/classification helpers). Avoids primitive obsession. */
type Char = string & { readonly __charBrand?: never }

/** Domain type: file extension (e.g. "mp4", "mkv"). */
type FileExtension = string & { readonly __extBrand?: never }

/** Domain type: fragment of a title being processed (name without extension, with separators). */
type TitleFragment = string & { readonly __titleBrand?: never }

function asChar(s: string): Char {
    return s as Char
}
function asTitleFragment(s: string): TitleFragment {
    return s as TitleFragment
}

export function formatDuration(d: Duration): string {
    const secs = d.seconds
    if (!secs || secs <= 0) return '0:00'
    const h = Math.floor(secs / 3600)
    const m = Math.floor((secs % 3600) / 60)
    const s = Math.floor(secs % 60)
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    return `${m}:${String(s).padStart(2, '0')}`
}

const MEDIA_EXTENSIONS: Set<FileExtension> = new Set([
    'mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'mp3', 'flac', 'aac', 'ogg', 'wav',
    'opus', 'm4a', 'ts', 'm2ts', 'vob', 'rmvb', '3gp', 'asf', 'divx', 'xvid', 'wma', 'aiff', 'alac',
])

function isLetter(ch: Char): boolean {
    const code = ch.charCodeAt(0)
    return (code >= 65 && code <= 90) || (code >= 97 && code <= 122)
}
function isDigit(ch: Char): boolean {
    const code = ch.charCodeAt(0)
    return code >= 48 && code <= 57
}
function isUpper(ch: Char): boolean {
    const code = ch.charCodeAt(0)
    return code >= 65 && code <= 90
}
function isLower(ch: Char): boolean {
    const code = ch.charCodeAt(0)
    return code >= 97 && code <= 122
}

/** True when prev and c are uppercase and next is lowercase (e.g. "ABc" → boundary before "c"). */
function isUpperUpperThenLower(prev: Char, c: Char, next: Char): boolean {
    if (!next) return false
    return isUpper(prev) && isUpper(c) && isLower(next)
}

function isMultiCapBoundary(s: TitleFragment, i: number): boolean {
    const prev = asChar(s[i - 1])
    const c = asChar(s[i])
    const next = asChar(s[i + 1])
    return isUpperUpperThenLower(prev, c, next)
}
function isLetterDigitBoundary(s: TitleFragment, i: number): boolean {
    return isLetter(asChar(s[i - 1])) && isDigit(asChar(s[i]))
}
function isDigitLetterBoundary(s: TitleFragment, i: number): boolean {
    return isDigit(asChar(s[i - 1])) && isLetter(asChar(s[i]))
}

/** True if a space should be inserted before s[i] (multi-cap or letter-digit boundary). */
function shouldInsertSpaceBefore(s: TitleFragment, i: number): boolean {
    if (i <= 0) return false
    return isMultiCapBoundary(s, i) || isLetterDigitBoundary(s, i) || isDigitLetterBoundary(s, i)
}

/**
 * Insert spaces at letter-number and multi-cap boundaries without using
 * backtracking-prone regexes (avoids ReDoS).
 */
function insertSpacesAtBoundaries(s: TitleFragment): TitleFragment {
    if (!s) return s
    const out: string[] = []
    for (let i = 0; i < s.length; i += 1) {
        if (shouldInsertSpaceBefore(s, i)) out.push(' ')
        out.push(s[i])
    }
    return out.join('')
}

/** Strip common media extensions from a filename. */
function stripExtension(name: string): TitleFragment {
    const match = name.match(/\.([^.]+)$/i)
    if (match && MEDIA_EXTENSIONS.has(match[1].toLowerCase() as FileExtension)) return asTitleFragment(name.slice(0, -match[0].length))
    return asTitleFragment(name)
}

/**
 * Format a media filename into a readable title. Handles:
 * - Separators (._-) → spaces
 * - CamelCase/PascalCase (MyCoolVideo → My Cool Video)
 * - Letter-number boundaries (Video2 → Video 2)
 * - UPPER_SNAKE (MY_COOL_VIDEO → My Cool Video)
 * - Mixed styles (myCool_Video-01 → My Cool Video 01)
 */
export function formatTitle(name: MediaFileName): string {
    const raw = name?.value ?? ''
    if (!raw) return ''
    const withoutExt = stripExtension(raw.trim())
    if (!withoutExt) return ''

    // Replace separators with spaces
    let spaced = withoutExt.replace(/[._-]+/g, ' ')

    // Split CamelCase / PascalCase: MyCoolVideo → My Cool Video (single-char classes, no backtracking)
    spaced = spaced.replace(/([a-z])([A-Z])/g, '$1 $2')
    // Split multiple caps before cap+lower and letter-number boundaries (loop avoids ReDoS-prone regex)
    spaced = insertSpacesAtBoundaries(asTitleFragment(spaced))

    const normalized = spaced.replace(/\s+/g, ' ').trim()
    return normalized.replace(/\b\w/g, (c) => c.toUpperCase())
}

// fallback is returned when bytes is 0 or falsy.
// IndexPage uses '0 B' (upload contexts); PlayerPage uses '—' (metadata display).
// TODO: Missing 'TB' in sizes array — files or storage values > 1 TB will produce
// `undefined` for the unit because `i` will be 4 but `sizes[4]` doesn't exist.
// The duplicate in `adminUtils.ts` (`formatBytes`) already includes 'TB'.
// WHY: Any media library with > 1 TB total size will show e.g. "1.0 undefined"
// in the UI wherever this function is used (IndexPage file size display, etc.).
// FIX: Add 'TB' to the sizes array: `['B', 'KB', 'MB', 'GB', 'TB']`.
export function formatFileSize(fs: FileSize, fallback = '—'): string {
    const bytes = fs.bytes
    if (!bytes) return fallback
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`
}
