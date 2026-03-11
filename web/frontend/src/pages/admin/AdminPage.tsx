import {useEffect, useMemo, useState} from 'react'
import {Link, useLocation, useNavigate} from 'react-router-dom'
import {useAuthStore} from '@/stores/authStore'
import {useSettingsStore} from '@/stores/settingsStore'
import {SectionErrorBoundary} from '@/components/ErrorBoundary'
import {DashboardTab} from './DashboardTab'
import {UsersTab} from './UsersTab'
import {MediaTab} from './MediaTab'
import {AnalyticsTab} from './AnalyticsTab'
import {SourcesTab} from './SourcesTab'
import {SystemTab} from './SystemTab'
import {PlaylistsTab} from './PlaylistsTab'
import '@/styles/admin.css'

type Tab =
    | 'dashboard'
    | 'users'
    | 'media'
    | 'analytics'
    | 'sources'
    | 'playlists'
    | 'system'

const VALID_TABS: Tab[] = ['dashboard', 'users', 'media', 'analytics', 'sources', 'playlists', 'system']

export function AdminPage() {
    const navigate = useNavigate()
    const location = useLocation()
    const logout = useAuthStore((s) => s.logout)
    const isAdmin = useAuthStore((s) => s.isAdmin)
    const isLoading = useAuthStore((s) => s.isLoading)
    const features = useSettingsStore((s) => s.serverSettings?.features)
    const initialTab = (location.state as { tab?: string } | null)?.tab
    const [activeTab, setActiveTab] = useState<Tab>(
        VALID_TABS.includes(initialTab as Tab) ? (initialTab as Tab) : 'dashboard'
    )

    const tabs: Array<{ id: Tab; label: string; icon: string }> = useMemo(() => [
        {id: 'dashboard' as Tab, label: 'Dashboard', icon: 'bi-speedometer2'},
        {id: 'users' as Tab, label: 'Users', icon: 'bi-people-fill'},
        {id: 'media' as Tab, label: 'Media', icon: 'bi-folder-fill'},
        ...(features?.enableAnalytics !== false ? [{id: 'analytics' as Tab, label: 'Analytics', icon: 'bi-bar-chart-fill'}] : []),
        {id: 'sources' as Tab, label: 'Sources', icon: 'bi-cloud-arrow-down-fill'},
        ...(features?.enablePlaylists !== false ? [{id: 'playlists' as Tab, label: 'Playlists', icon: 'bi-collection-fill'}] : []),
        {id: 'system' as Tab, label: 'System', icon: 'bi-gear-fill'},
    ], [features?.enableAnalytics, features?.enablePlaylists])

    useEffect(() => {
        if (!tabs.some(t => t.id === activeTab)) {
            queueMicrotask(() => { setActiveTab('dashboard'); })
        }
    }, [features, activeTab, tabs])

    if (!isLoading && !isAdmin) {
        navigate('/login', {replace: true})
        return null
    }

    async function handleLogout() {
        await logout()
        navigate('/login', {replace: true})
    }

    return (
        <div className="admin-page">
            <div className="admin-header-bar">
                <h1><i className="bi bi-shield-fill"/> Admin Panel</h1>
                <div className="admin-header-actions">
                    <Link to="/" className="admin-nav-btn"><i className="bi bi-house-fill"/> Home</Link>
                    <button className="admin-nav-btn" onClick={handleLogout}><i
                        className="bi bi-box-arrow-right"/> Logout
                    </button>
                </div>
            </div>

            <div className="admin-tab-nav">
                {tabs.map(tab => (
                    <button
                        key={tab.id}
                        className={`admin-tab-btn ${activeTab === tab.id ? 'active' : ''}`}
                        onClick={() => { setActiveTab(tab.id); }}
                    >
                        <i className={`bi ${tab.icon}`}/> {tab.label}
                    </button>
                ))}
            </div>

            <div className="admin-content">
                <SectionErrorBoundary title="Admin panel section unavailable">
                    {activeTab === 'dashboard' && <DashboardTab/>}
                    {activeTab === 'users' && <UsersTab/>}
                    {activeTab === 'media' && <MediaTab/>}
                    {activeTab === 'analytics' && <AnalyticsTab/>}
                    {activeTab === 'sources' && <SourcesTab/>}
                    {activeTab === 'playlists' && <PlaylistsTab/>}
                    {activeTab === 'system' && <SystemTab/>}
                </SectionErrorBoundary>
            </div>
        </div>
    )
}
