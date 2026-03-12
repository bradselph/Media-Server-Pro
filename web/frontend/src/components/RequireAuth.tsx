import {Navigate, useLocation} from 'react-router-dom'
import {useAuthStore} from '@/stores/authStore'

interface Props {
    children: React.ReactNode
    adminOnly?: boolean
}

export function RequireAuth({children, adminOnly}: Props) {
    const {isAuthenticated, isAdmin, isLoading} = useAuthStore()
    const location = useLocation()

    if (isLoading) {
        return <div className="loading-screen">Loading...</div>
    }

    // TODO: Query string and hash are lost — only `location.pathname` is encoded in the
    // redirect parameter. If the user was at e.g. `/player?id=abc123`, the redirect will
    // be `/login?redirect=%2Fplayer` and the `?id=abc123` query string is dropped.
    // WHY: After login, the user is redirected back to `/player` without the media ID,
    // resulting in a "no media selected" empty state instead of resuming their content.
    // FIX: Include `location.search` (and optionally `location.hash`) in the redirect:
    //   `/login?redirect=${encodeURIComponent(location.pathname + location.search + location.hash)}`
    if (!isAuthenticated) {
        return <Navigate to={`/login?redirect=${encodeURIComponent(location.pathname)}`} replace/>
    }

    if (adminOnly && !isAdmin) {
        return <Navigate to="/login" replace/>
    }

    return <>{children}</>
}
