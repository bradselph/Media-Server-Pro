/**
 * Shared type-narrowing utilities — imported explicitly to avoid Nuxt auto-import TDZ issues.
 */

type AnyRecord = Record<string, unknown>

/**
 * Narrow an unknown value to a plain object (Record).
 * Returns null for primitives, arrays, null, and undefined.
 */
export function asRecord(value: unknown): AnyRecord | null {
    return value !== null && typeof value === 'object' && !Array.isArray(value)
        ? (value as AnyRecord)
        : null
}
