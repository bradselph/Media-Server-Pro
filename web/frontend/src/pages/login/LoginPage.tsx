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

    const redirectTo = searchParams.get('redirect') || '/'

    async function handleSubmit(e: FormEvent) {
        e.preventDefault()
        setError('')
        setIsSubmitting(true)

        try {
            const result = await login(username, password)
            navigate(result.isAdmin ? '/admin' : redirectTo, {replace: true})
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
