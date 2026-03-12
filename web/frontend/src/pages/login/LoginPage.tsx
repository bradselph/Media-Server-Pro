import {type FormEvent, useState} from 'react'
import {Link, useNavigate, useSearchParams} from 'react-router-dom'
import {useAuthStore} from '@/stores/authStore'
import {ApiError} from '@/api/client'
import '@/styles/auth.css'

type LoginResult = { isAdmin: boolean }

function getPlayerRedirectPath(redirectTo: string): string {
    const state = useAuthStore.getState()
    const canView = state.permissions.can_view_mature
    const showMature = state.user?.preferences?.show_mature
    if (canView && showMature) return redirectTo
    if (canView) return `/profile?mature_redirect=${encodeURIComponent(redirectTo)}`
    return '/'
}

function resolvePostLoginPath(result: LoginResult, redirectTo: string): string {
    if (result.isAdmin) return '/admin'
    if (redirectTo.startsWith('/player')) return getPlayerRedirectPath(redirectTo)
    return redirectTo
}

function navigateAfterLogin(
    navigate: (path: string, opts?: { replace?: boolean }) => void,
    result: LoginResult,
    redirectTo: string
): void {
    navigate(resolvePostLoginPath(result, redirectTo), {replace: true})
}

function parseRedirect(searchParams: URLSearchParams): string {
    const raw = searchParams.get('redirect') || '/'
    const safe = raw.startsWith('/') && !raw.startsWith('//')
    return safe ? raw : '/'
}

function getLoginErrorMessage(err: unknown): string {
    return err instanceof ApiError ? err.message : 'Login failed. Please try again.'
}

type LoginFormProps = {
    login: (username: string, password: string) => Promise<LoginResult>
    redirectTo: string
    navigate: (path: string, opts?: { replace?: boolean }) => void
}

function LoginForm({login, redirectTo, navigate}: LoginFormProps) {
    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [error, setError] = useState('')
    const [isSubmitting, setIsSubmitting] = useState(false)

    async function handleSubmit(e: FormEvent) {
        e.preventDefault()
        setError('')
        setIsSubmitting(true)
        try {
            const result = await login(username, password)
            navigateAfterLogin(navigate, result, redirectTo)
        } catch (err: unknown) {
            setError(getLoginErrorMessage(err))
        } finally {
            setIsSubmitting(false)
        }
    }

    return (
        <>
            {error ? <div className="auth-error">{error}</div> : null}
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
        </>
    )
}

function GuestBrowseButton({redirectTo, navigate}: { redirectTo: string; navigate: (p: string) => void }) {
    return (
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
    )
}

export function LoginPage() {
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const redirectTo = parseRedirect(searchParams)
    const login = useAuthStore((s) => s.login)
    const allowGuests = useAuthStore((s) => s.allowGuests)

    return (
        <div className="auth-page">
            <div className="auth-card">
                <h1>Sign In</h1>
                <p className="auth-subtitle">Sign in to your media server account</p>
                <LoginForm login={login} redirectTo={redirectTo} navigate={navigate} />
                <div className="auth-footer">
                    Don't have an account? <Link to="/signup">Sign up</Link>
                </div>
                {allowGuests ? <GuestBrowseButton redirectTo={redirectTo} navigate={navigate} /> : null}
            </div>
        </div>
    )
}
