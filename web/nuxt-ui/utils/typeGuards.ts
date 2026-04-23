/**
 * Shared type-narrowing utilities — imported explicitly to avoid Nuxt auto-import TDZ issues.
 */

type AnyRecord = Record<string, unknown>

/**
 * Narrow an unknown value to a plain object (Record).
 * Returns null for primitives, arrays, null, and undefined.
 */
export function asRecord(value: unknown): AnyRecord | null {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) return null
    const proto = Object.getPrototypeOf(value)
    return proto === Object.prototype || proto === null ? (value as AnyRecord) : null
}
