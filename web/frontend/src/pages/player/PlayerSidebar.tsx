import { SectionErrorBoundary } from '@/components/ErrorBoundary'
import type { Suggestion } from '@/api/types'
import { SimilarItem } from './SimilarItem'
import { CSS_TEXT_MUTED } from './playerConstants'

type PlayerSidebarProps = {
    relatedLabel: string
    relatedStillLoading: boolean
    similarError: boolean
    similarRefetch: () => void
    related: Suggestion[]
    canViewMature: boolean
    user: { id: string } | null
    handleRate: (rating: number) => void
    ratingHover: number
    setRatingHover: (v: number) => void
    userRating: number
}

const MUTED_STYLE = { color: CSS_TEXT_MUTED, fontSize: 13 }

function RelatedSection({
    relatedStillLoading,
    similarError,
    similarRefetch,
    related,
    canViewMature,
}: Pick<
    PlayerSidebarProps,
    'relatedStillLoading' | 'similarError' | 'similarRefetch' | 'related' | 'canViewMature'
>) {
    if (relatedStillLoading) return <p style={MUTED_STYLE}>Loading…</p>
    if (similarError) {
        return (
            <p style={MUTED_STYLE}>
                Suggestions still loading.{' '}
                <button
                    type="button"
                    className="media-action-btn"
                    style={{ marginTop: 4 }}
                    onClick={() => similarRefetch()}
                >
                    Retry
                </button>
            </p>
        )
    }
    if (related.length === 0) {
        return <p style={MUTED_STYLE}>No suggestions yet. Add more media to your library.</p>
    }
    return (
        <>
            {related.map((entry) => (
                <SimilarItem key={entry.media_id} entry={entry} canViewMature={canViewMature} />
            ))}
        </>
    )
}

function RatingSection({
    handleRate,
    ratingHover,
    setRatingHover,
    userRating,
}: Pick<
    PlayerSidebarProps,
    'handleRate' | 'ratingHover' | 'setRatingHover' | 'userRating'
>) {
    const isActive = (star: number) => star <= (ratingHover || userRating)
    return (
        <div className="player-sidebar-card">
            <h3><i className="bi bi-star-fill" /> Rate This</h3>
            <div style={{ display: 'flex', gap: 4, marginTop: 4 }}>
                {[1, 2, 3, 4, 5].map((star) => (
                    <button
                        key={star}
                        onClick={() => handleRate(star)}
                        onMouseEnter={() => setRatingHover(star)}
                        onMouseLeave={() => setRatingHover(0)}
                        style={{
                            background: 'none',
                            border: 'none',
                            cursor: 'pointer',
                            fontSize: 22,
                            padding: '2px 3px',
                            color: isActive(star) ? '#f59e0b' : CSS_TEXT_MUTED,
                            transition: 'color 0.15s',
                        }}
                        title={`Rate ${star} star${star !== 1 ? 's' : ''}`}
                    >
                        <i className={`bi bi-star${isActive(star) ? '-fill' : ''}`} />
                    </button>
                ))}
            </div>
            {userRating > 0 && (
                <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
                    You rated this {userRating}/5
                </p>
            )}
        </div>
    )
}

export function PlayerSidebar(props: PlayerSidebarProps) {
    const {
        relatedLabel,
        relatedStillLoading,
        similarError,
        similarRefetch,
        related,
        canViewMature,
        user,
        handleRate,
        ratingHover,
        setRatingHover,
        userRating,
    } = props

    return (
        <SectionErrorBoundary title="Sidebar unavailable">
            <div className="player-sidebar">
                <div className="player-sidebar-card">
                    <h3><i className="bi bi-play-fill" /> {relatedLabel}</h3>
                    <RelatedSection
                        relatedStillLoading={relatedStillLoading}
                        similarError={similarError}
                        similarRefetch={similarRefetch}
                        related={related}
                        canViewMature={canViewMature}
                    />
                </div>

                {user ? (
                    <RatingSection
                        handleRate={handleRate}
                        ratingHover={ratingHover}
                        setRatingHover={setRatingHover}
                        userRating={userRating}
                    />
                ) : null}

                <div className="player-sidebar-card player-shortcuts-card">
                    <h3><i className="bi bi-keyboard" aria-hidden /> Shortcuts</h3>
                    <div className="player-shortcuts-grid">
                        <kbd>Space</kbd> <span>Play/Pause</span>
                        <kbd>K</kbd> <span>Play/Pause</span>
                        <kbd>J</kbd> <span>Back 10s</span>
                        <kbd>L</kbd> <span>Forward 10s</span>
                        <kbd>←</kbd> <span>Back 5s</span>
                        <kbd>→</kbd> <span>Forward 5s</span>
                        <kbd>Home</kbd> <span>Start</span>
                        <kbd>End</kbd> <span>End</span>
                        <kbd>↑</kbd> <span>Volume up</span>
                        <kbd>↓</kbd> <span>Volume down</span>
                        <kbd>F</kbd> <span>Fullscreen</span>
                        <kbd>T</kbd> <span>Theater</span>
                        <kbd>M</kbd> <span>Mute</span>
                        <kbd>0-9</kbd> <span>Seek to %</span>
                        <kbd>&lt; &gt;</kbd> <span>Speed</span>
                        <kbd>Esc</kbd> <span>Close settings</span>
                    </div>
                </div>
            </div>
        </SectionErrorBoundary>
    )
}
