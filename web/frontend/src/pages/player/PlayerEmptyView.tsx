import { Link } from 'react-router-dom'
import { ICON_ARROW_LEFT, LABEL_BACK_TO_LIBRARY, CSS_TEXT_MUTED } from './playerConstants'
import { getMediaErrorContent } from './playerErrorContent'
type PlayerEmptyViewProps =
    | { variant: 'no-id' }
    | { variant: 'loading' }
    | { variant: 'error'; is403: boolean; errMsg: string; playerUrl: string }

export function PlayerEmptyView(props: PlayerEmptyViewProps) {
    return (
        <div className="player-page">
            <div className="player-page-container">
                <div className="player-header">
                    <Link to="/" className="player-back-btn"><i className={ICON_ARROW_LEFT}/> {LABEL_BACK_TO_LIBRARY}</Link>
                </div>
                {props.variant === 'no-id' && (
                    <div style={{ textAlign: 'center', padding: '40px 20px' }}>
                        <p style={{ color: CSS_TEXT_MUTED, marginBottom: 16 }}>
                            No media selected. Choose something from the library to play.
                        </p>
                        <Link to="/" className="player-back-btn" style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                            <i className={ICON_ARROW_LEFT} /> {LABEL_BACK_TO_LIBRARY}
                        </Link>
                    </div>
                )}
                {props.variant === 'loading' && (
                    <div style={{ textAlign: 'center', padding: '60px 0', color: CSS_TEXT_MUTED }}>
                        Loading media...
                    </div>
                )}
                {props.variant === 'error' && (
                    <div style={{ textAlign: 'center', padding: '60px 0' }}>
                        {getMediaErrorContent(props.is403, props.errMsg, props.playerUrl)}
                    </div>
                )}
            </div>
        </div>
    )
}
