import {asRecord} from '~/utils/typeGuards'

function asString(value: unknown): string {
    return typeof value === 'string' ? value.trim() : ''
}

// Release-tag / scene-naming noise we want to strip from filename-derived titles.
// Tokens are matched case-insensitively as whole words after the filename has been
// normalised to space-separated chunks. Add new tokens here when new patterns surface.
const NOISE_TOKENS = new Set<string>([
    '1080p', '720p', '480p', '2160p', '4k', '8k', 'uhd', 'hd', 'sd',
    'h264', 'h265', 'x264', 'x265', 'hevc', 'av1', 'xvid', 'divx',
    'webrip', 'webdl', 'web', 'bluray', 'brrip', 'bdrip', 'dvdrip',
    'hdtv', 'hdrip', 'pdtv', 'cam', 'ts', 'tc', 'r5',
    'aac', 'aac2', 'ac3', 'dts', 'mp3', 'flac', 'opus',
    'xxx', 'porn', 'rq', 'remux',
    '10bit', '8bit', 'hdr', 'sdr', 'dv',
    'multi', 'dual', 'subs', 'sub',
])

// Bracketed/parenthesised noise like [1080p], (WEB-DL), {XXX}. We strip the whole
// brace group when its inner content is purely noise tokens; we keep year tags
// like (2024) and meaningful parentheticals.
function stripBraceNoise(s: string): string {
    return s.replace(/[\[\(\{]([^\]\)\}]+)[\]\)\}]/g, (full, inner: string) => {
        const tokens = inner.toLowerCase().split(/[\s._-]+/).filter(Boolean)
        if (tokens.length === 0) return full
        // Keep 4-digit years.
        if (/^\d{4}$/.test(inner.trim())) return full
        const allNoise = tokens.every(t => NOISE_TOKENS.has(t) || /^\d+$/.test(t))
        return allNoise ? ' ' : full
    })
}

// Drop trailing hash-like tokens (8+ hex/alnum chars with no vowel) commonly
// appended by uploaders, e.g. "MyClip - a3f9b2c1d4".
function stripTrailingHash(s: string): string {
    return s.replace(/[\s_-]+[a-f0-9]{8,}$/i, '')
        .replace(/[\s_-]+[A-Za-z0-9]{10,}$/i, (m) => {
            const tail = m.trim().toLowerCase()
            // Only strip if it has no vowels (hash-like). Keep real words like
            // "introduction" intact even though they exceed 10 chars.
            return /[aeiou]/.test(tail) ? m : ''
        })
}

// Remove standalone noise tokens after the string has been space-normalised.
function stripNoiseTokens(s: string): string {
    return s.split(' ')
        .filter(tok => tok && !NOISE_TOKENS.has(tok.toLowerCase()))
        .join(' ')
}

// Returns true when the candidate looks like a random hash/identifier rather
// than a human-meaningful title — no spaces, mixed letters/digits, low vowel
// ratio. Triggers the fallback path that substitutes a generic label.
function looksLikeHash(s: string): boolean {
    const t = s.trim()
    if (!t || t.length < 10) return false
    if (/\s/.test(t)) return false
    const letters = t.match(/[A-Za-z]/g)?.length ?? 0
    const digits = t.match(/\d/g)?.length ?? 0
    if (letters === 0 || digits === 0) {
        // Pure-digit or pure-letter ids of length >= 12 are still hash-like.
        if (t.length < 12) return false
    }
    const vowels = t.match(/[aeiouAEIOU]/g)?.length ?? 0
    const ratio = letters > 0 ? vowels / letters : 0
    return ratio < 0.18
}

function titleCaseIfFlat(s: string): string {
    // If the whole string is single-case (all lower or all upper) AND has no
    // mixed-case markers, apply a light title-case pass. Leaves intentional
    // ALL CAPS alone if it contains punctuation or numerals that suggest the
    // user typed it that way.
    const hasUpper = /[A-Z]/.test(s)
    const hasLower = /[a-z]/.test(s)
    if (hasUpper && hasLower) return s
    return s.replace(/\b([a-z])([a-z]*)\b/gi, (_, h: string, t: string) =>
        h.toUpperCase() + t.toLowerCase())
}

function cleanupFilenameLikeTitle(input: string): string {
    const raw = input.trim()
    if (!raw) return ''

    const slash = Math.max(raw.lastIndexOf('/'), raw.lastIndexOf('\\'))
    const base = slash >= 0 ? raw.slice(slash + 1) : raw
    const noExt = base.replace(/\.[A-Za-z0-9]{2,5}$/, '')
    // Strip download/recording timestamp suffix, e.g. _2026-03-24T21-54-54
    const noTimestamp = noExt.replace(/_\d{4}[-_]\d{2}[-_]\d{2}T\d{2}[-_]\d{2}[-_]\d{2}$/, '')
    const noBrace = stripBraceNoise(noTimestamp)
    const noHash = stripTrailingHash(noBrace)
    const normalized = noHash
        .replaceAll(/[_.-]+/g, ' ')
        // Split camelCase / PascalCase: insert space between a lowercase letter and an uppercase letter
        .replaceAll(/([a-z])([A-Z])/g, '$1 $2')
        // Split boundary between a letter and a digit (and vice versa)
        .replaceAll(/([a-zA-Z])(\d)/g, '$1 $2')
        .replaceAll(/(\d)([a-zA-Z])/g, '$1 $2')
        .replaceAll(/\s+/g, ' ')
        .trim()

    const denoised = stripNoiseTokens(normalized).trim()
    const cased = titleCaseIfFlat(denoised || normalized)
    return cased || base
}

/**
 * Resolve a stable display title across mixed backend payloads.
 * Order:
 * 1) Explicit title/name/media_name (cleaned if filename-like)
 * 2) metadata.title
 * 3) filename/media_path/path (always cleaned)
 * 4) media_id/id — but hash-like ids fall back to a generic label
 */
export function getDisplayTitle(item: unknown): string {
    const rec = asRecord(item)
    if (!rec) return ''

    const direct =
        asString(rec.title) ||
        asString(rec.name) ||
        asString(rec.media_name)
    if (direct) {
        // If this looks like a filename, normalize for UI readability.
        const looksFilenameLike = /\.[A-Za-z0-9]{2,5}$/.test(direct) || /[_.]/.test(direct)
        const cleaned = looksFilenameLike ? cleanupFilenameLikeTitle(direct) : direct
        if (cleaned && !looksLikeHash(cleaned)) return cleaned
        // Direct value was hash-like even after cleanup — fall through to other
        // hints (metadata.title, filename, type-based fallback) before giving up.
    }

    const metadata = asRecord(rec.metadata)
    const metaTitle = metadata ? asString(metadata.title) : ''
    if (metaTitle && !looksLikeHash(metaTitle)) return metaTitle

    const filenameLike =
        asString(rec.filename) ||
        asString(rec.media_path) ||
        asString(rec.path)
    if (filenameLike) {
        const cleaned = cleanupFilenameLikeTitle(filenameLike)
        if (cleaned && !looksLikeHash(cleaned)) return cleaned
    }

    // Hash-like fallback: synthesise a label from type/category instead of
    // dumping the raw id on the user. Examples: "Untitled video", "Untitled
    // audio · Compilations".
    const type = asString(rec.type) || asString(rec.media_type)
    const category = asString(rec.category)
    if (type || category) {
        const kind = type ? type.charAt(0).toUpperCase() + type.slice(1) : 'Item'
        return category ? `Untitled ${kind} · ${category}` : `Untitled ${kind}`
    }

    return asString(rec.media_id) || asString(rec.id) || 'Untitled'
}
