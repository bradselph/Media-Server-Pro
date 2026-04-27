// Resolve a server-supplied category name to a Lucide icon ID.
// The SPA must not own the taxonomy (that lives on the server),
// so this resolver works by keyword match and falls back to a
// generic folder icon for anything it doesn't recognize. New
// taxonomies (mature-streaming, generic media, etc.) ship without
// code changes — only the icon resolution adapts.

const RULES: ReadonlyArray<readonly [RegExp, string]> = [
  // Mature streaming taxonomy
  [/^trending|hot|popular/i, 'i-lucide-flame'],
  [/amateur|home/i, 'i-lucide-video'],
  [/pov|first.?person/i, 'i-lucide-eye'],
  [/couples?|pairs?/i, 'i-lucide-users'],
  [/solo|individual/i, 'i-lucide-user'],
  [/cosplay|costume/i, 'i-lucide-venetian-mask'],
  [/photo|gallery|image/i, 'i-lucide-images'],

  // Generic media taxonomy
  [/audio|music|podcast|audiobook|sound/i, 'i-lucide-music'],
  [/anime/i, 'i-lucide-star'],
  [/movie|film/i, 'i-lucide-film'],
  [/(tv|show|series|episode)/i, 'i-lucide-tv'],
  [/doc(ument(ary|aries)?)?/i, 'i-lucide-book-open'],
  [/news|journal/i, 'i-lucide-newspaper'],
  [/game|gaming/i, 'i-lucide-gamepad-2'],
  [/educat|tutorial|lecture/i, 'i-lucide-graduation-cap'],
  [/sport/i, 'i-lucide-trophy'],
  [/comedy|stand.?up/i, 'i-lucide-laugh'],
  [/uncateg/i, 'i-lucide-folder'],
]

export function iconForCategory(name: string | null | undefined): string {
  if (!name) return 'i-lucide-folder'
  for (const [re, icon] of RULES) {
    if (re.test(name)) return icon
  }
  return 'i-lucide-folder'
}

// Deterministic gradient swatch used for category tiles when the
// server does not supply a cover image. Pairs with `getItemGradient()`
// in components but is keyed by category name rather than item id.
const SWATCHES: ReadonlyArray<readonly [string, string]> = [
  ['#1a0835', '#9333ea'], ['#081530', '#2563eb'], ['#1a0808', '#dc2626'],
  ['#081508', '#16a34a'], ['#1a1208', '#d97706'], ['#081515', '#0891b2'],
  ['#150815', '#db2777'], ['#0a0815', '#6366f1'], ['#150a0a', '#ea580c'],
  ['#0a1515', '#059669'], ['#0f0a20', '#a855f7'], ['#1a1000', '#ca8a04'],
]

export function gradientForCategory(name: string | null | undefined): string {
  if (!name) return `linear-gradient(148deg, ${SWATCHES[0][0]}, ${SWATCHES[0][1]})`
  let hash = 0
  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) & 0xffff
  const [a, b] = SWATCHES[hash % SWATCHES.length]
  return `linear-gradient(148deg, ${a} 0%, ${b} 100%)`
}
