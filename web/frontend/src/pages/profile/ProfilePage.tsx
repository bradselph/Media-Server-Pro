import {type FormEvent, useEffect, useMemo, useState} from 'react'
import {Link, useNavigate, useSearchParams} from 'react-router-dom'
import {useAuthStore} from '@/stores/authStore'
import {useThemeStore} from '@/stores/themeStore'
import {useQueryClient} from '@tanstack/react-query'
import {authApi, permissionsApi as permApi, preferencesApi, storageApi, watchHistoryApi} from '@/api/endpoints'
import {ApiError} from '@/api/client'
import {useToast} from '@/components/Toast'
import type {PermissionsInfo, StorageUsage, UserPreferences, WatchHistoryEntry} from '@/api/types'
import '@/styles/profile.css'

function formatDate(timestamp: string | undefined): string {
    if (!timestamp) return 'N/A'
    const date = new Date(timestamp)
    return date.toLocaleDateString(undefined, {year: 'numeric', month: 'long', day: 'numeric'})
}

function formatDuration(seconds: number): string {
    if (!seconds || seconds <= 0) return '0m'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
}

function cleanFileName(name: string): string {
    const base = name.split('/').pop()?.split('\\').pop() || name
    return base.replace(/\.[^.]+$/, '').replace(/[_-]/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}

function displayMediaName(entry: { media_name?: string; media_id: string }): string {
    if (entry.media_name) return cleanFileName(entry.media_name)
    // TODO: This fallback displays a truncated UUID (or pre-v0.32.0 MD5 path hash) as a label,
    // which is not human-readable. WatchHistoryItem.MediaName (media_name) is populated by
    // api/handlers/auth.go GetWatchHistory only for items present in the current media catalog.
    // Legacy watch history entries recorded before the UUID migration (pre-v0.32.0) stored a
    // path-derived MD5 as media_id and had no media_name. There is no migration path to backfill
    // media_name for these entries. Options:
    //   (a) Add a backend migration task that looks up MediaName by MediaID and updates stored
    //       WatchHistoryItem rows in the users table (GORM JSON column).
    //   (b) Show a generic "Unknown title" label instead of a truncated hash.
    //   (c) Filter out entries where media_name is absent from the displayed history.
    // Fallback: truncated MD5 hash for legacy entries without media_name
    return entry.media_id.slice(0, 8) + '…'
}

export function ProfilePage() {
    const {user, checkSession} = useAuthStore()
    const {theme, setTheme} = useThemeStore()
    const {showToast} = useToast()
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const queryClient = useQueryClient()

    // Mature content redirect: validate to prevent open redirect attacks
    const rawMatureRedirect = searchParams.get('mature_redirect') || ''
    const matureRedirect = rawMatureRedirect.startsWith('/') && !rawMatureRedirect.startsWith('//') ? rawMatureRedirect : ''

    const [preferences, setPreferences] = useState<UserPreferences | null>(null)
    const [prefsError, setPrefsError] = useState(false)
    const [watchHistory, setWatchHistory] = useState<WatchHistoryEntry[]>([])
    const [watchHistoryError, setWatchHistoryError] = useState(false)
    const [prefsLoading, setPrefsLoading] = useState(true)
    const [storageUsage, setStorageUsage] = useState<StorageUsage | null>(null)
    const [permissions, setPermissions] = useState<PermissionsInfo | null>(null)

    // Password form state
    const [currentPassword, setCurrentPassword] = useState('')
    const [newPassword, setNewPassword] = useState('')
    const [confirmPassword, setConfirmPassword] = useState('')
    const [passwordError, setPasswordError] = useState('')
    const [passwordSubmitting, setPasswordSubmitting] = useState(false)

    // Preferences form state
    const [prefsSubmitting, setPrefsSubmitting] = useState(false)

    // Watch history sort/filter state
    const [historySortBy, setHistorySortBy] = useState<'watched_at' | 'name' | 'duration' | 'progress'>('watched_at')
    const [historySortDesc, setHistorySortDesc] = useState(true)
    const [historySearch, setHistorySearch] = useState('')

    // Delete account form state
    const [deletePassword, setDeletePassword] = useState('')
    const [deleteError, setDeleteError] = useState('')
    const [deleteSubmitting, setDeleteSubmitting] = useState(false)
    const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)

    useEffect(() => {
        loadPreferences()
        loadWatchHistory()
        loadStorageAndPermissions()
    }, [])

    async function loadStorageAndPermissions() {
        try {
            const [storage, perms] = await Promise.all([storageApi.getUsage(), permApi.get()])
            setStorageUsage(storage)
            setPermissions(perms)
        } catch {
            // non-critical — silently ignore
        }
    }

    async function loadPreferences() {
        try {
            const prefs = await preferencesApi.get()
            setPreferences(prefs)
            if (prefs.theme && prefs.theme !== 'auto') {
                setTheme(prefs.theme as 'light' | 'dark')
            }
        } catch (err) {
            // 404 means no preferences saved yet (new user) — not an error
            if (!(err instanceof ApiError && err.status === 404)) {
                setPrefsError(true)
            }
        } finally {
            setPrefsLoading(false)
        }
    }

    async function loadWatchHistory() {
        try {
            const history = await watchHistoryApi.list()
            setWatchHistory(Array.isArray(history) ? history : [])
        } catch {
            setWatchHistoryError(true)
        }
    }

    async function handlePasswordSubmit(e: FormEvent) {
        e.preventDefault()
        setPasswordError('')

        if (newPassword.length < 8) {
            setPasswordError('Password must be at least 8 characters')
            return
        }
        if (newPassword !== confirmPassword) {
            setPasswordError('Passwords do not match')
            return
        }

        setPasswordSubmitting(true)
        try {
            await authApi.changePassword(currentPassword, newPassword)
            showToast('Password changed successfully', 'success')
            setCurrentPassword('')
            setNewPassword('')
            setConfirmPassword('')
        } catch (err: unknown) {
            if (err instanceof ApiError) {
                setPasswordError(err.message)
            } else {
                setPasswordError('Failed to change password')
            }
        } finally {
            setPasswordSubmitting(false)
        }
    }

    async function handlePreferencesSubmit(e: FormEvent) {
        e.preventDefault()
        if (!preferences) return

        setPrefsSubmitting(true)
        try {
            await preferencesApi.update(preferences)
            if (preferences.theme && preferences.theme !== 'auto') {
                setTheme(preferences.theme as 'light' | 'dark')
            }
            // Sync equalizer band count to localStorage so useEqualizer picks it up on next player load.
            if (preferences.equalizer_preset === '10' || preferences.equalizer_preset === '31') {
                try {
                    const raw = localStorage.getItem('media_streamer_settings')
                    const stored: Record<string, unknown> = raw ? JSON.parse(raw) : {}
                    localStorage.setItem('media_streamer_settings', JSON.stringify({
                        ...stored,
                        eqBands: preferences.equalizer_preset
                    }))
                } catch { /* storage full or unavailable */
                }
            }
            await checkSession()
            // Invalidate media cache so index page reflects mature filter changes
            await queryClient.invalidateQueries({queryKey: ['media']})
            showToast('Preferences saved', 'success')
            // Mature redirect flow: if user enabled show_mature, go to the media
            if (matureRedirect) {
                if (preferences.show_mature) {
                    navigate(matureRedirect, {replace: true})
                } else {
                    navigate('/', {replace: true})
                }
                return
            }
        } catch {
            showToast('Failed to save preferences', 'error')
        } finally {
            setPrefsSubmitting(false)
        }
    }

    function updatePref<K extends keyof UserPreferences>(key: K, value: UserPreferences[K]) {
        setPreferences(prev => prev ? {...prev, [key]: value} : null)
    }

    async function handleDeleteHistoryItem(mediaId: string) {
        try {
            await watchHistoryApi.delete(mediaId)
            setWatchHistory(prev => prev.filter(e => e.media_id !== mediaId))
        } catch {
            showToast('Failed to remove history entry', 'error')
        }
    }

    async function handleDeleteAccount(e: React.FormEvent) {
        e.preventDefault()
        setDeleteError('')
        setDeleteSubmitting(true)
        try {
            await authApi.deleteAccount(deletePassword)
            showToast('Account deleted', 'success')
            // Server will have logged us out; redirect to home
            window.location.href = '/'
        } catch (err: unknown) {
            if (err instanceof ApiError) {
                setDeleteError(err.message)
            } else {
                setDeleteError('Failed to delete account')
            }
        } finally {
            setDeleteSubmitting(false)
        }
    }

    const sortedHistory = useMemo(() => {
        let items = [...watchHistory]
        if (historySearch) {
            const q = historySearch.toLowerCase()
            items = items.filter(e => displayMediaName(e).toLowerCase().includes(q))
        }
        items.sort((a, b) => {
            let cmp = 0
            switch (historySortBy) {
                case 'watched_at':
                    cmp = new Date(a.watched_at).getTime() - new Date(b.watched_at).getTime()
                    break
                case 'name':
                    cmp = displayMediaName(a).localeCompare(displayMediaName(b))
                    break
                case 'duration':
                    cmp = (a.duration || 0) - (b.duration || 0)
                    break
                case 'progress':
                    cmp = (a.progress || 0) - (b.progress || 0)
                    break
            }
            return historySortDesc ? -cmp : cmp
        })
        return items
    }, [watchHistory, historySortBy, historySortDesc, historySearch])

    if (!user) {
        return <div className="loading-screen">Loading...</div>
    }

    return (
        <div className="profile-page">
            <div className="profile-header">
                <div>
                    <h1>User Profile</h1>
                    <p className="profile-subtitle">Manage your account settings and preferences</p>
                </div>
                <Link to="/" className="back-link">Back to Library</Link>
            </div>

            {matureRedirect && (
                <div style={{
                    background: 'var(--bg-secondary)',
                    border: '1px solid var(--accent-color, #667eea)',
                    borderRadius: 8,
                    padding: '12px 16px',
                    marginBottom: 16,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 12,
                    flexWrap: 'wrap',
                }}>
                    <span style={{fontSize: 14}}>
                        <i className="bi bi-shield-lock-fill" style={{marginRight: 6, color: 'var(--accent-color, #667eea)'}}/>{' '}
                        Enable mature content below to view the requested media.
                    </span>
                    <button
                        className="btn btn-sm"
                        onClick={() => navigate('/', {replace: true})}
                        style={{whiteSpace: 'nowrap'}}
                    >
                        Skip
                    </button>
                </div>
            )}

            <div className="profile-grid">
                {/* Account Info Card */}
                <div className="profile-card">
                    <h2>Account Information</h2>
                    <div className="info-grid">
                        <div className="info-item">
                            <span className="info-label">Username</span>
                            <span className="info-value">{user.username}</span>
                        </div>
                        <div className="info-item">
                            <span className="info-label">Account Type</span>
                            <span className="info-value role-badge">{user.role}</span>
                        </div>
                        <div className="info-item">
                            <span className="info-label">Member Since</span>
                            <span className="info-value">{formatDate(user.created_at)}</span>
                        </div>
                        <div className="info-item">
                            <span className="info-label">Last Login</span>
                            <span className="info-value">{formatDate(user.last_login)}</span>
                        </div>
                    </div>
                </div>

                {/* Storage Usage Card */}
                {storageUsage && (
                    <div className="profile-card">
                        <h2>Storage Usage</h2>
                        <div className="info-grid">
                            <div className="info-item">
                                <span className="info-label">Used</span>
                                <span className="info-value">{storageUsage.used_gb.toFixed(2)} GB</span>
                            </div>
                            <div className="info-item">
                                <span className="info-label">Quota</span>
                                <span className="info-value">
                                    {storageUsage.quota_gb > 0 ? `${storageUsage.quota_gb.toFixed(1)} GB` : 'Unlimited'}
                                </span>
                            </div>
                        </div>
                        <div style={{marginTop: 12}}>
                            <div style={{
                                display: 'flex',
                                justifyContent: 'space-between',
                                marginBottom: 4,
                                fontSize: 13
                            }}>
                                <span style={{color: 'var(--text-muted)'}}>
                                    {storageUsage.used_bytes.toLocaleString()} bytes used
                                </span>
                                <span style={{color: 'var(--text-color)'}}>{storageUsage.percentage.toFixed(1)}%</span>
                            </div>
                            <div style={{
                                background: 'var(--border-color)',
                                borderRadius: 4,
                                height: 8,
                                overflow: 'hidden'
                            }}>
                                <div style={{
                                    width: `${Math.min(storageUsage.percentage, 100)}%`,
                                    height: '100%',
                                    background: storageUsage.percentage > 90 ? '#ef4444' : storageUsage.percentage > 70 ? '#f59e0b' : 'var(--accent-color, #667eea)',
                                    borderRadius: 4,
                                    transition: 'width 0.3s ease',
                                }}/>
                            </div>
                        </div>
                    </div>
                )}

                {/* Permissions Card */}
                {permissions && (
                    <div className="profile-card">
                        <h2>My Permissions</h2>
                        <div className="info-grid">
                            {([
                                {label: 'Stream', value: permissions.capabilities.canStream},
                                {label: 'Download', value: permissions.capabilities.canDownload},
                                {label: 'Upload', value: permissions.capabilities.canUpload},
                                {label: 'Create Playlists', value: permissions.capabilities.canCreatePlaylists},
                                {label: 'View Mature', value: permissions.capabilities.canViewMature},
                                ...(permissions.capabilities.canDelete !== undefined ? [{
                                    label: 'Delete',
                                    value: permissions.capabilities.canDelete
                                }] : []),
                                ...(permissions.capabilities.canManage !== undefined ? [{
                                    label: 'Manage',
                                    value: permissions.capabilities.canManage
                                }] : []),
                            ] as Array<{ label: string; value: boolean | undefined }>).map(({label, value}) => (
                                <div key={label} className="info-item">
                                    <span className="info-label">{label}</span>
                                    <span className="info-value" style={{color: value ? '#10b981' : '#ef4444'}}>
                                        <i className={`bi bi-${value ? 'check-circle-fill' : 'x-circle-fill'}`}/> {value ? 'Yes' : 'No'}
                                    </span>
                                </div>
                            ))}
                        </div>
                        {permissions.limits && (
                            <div className="info-grid"
                                 style={{marginTop: 8, paddingTop: 8, borderTop: '1px solid var(--border-color)'}}>
                                <div className="info-item">
                                    <span className="info-label">Storage Quota</span>
                                    <span className="info-value">
                                        {permissions.limits.storage_quota > 0
                                            ? `${(permissions.limits.storage_quota / 1073741824).toFixed(0)} GB`
                                            : 'Unlimited'}
                                    </span>
                                </div>
                                <div className="info-item">
                                    <span className="info-label">Concurrent Streams</span>
                                    <span className="info-value">{permissions.limits.concurrent_streams}</span>
                                </div>
                            </div>
                        )}
                    </div>
                )}

                {/* Change Password Card */}
                <div className="profile-card">
                    <h2>Change Password</h2>
                    {passwordError && <div className="form-error">{passwordError}</div>}
                    <form onSubmit={handlePasswordSubmit}>
                        <div className="form-group">
                            <label htmlFor="current-password">Current Password</label>
                            <input
                                id="current-password"
                                type="password"
                                value={currentPassword}
                                onChange={e => setCurrentPassword(e.target.value)}
                                required
                                autoComplete="current-password"
                            />
                        </div>
                        <div className="form-group">
                            <label htmlFor="new-password">New Password</label>
                            <input
                                id="new-password"
                                type="password"
                                value={newPassword}
                                onChange={e => setNewPassword(e.target.value)}
                                required
                                minLength={8}
                                autoComplete="new-password"
                            />
                            <span className="form-hint">Must be at least 8 characters</span>
                        </div>
                        <div className="form-group">
                            <label htmlFor="confirm-password">Confirm New Password</label>
                            <input
                                id="confirm-password"
                                type="password"
                                value={confirmPassword}
                                onChange={e => setConfirmPassword(e.target.value)}
                                required
                                autoComplete="new-password"
                            />
                        </div>
                        <button type="submit" className="btn btn-primary" disabled={passwordSubmitting}>
                            {passwordSubmitting ? 'Changing...' : 'Change Password'}
                        </button>
                    </form>
                </div>

                {/* Preferences Card */}
                <div className="profile-card">
                    <h2>Preferences</h2>
                    {prefsLoading ? (
                        <p>Loading preferences...</p>
                    ) : prefsError ? (
                        <p style={{color: 'var(--error-color, #dc3545)'}}>Failed to load preferences. Please refresh the
                            page.</p>
                    ) : (
                        <form onSubmit={handlePreferencesSubmit}>
                            <div className="form-group">
                                <label htmlFor="default-quality">Default Video Quality</label>
                                <select
                                    id="default-quality"
                                    value={preferences?.default_quality || 'auto'}
                                    onChange={e => updatePref('default_quality', e.target.value)}
                                >
                                    <option value="auto">Auto</option>
                                    <option value="low">Low (360p)</option>
                                    <option value="medium">Medium (480p)</option>
                                    <option value="high">High (720p)</option>
                                    <option value="ultra">Ultra (1080p)</option>
                                </select>
                            </div>

                            <div className="form-group">
                                <label htmlFor="theme-preference">Theme</label>
                                <select
                                    id="theme-preference"
                                    value={preferences?.theme || theme}
                                    onChange={e => updatePref('theme', e.target.value as UserPreferences['theme'])}
                                >
                                    <option value="auto">Auto (System)</option>
                                    <option value="light">Light</option>
                                    <option value="dark">Dark</option>
                                </select>
                            </div>

                            <div className="form-group">
                                <label htmlFor="eq-bands">Equalizer Bands</label>
                                <select
                                    id="eq-bands"
                                    value={preferences?.equalizer_preset || '10'}
                                    onChange={e => updatePref('equalizer_preset', e.target.value)}
                                >
                                    <option value="10">10-Band (Standard)</option>
                                    <option value="31">31-Band (Professional)</option>
                                </select>
                            </div>

                            <div className="form-group">
                                <label htmlFor="items-per-page">Items Per Page</label>
                                <select
                                    id="items-per-page"
                                    value={preferences?.items_per_page ?? 24}
                                    onChange={e => updatePref('items_per_page', Number(e.target.value))}
                                >
                                    <option value={12}>12</option>
                                    <option value={24}>24 (default)</option>
                                    <option value={48}>48</option>
                                    <option value={96}>96</option>
                                </select>
                            </div>

                            <div className="form-group">
                                <label htmlFor="playback-speed">Default Playback Speed</label>
                                <select
                                    id="playback-speed"
                                    value={preferences?.playback_speed ?? 1}
                                    onChange={e => updatePref('playback_speed', Number(e.target.value))}
                                >
                                    <option value={0.5}>0.5×</option>
                                    <option value={0.75}>0.75×</option>
                                    <option value={1}>1× (normal)</option>
                                    <option value={1.25}>1.25×</option>
                                    <option value={1.5}>1.5×</option>
                                    <option value={2}>2×</option>
                                </select>
                            </div>

                            <div className="checkbox-group">
                                <label className="checkbox-label">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.auto_play ?? false}
                                        onChange={e => updatePref('auto_play', e.target.checked)}
                                    />
                                    Autoplay next track
                                </label>
                                <label className="checkbox-label">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.resume_playback ?? true}
                                        onChange={e => updatePref('resume_playback', e.target.checked)}
                                    />
                                    Resume playback position
                                </label>
                                <label className="checkbox-label">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.show_analytics ?? false}
                                        onChange={e => updatePref('show_analytics', e.target.checked)}
                                    />
                                    Show analytics bar
                                </label>
                            </div>

                            <div className="content-settings">
                                <h3>Content Settings</h3>
                                <label className="checkbox-label mature-checkbox">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.show_mature ?? false}
                                        onChange={e => updatePref('show_mature', e.target.checked)}
                                    />
                                    Allow mature content (18+)
                                </label>
                            </div>

                            <div className="content-settings">
                                <h3>Home Page Sections</h3>
                                <p style={{fontSize: 13, color: 'var(--text-muted)', marginBottom: 8}}>
                                    Choose which sections appear on your home page.
                                </p>
                                <label className="checkbox-label">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.show_continue_watching ?? true}
                                        onChange={e => updatePref('show_continue_watching', e.target.checked)}
                                    />
                                    Continue Watching
                                </label>
                                <label className="checkbox-label">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.show_recommended ?? true}
                                        onChange={e => updatePref('show_recommended', e.target.checked)}
                                    />
                                    Recommended For You
                                </label>
                                <label className="checkbox-label">
                                    <input
                                        type="checkbox"
                                        checked={preferences?.show_trending ?? true}
                                        onChange={e => updatePref('show_trending', e.target.checked)}
                                    />
                                    Trending
                                </label>
                            </div>

                            <button type="submit" className="btn btn-primary" disabled={prefsSubmitting}>
                                {prefsSubmitting ? 'Saving...' : 'Save Preferences'}
                            </button>
                        </form>
                    )}
                </div>

                {/* Watch History Card */}
                <div className="profile-card">
                    <h2>Watch History</h2>
                    {watchHistoryError ? (
                        <p className="empty-state" style={{color: 'var(--error-color, #dc3545)'}}>Failed to load watch
                            history</p>
                    ) : watchHistory.length === 0 ? (
                        <p className="empty-state">No watch history yet</p>
                    ) : (
                        <>
                            <div style={{display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center'}}>
                                <input
                                    type="text"
                                    placeholder="Search history..."
                                    value={historySearch}
                                    onChange={e => setHistorySearch(e.target.value)}
                                    style={{flex: '1 1 160px', minWidth: 120, padding: '6px 10px', borderRadius: 6, border: '1px solid var(--border-color)', background: 'var(--bg-secondary)', color: 'var(--text-color)', fontSize: 13}}
                                />
                                <select
                                    value={historySortBy}
                                    onChange={e => setHistorySortBy(e.target.value as typeof historySortBy)}
                                    style={{padding: '6px 10px', borderRadius: 6, border: '1px solid var(--border-color)', background: 'var(--bg-secondary)', color: 'var(--text-color)', fontSize: 13}}
                                >
                                    <option value="watched_at">Date Watched</option>
                                    <option value="name">Title</option>
                                    <option value="duration">Duration</option>
                                    <option value="progress">Progress</option>
                                </select>
                                <button
                                    className="btn btn-sm"
                                    onClick={() => setHistorySortDesc(d => !d)}
                                    title={historySortDesc ? 'Descending' : 'Ascending'}
                                    style={{padding: '6px 10px', fontSize: 13, minWidth: 32}}
                                >
                                    {historySortDesc ? '\u25BC' : '\u25B2'}
                                </button>
                            </div>
                            {sortedHistory.length === 0 ? (
                                <p className="empty-state">No matching history entries</p>
                            ) : (
                                <div className="history-list">
                                    {sortedHistory.map((entry, i) => (
                                        <div key={`${entry.media_id}-${i}`} className="history-item">
                                            <div className="history-info">
                                                <Link
                                                    to={`/player?id=${encodeURIComponent(entry.media_id)}`}
                                                    className="history-title"
                                                >
                                                    {displayMediaName(entry)}
                                                </Link>
                                                <span className="history-meta">
                                                    {formatDuration(entry.duration)} &middot; {Math.round(entry.progress * 100)}% watched
                                                </span>
                                            </div>
                                            <span className="history-date">{formatDate(entry.watched_at)}</span>
                                            <button
                                                className="btn btn-sm btn-danger"
                                                onClick={() => handleDeleteHistoryItem(entry.media_id)}
                                                title="Remove from history"
                                            >
                                                <i className="bi bi-x"/>
                                            </button>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </>
                    )}
                </div>

                {/* Delete Account Card */}
                {user.role !== 'admin' && (
                    <div className="profile-card profile-card-danger">
                        <h2>Danger Zone</h2>
                        {!showDeleteConfirm ? (
                            <div>
                                <p className="form-hint">Permanently delete your account and all associated data.</p>
                                <button className="btn btn-danger" onClick={() => setShowDeleteConfirm(true)}>
                                    Delete Account
                                </button>
                            </div>
                        ) : (
                            <form onSubmit={handleDeleteAccount}>
                                <p className="form-hint">Enter your password to confirm account deletion. This cannot be
                                    undone.</p>
                                {deleteError && <div className="form-error">{deleteError}</div>}
                                <div className="form-group">
                                    <label htmlFor="delete-password">Confirm Password</label>
                                    <input
                                        id="delete-password"
                                        type="password"
                                        value={deletePassword}
                                        onChange={e => setDeletePassword(e.target.value)}
                                        required
                                        autoComplete="current-password"
                                    />
                                </div>
                                <div style={{display: 'flex', gap: 8}}>
                                    <button type="submit" className="btn btn-danger" disabled={deleteSubmitting}>
                                        {deleteSubmitting ? 'Deleting...' : 'Confirm Delete'}
                                    </button>
                                    <button type="button" className="btn" onClick={() => {
                                        setShowDeleteConfirm(false);
                                        setDeletePassword('');
                                        setDeleteError('')
                                    }}>
                                        Cancel
                                    </button>
                                </div>
                            </form>
                        )}
                    </div>
                )}
            </div>
        </div>
    )
}
