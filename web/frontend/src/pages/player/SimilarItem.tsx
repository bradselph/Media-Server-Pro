import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { Suggestion } from '@/api/types'
import { formatTitle } from '@/utils/formatters'
import { thumbnailUrlWithMatureBuster } from './playerHelpers'

export function SimilarItem({ entry, canViewMature }: { entry: Suggestion; canViewMature: boolean }) {
    const name = formatTitle({ value: entry.title || entry.media_id })
    const thumbUrl = thumbnailUrlWithMatureBuster(entry.thumbnail_url ?? undefined, canViewMature)
    const [thumbFailed, setThumbFailed] = useState(false)
    const isAudio = entry.media_type === 'audio'

    return (
        <Link to={`/player?id=${encodeURIComponent(entry.media_id)}`} className="related-item">
            {thumbUrl && !thumbFailed ? (
                <img
                    className="related-thumb"
                    src={thumbUrl}
                    alt={name}
                    loading="lazy"
                    onError={() => setThumbFailed(true)}
                />
            ) : (
                <div className="related-thumb-placeholder">
                    <i className={isAudio ? 'bi bi-music-note-beamed' : 'bi bi-play-circle'}/>
                </div>
            )}
            <div className="related-info">
                <div className="related-title">{name}</div>
                <div className="related-meta">
                    {entry.category && <span>{entry.category} · </span>}
                    {entry.score !== null && entry.score !== undefined ? `${Math.round(entry.score * 100)}% match` : 'Similar'}
                </div>
            </div>
        </Link>
    )
}
