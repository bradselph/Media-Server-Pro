import type { ReactElement } from 'react'
import { Link } from 'react-router-dom'
import { ERROR_MSG_MATURE, ERROR_STYLE_HEADING, ICON_ARROW_LEFT, LABEL_BACK_TO_LIBRARY, CSS_TEXT_MUTED } from './playerConstants'

export function getMediaErrorContent(is403: boolean, errMsg: string, playerUrl: string): ReactElement {
    if (is403 && errMsg.includes('log in')) {
        return (
            <>
                <p style={ERROR_STYLE_HEADING}>
                    <i className="bi bi-shield-lock-fill"/> {ERROR_MSG_MATURE}
                </p>
                <Link to={`/login?redirect=${encodeURIComponent(playerUrl)}`}>
                    Sign in to view this content
                </Link>
            </>
        )
    }
    if (is403 && errMsg.includes('permission')) {
        return (
            <>
                <p style={ERROR_STYLE_HEADING}>
                    <i className="bi bi-shield-lock-fill"/> Your account does not have permission to view mature content.
                </p>
                <p style={{ color: CSS_TEXT_MUTED, fontSize: 14 }}>Contact an administrator to request access.</p>
                <Link to="/" style={{ marginTop: 12, display: 'inline-block' }}><i className={ICON_ARROW_LEFT}/> {LABEL_BACK_TO_LIBRARY}</Link>
            </>
        )
    }
    const isMaturePreferencesError = errMsg.includes('Enable') || errMsg.includes('preferences')
    if (is403 && isMaturePreferencesError) {
        return (
            <>
                <p style={ERROR_STYLE_HEADING}>
                    <i className="bi bi-shield-lock-fill"/> {ERROR_MSG_MATURE}
                </p>
                <Link to={`/profile?mature_redirect=${encodeURIComponent(playerUrl)}`}>
                    Enable mature content in profile settings
                </Link>
            </>
        )
    }
    return (
        <>
            <p style={ERROR_STYLE_HEADING}>Media not found or unavailable.</p>
            <Link to="/"><i className={ICON_ARROW_LEFT}/> {LABEL_BACK_TO_LIBRARY}</Link>
        </>
    )
}
