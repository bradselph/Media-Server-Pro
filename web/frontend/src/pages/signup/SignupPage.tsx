import {type FormEvent, useState} from 'react'
import {Link, useNavigate} from 'react-router-dom'
import {authApi} from '@/api/endpoints'
import {ApiError} from '@/api/client'
import {useAuthStore} from '@/stores/authStore'
import '@/styles/auth.css'

export function SignupPage() {
    const navigate = useNavigate()
    const checkSession = useAuthStore((s) => s.checkSession)

    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [confirmPassword, setConfirmPassword] = useState('')
    const [email, setEmail] = useState('')
    const [error, setError] = useState('')
    const [isSubmitting, setIsSubmitting] = useState(false)

    async function handleSubmit(e: FormEvent) {
        e.preventDefault()
        setError('')

        if (password !== confirmPassword) {
            setError('Passwords do not match')
            return
        }

        if (password.length < 8) {
            setError('Password must be at least 8 characters')
            return
        }

        setIsSubmitting(true)

        try {
            // Backend creates user + session cookie; fetch full user data via checkSession
            // for consistent initialisation (same flow as login)
            await authApi.register(username, password, email || undefined)
            await checkSession()
            navigate('/', {replace: true})
        } catch (err: unknown) {
            if (err instanceof ApiError) {
                setError(err.message)
            } else {
                setError('Registration failed. Please try again.')
            }
        } finally {
            setIsSubmitting(false)
        }
    }

    return (
        <div className="auth-page">
            <div className="auth-card">
                <h1>Create Account</h1>
                <p className="auth-subtitle">Register for a new media server account</p>

                {error && <div className="auth-error">{error}</div>}

                <form onSubmit={handleSubmit}>
                    <div className="form-group">
                        <label htmlFor="username">Username</label>
                        <input
                            id="username"
                            type="text"
                            value={username}
                            onChange={(e) => { setUsername(e.target.value); }}
                            placeholder="Choose a username"
                            required
                            autoComplete="username"
                            autoFocus
                        />
                    </div>

                    <div className="form-group">
                        <label htmlFor="email">Email (optional)</label>
                        <input
                            id="email"
                            type="email"
                            value={email}
                            onChange={(e) => setEmail(e.target.value)}
                            placeholder="Enter your email"
                            autoComplete="email"
                        />
                    </div>

                    <div className="form-group">
                        <label htmlFor="password">Password</label>
                        <input
                            id="password"
                            type="password"
                            value={password}
                            onChange={(e) => { setPassword(e.target.value); }}
                            placeholder="Choose a password (min 8 characters)"
                            required
                            minLength={8}
                            autoComplete="new-password"
                        />
                    </div>

                    <div className="form-group">
                        <label htmlFor="confirm-password">Confirm Password</label>
                        <input
                            id="confirm-password"
                            type="password"
                            value={confirmPassword}
                            onChange={(e) => { setConfirmPassword(e.target.value); }}
                            placeholder="Confirm your password"
                            required
                            autoComplete="new-password"
                        />
                    </div>

                    <button type="submit" className="auth-button" disabled={isSubmitting}>
                        {isSubmitting ? 'Creating account...' : 'Create Account'}
                    </button>
                </form>

                <div className="auth-footer">
                    Already have an account? <Link to="/login">Sign in</Link>
                </div>
            </div>
        </div>
    )
}
