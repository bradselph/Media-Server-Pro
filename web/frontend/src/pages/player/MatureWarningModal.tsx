import { Link, useNavigate } from 'react-router-dom'
import { ICON_ARROW_LEFT } from './playerConstants'

type MatureWarningModalProps = {
    onAccept: () => void
}

export function MatureWarningModal({ onAccept }: MatureWarningModalProps) {
    const navigate = useNavigate()
    return (
        <div className="mature-modal-overlay">
            <div className="mature-modal-box">
                <div className="mature-modal-header"><i className="bi bi-exclamation-triangle-fill"/> 18+
                    Content Warning
                </div>
                <div className="mature-modal-body">
                    <div className="mature-modal-icon"><i className="bi bi-exclamation-triangle-fill"/></div>
                    <h3>Adult Content Ahead</h3>
                    <p>
                        This media has been marked as containing mature/adult content (18+/NSFW).
                        By continuing, you confirm that you are at least 18 years old.
                    </p>
                    <div className="mature-modal-actions">
                        <button
                            className="media-action-btn media-action-btn-primary"
                            style={{ background: '#dc3545', borderColor: '#dc3545' }}
                            onClick={onAccept}
                        >
                            <i className="bi bi-check-circle"/> I am 18+, Continue
                        </button>
                        <button className="media-action-btn" onClick={() => { navigate('/'); }}>
                            <i className={ICON_ARROW_LEFT}/> Go Back
                        </button>
                    </div>
                    <p className="mature-modal-note">
                        You can disable this warning in your <Link to="/profile">profile settings</Link>.
                    </p>
                </div>
            </div>
        </div>
    )
}
