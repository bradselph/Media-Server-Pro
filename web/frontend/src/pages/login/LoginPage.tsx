import {type FormEvent, useState} from 'react'
import {Link, useNavigate, useSearchParams} from 'react-router-dom'
import {useAuthStore} from '@/stores/authStore'
import {ApiError} from '@/api/client'
import '@/styles/auth.css'

export function LoginPage() {
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const login = useAuthStore((s) => s.login)
    const allowGuests = useAuthStore((s) => s.allowGuests)

    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [error, setError] = useState('')
    const [isSubmitting, setIsSubmitting] = useState(false)

    // Validate redirect param to prevent open redirect attacks
    const rawRedirect = searchParams.get('redirect') || '/'
    const redirectTo = rawRedirect.startsWith('/') && !rawRedirect.startsWith('//') ? rawRedirect : '/'

    async function handleSubmit(e: FormEvent) {
        e.preventDefault()
        setError('')
        setIsSubmitting(true)

        try {
            const result = await login(username, password)
            if (result.isAdmin) {
                navigate('/admin', {replace: true})
            } else if (redirectTo.startsWith('/player')) {
                // Mature content redirect flow: check if user can actually view it
                const state = useAuthStore.getState()
                if (state.permissions.can_view_mature && state.user?.preferences?.show_mature) {
                    // Full access — go straight to the player
                    navigate(redirectTo, {replace: true})
                } else if (state.permissions.can_view_mature) {
                    // Has permission but show_mature is off — send to profile to enable
                    navigate(`/profile?mature_redirect=${encodeURIComponent(redirectTo)}`, {replace: true})
                } else {
                    // Admin hasn't granted permission — go home
                    navigate('/', {replace: true})
                }
            } else {
                navigate(redirectTo, {replace: true})
            }
        } catch (err: unknown) {
            if (err instanceof ApiError) {
                setError(err.message)
            } else {
                setError('Login failed. Please try again.')
            }
        } finally {
            setIsSubmitting(false)
        }
    }

    return (
        <div className="auth-page">
            <div className="auth-card">
                <h1>Sign In</h1>
                <p className="auth-subtitle">Sign in to your media server account</p>

                {error && <div className="auth-error">{error}</div>}

                <form onSubmit={handleSubmit}>
                    <div className="form-group">
                        <label htmlFor="username">Username</label>
                        <input
                            id="username"
                            type="text"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                            placeholder="Enter your username"
                            required
                            autoComplete="username"
                            autoFocus
                        />
                    </div>

                    <div className="form-group">
                        <label htmlFor="password">Password</label>
                        <input
                            id="password"
                            type="password"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            placeholder="Enter your password"
                            required
                            autoComplete="current-password"
                        />
                    </div>

                    <button type="submit" className="auth-button" disabled={isSubmitting}>
                        {isSubmitting ? 'Signing in...' : 'Sign In'}
                    </button>
                </form>

                <div className="auth-footer">
                    Don't have an account? <Link to="/signup">Sign up</Link>
                </div>

                {allowGuests && (
                    <div className="auth-footer" style={{marginTop: 8}}>
                        <button
                            type="button"
                            className="auth-button"
                            style={{
                                background: 'transparent',
                                border: '1px solid var(--border-color)',
                                color: 'var(--text-muted)'
                            }}
                            onClick={() => navigate(redirectTo)}
                        >
                            Browse as Guest
                        </button>
                    </div>
                )}
            </div>
        </div>
    )
}
