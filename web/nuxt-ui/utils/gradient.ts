// Deterministic fallback-art gradient derived from a media ID. Rendered behind
// cards / now-playing surfaces when an item has no thumbnail. Centralised here
// so the palette and hashing stay consistent across every surface (previously
// duplicated in index.vue, player.vue, RecommendationRow.vue and
// NowPlayingSidebar.vue, kept in sync by hand).
const PALETTES: [string, string][] = [
  ['#1a0835', '#9333ea'], ['#081530', '#2563eb'], ['#1a0808', '#dc2626'],
  ['#081508', '#16a34a'], ['#1a1208', '#d97706'], ['#081515', '#0891b2'],
  ['#150815', '#db2777'], ['#0a0815', '#6366f1'], ['#150a0a', '#ea580c'],
  ['#0a1515', '#059669'], ['#0f0a20', '#a855f7'], ['#1a1000', '#ca8a04'],
]

export function getMediaGradient(id: string): string {
  let hash = 0
  for (let i = 0; i < id.length; i++) hash = (hash * 31 + id.charCodeAt(i)) & 0xffff
  const [c1, c2] = PALETTES[hash % PALETTES.length]
  return `linear-gradient(135deg, ${c1}, ${c2})`
}
