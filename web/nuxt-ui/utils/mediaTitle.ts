type UnknownRecord = Record<string, unknown>

function asRecord(value: unknown): UnknownRecord | null {
  return value && typeof value === 'object' ? (value as UnknownRecord) : null
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function cleanupFilenameLikeTitle(input: string): string {
  const raw = input.trim()
  if (!raw) return ''

  const slash = Math.max(raw.lastIndexOf('/'), raw.lastIndexOf('\\'))
  const base = slash >= 0 ? raw.slice(slash + 1) : raw
  const noExt = base.replace(/\.[A-Za-z0-9]{2,5}$/, '')
  const normalized = noExt
    .replace(/[_\.]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()

  return normalized || base
}

/**
 * Resolve a stable display title across mixed backend payloads.
 * Order:
 * 1) Explicit title/name/media_name
 * 2) metadata.title
 * 3) filename/media_path/path
 * 4) media_id/id
 */
export function getDisplayTitle(item: unknown): string {
  const rec = asRecord(item)
  if (!rec) return ''

  const direct =
    asString(rec.title) ||
    asString(rec.name) ||
    asString(rec.media_name)
  if (direct) return direct

  const metadata = asRecord(rec.metadata)
  const metaTitle = metadata ? asString(metadata.title) : ''
  if (metaTitle) return metaTitle

  const filenameLike =
    asString(rec.filename) ||
    asString(rec.media_path) ||
    asString(rec.path)
  if (filenameLike) return cleanupFilenameLikeTitle(filenameLike)

  return asString(rec.media_id) || asString(rec.id)
}
