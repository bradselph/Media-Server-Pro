import {Component, type ReactNode} from 'react'

interface Props {
    children: ReactNode
}

interface State {
    error: Error | null
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
