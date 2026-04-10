import {decode} from 'blurhash'

// LRU-style cache: keep the last 200 decoded hashes to avoid repeated canvas work
const cache = new Map<string, string>()
const MAX_CACHE = 200

/**
 * Decode a BlurHash string to a CSS-ready data URL (PNG).
 * Returns null when called server-side or when the hash is invalid.
 *
 * @param hash   - BlurHash string from MediaItem.blur_hash
 * @param width  - decode target width in pixels (default 32)
 * @param height - decode target height in pixels (default 18, 16:9 ratio)
 */
export function blurHashToDataUrl(
    hash: string | undefined | null,
    width = 32,
    height = 18,
): string | null {
    if (!hash) return null
    if (typeof document === 'undefined') return null // SSR guard

    const key = `${hash}:${width}:${height}`
    if (cache.has(key)) return cache.get(key) ?? null

    try {
        const pixels = decode(hash, width, height)
        const canvas = document.createElement('canvas')
        canvas.width = width
        canvas.height = height
        const ctx = canvas.getContext('2d')
        if (!ctx) return null
        const imageData = ctx.createImageData(width, height)
        imageData.data.set(pixels)
        ctx.putImageData(imageData, 0, 0)
        const dataUrl = canvas.toDataURL('image/png')

        if (cache.size >= MAX_CACHE) {
            const firstKey = cache.keys().next().value
            if (firstKey !== undefined) cache.delete(firstKey)
        }
        cache.set(key, dataUrl)
        return dataUrl
    } catch {
        return null
    }
}
