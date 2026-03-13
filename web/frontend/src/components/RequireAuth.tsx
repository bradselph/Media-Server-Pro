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

    if (!isAuthenticated) {
        const redirect = location.pathname + location.search + location.hash
        return <Navigate to={`/login?redirect=${encodeURIComponent(redirect)}`} replace/>
    }

    if (adminOnly && !isAdmin) {
        return <Navigate to="/login" replace/>
    }

    return <>{children}</>
}
