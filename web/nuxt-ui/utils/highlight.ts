// Wrap every case-insensitive occurrence of `needle` inside `haystack` with
// <mark> for the search-results highlight (checklist §7). HTML in the input
// is escaped first so an attacker-controlled title cannot inject script tags
// when the result is rendered with v-html.

const HTML_ESCAPE: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    '\'': '&#39;',
}

function escapeHtml(s: string): string {
    return s.replace(/[&<>"']/g, ch => HTML_ESCAPE[ch] ?? ch)
}

function escapeRegExp(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * Returns an HTML string with every case-insensitive match of `needle` in
 * `haystack` wrapped in `<mark>`. Both inputs are HTML-escaped before the
 * regex runs, so the result is safe to feed to v-html.
 *
 * If `needle` is empty/whitespace, the haystack is returned escaped as-is.
 */
export function highlightMatch(haystack: string, needle: string): string {
    const safeHaystack = escapeHtml(haystack ?? '')
    const trimmed = (needle ?? '').trim()
    if (!trimmed) return safeHaystack
    const safeNeedle = escapeHtml(trimmed)
    const pattern = new RegExp(`(${escapeRegExp(safeNeedle)})`, 'gi')
    return safeHaystack.replace(pattern, '<mark class="bg-primary/20 text-default rounded px-0.5">$1</mark>')
}
