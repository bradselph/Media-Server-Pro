import {Component, type ReactNode} from 'react'

interface Props {
    children: ReactNode
}

interface State {
    error: Error | null
}

// ── SectionErrorBoundary ──────────────────────────────────────────────────────

interface SectionProps {
    children: ReactNode
    /** Optional label shown in the error card header. */
    title?: string
}

interface SectionState {
    error: Error | null
}

/**
 * Inline error boundary for isolating individual page sections.
 *
 * Shows a compact, self-contained error card instead of taking over the full
 * page. "Retry" resets the boundary state so the child can re-mount — no page
 * reload is needed. Use this to protect sidebar panels, admin tabs, or any
 * UI section whose failure should not break the surrounding page:
 *
 *   <SectionErrorBoundary title="Similar media">
 *     <SimilarVideosPanel />
 *   </SectionErrorBoundary>
 */
export class SectionErrorBoundary extends Component<SectionProps, SectionState> {
    state: SectionState = {error: null}

    static getDerivedStateFromError(error: Error): SectionState {
        return {error}
    }

    handleRetry = () => {
        this.setState({error: null})
    }

    render() {
        if (this.state.error) {
            return (
                <div style={{
                    padding: 16,
                    border: '1px solid var(--border-color, #2a2a2a)',
                    borderRadius: 8,
                    background: 'var(--bg-secondary, #1a1a1a)',
                    color: 'var(--text-color, #e0e0e0)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 8,
                    alignItems: 'flex-start',
                }}>
                    <div style={{display: 'flex', alignItems: 'center', gap: 8, color: '#ef4444'}}>
                        <i className="bi bi-exclamation-triangle-fill"/>
                        <strong>{this.props.title ?? 'Section unavailable'}</strong>
                    </div>
                    <p style={{margin: 0, fontSize: 13, color: 'var(--text-muted, #888)'}}>
                        {this.state.error.message || 'An unexpected error occurred in this section.'}
                    </p>
                    <button
                        onClick={this.handleRetry}
                        style={{
                            padding: '4px 12px',
                            background: 'transparent',
                            color: '#667eea',
                            border: '1px solid #667eea',
                            borderRadius: 4,
                            cursor: 'pointer',
                            fontSize: 13,
                        }}
                    >
                        <i className="bi bi-arrow-clockwise"/> Retry
                    </button>
                </div>
            )
        }
        return this.props.children
    }
}

/**
 * Catches render errors from lazy-loaded pages and shows a recovery UI.
 * Must be a class component — hooks cannot catch render-phase errors.
 */
export class ErrorBoundary extends Component<Props, State> {
    state: State = {error: null}

    static getDerivedStateFromError(error: Error): State {
        return {error}
    }

    handleReload = () => {
        this.setState({error: null})
        window.location.reload()
    }

    render() {
        if (this.state.error) {
            return (
                <div style={{
                    minHeight: '100vh',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    background: 'var(--bg-color, #0f0f0f)',
                    color: 'var(--text-color, #e0e0e0)',
                    flexDirection: 'column',
                    gap: 16,
                    padding: 24,
                    textAlign: 'center',
                }}>
                    <i className="bi bi-exclamation-triangle-fill" style={{fontSize: 40, color: '#ef4444'}}/>
                    <h2 style={{margin: 0}}>Something went wrong</h2>
                    <p style={{color: 'var(--text-muted, #888)', maxWidth: 400, margin: 0}}>
                        {this.state.error.message || 'An unexpected error occurred.'}
                    </p>
                    <button
                        onClick={this.handleReload}
                        style={{
                            padding: '8px 20px',
                            background: '#667eea',
                            color: '#fff',
                            border: 'none',
                            borderRadius: 6,
                            cursor: 'pointer',
                            fontSize: 14,
                        }}
                    >
                        <i className="bi bi-arrow-clockwise"/> Reload page
                    </button>
                </div>
            )
        }
        return this.props.children
    }
}

/**
 * Minimal full-page spinner shown while a lazy-loaded route chunk is downloading.
 */
export function PageLoader() {
    return (
        <div style={{
            minHeight: '100vh',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'var(--bg-color, #0f0f0f)',
        }}>
            <div style={{
                width: 36,
                height: 36,
                border: '3px solid var(--border-color, #2a2a2a)',
                borderTopColor: '#667eea',
                borderRadius: '50%',
                animation: 'spin 0.7s linear infinite',
            }}/>
            <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        </div>
    )
}
