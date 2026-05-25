// Build a safe JSON-LD payload string for embedding inside a
// <script type="application/ld+json"> tag via useHead's script.children.
//
// JSON.stringify does not escape the '<' character, so a media title or
// description containing the close-script sequence would terminate the
// JSON-LD block early and allow injected HTML to execute in <head>.
// Replace every '<' with the JSON-safe Unicode escape so the JSON
// parser still decodes it back to '<' but the HTML parser never sees
// the close-tag boundary inside the LD+JSON content.
//
// This helper lives in utils/ rather than directly inside the .vue
// page because Vue's macro pre-parser (?macro=true) trips on literal
// '<' characters in script-block source — including ones inside string
// literals, regex literals, and comments — and reports them as
// "Invalid end tag" / "Element is missing end tag" build failures.
const LT = String.fromCharCode(60)

export function safeJsonLD(obj: unknown): string {
    return JSON.stringify(obj).split(LT).join('\\u003c')
}
