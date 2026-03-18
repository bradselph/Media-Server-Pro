import {lazy, Suspense, useEffect} from 'react'
import {BrowserRouter, Route, Routes} from 'react-router-dom'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'

import {useAuthStore} from '@/stores/authStore'
import {useSettingsStore} from '@/stores/settingsStore'
// Ensure theme is applied on first paint for all routes (player, admin, etc. don't import themeStore)
import '@/stores/themeStore'
import {AgeGateProvider} from '@/components/AgeGate'
import {RequireAuth} from '@/components/RequireAuth'
import {ToastProvider} from '@/components/Toast'
import {ErrorBoundary, PageLoader} from '@/components/ErrorBoundary'

// Route-based code splitting — each page is its own JS chunk
const LoginPage = lazy(() => import('@/pages/login/LoginPage').then(m => ({default: m.LoginPage})))
const SignupPage = lazy(() => import('@/pages/signup/SignupPage').then(m => ({default: m.SignupPage})))
const ProfilePage = lazy(() => import('@/pages/profile/ProfilePage').then(m => ({default: m.ProfilePage})))
const IndexPage = lazy(() => import('@/pages/index/IndexPage').then(m => ({default: m.IndexPage})))
const PlayerPage = lazy(() => import('@/pages/player/PlayerPage').then(m => ({default: m.PlayerPage})))
const AdminPage = lazy(() => import('@/pages/admin/AdminPage').then(m => ({default: m.AdminPage})))

const queryClient = new QueryClient({
    defaultOptions: {
        queries: {
            retry: 1,
            staleTime: 5 * 60 * 1000, // 5 minutes, matches old sessionStorage TTL
            refetchOnWindowFocus: false,
        },
    },
})

function AppInit({children}: { children: React.ReactNode }) {
    const checkSession = useAuthStore((s) => s.checkSession)
    const loadServerSettings = useSettingsStore((s) => s.loadServerSettings)
    const settingsError = useSettingsStore((s) => s.error)

    useEffect(() => {
        checkSession()
        loadServerSettings()
    }, [checkSession, loadServerSettings])

    // DC-07: log settings errors so they're visible during development and debugging
    useEffect(() => {
        if (settingsError) {
            console.warn('[settings] Failed to load server settings:', settingsError)
        }
    }, [settingsError])

    return <>{children}</>
}

export default function App() {
    return (
        <QueryClientProvider client={queryClient}>
            <ToastProvider>
                <BrowserRouter>
                    <AppInit>
                        <AgeGateProvider>
                            <ErrorBoundary>
                                <Suspense fallback={<PageLoader/>}>
                                    <Routes>
                                        {/* Public routes */}
                                        <Route path="/" element={<IndexPage/>}/>
                                        <Route path="/login" element={<LoginPage/>}/>
                                        <Route path="/admin-login" element={<LoginPage/>}/>
                                        <Route path="/signup" element={<SignupPage/>}/>
                                        <Route path="/player" element={<PlayerPage/>}/>

                                        {/* Protected routes */}
                                        <Route
                                            path="/profile"
                                            element={
                                                <RequireAuth>
                                                    <ProfilePage/>
                                                </RequireAuth>
                                            }
                                        />

                                        {/* Admin routes */}
                                        <Route
                                            path="/admin"
                                            element={
                                                <RequireAuth adminOnly>
                                                    <AdminPage/>
                                                </RequireAuth>
                                            }
                                        />
                                    </Routes>
                                </Suspense>
                            </ErrorBoundary>
                        </AgeGateProvider>
                    </AppInit>
                </BrowserRouter>
            </ToastProvider>
        </QueryClientProvider>
    )
}
