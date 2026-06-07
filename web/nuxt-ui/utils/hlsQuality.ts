// Display name for an HLS quality level. Appends a high-frame-rate suffix
// (e.g. "1080p60") only when fps exceeds 30 — standard 24/25/30 is implied,
// matching the YouTube convention. Shared so the quality dropdown and the
// active-quality readouts label the same level identically.
export function hlsQualityName(q: { name: string; fps?: number } | null | undefined): string {
  if (!q) return 'Auto'
  return q.fps && q.fps > 30 ? `${q.name}${q.fps}` : q.name
}
