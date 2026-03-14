import {useMemo, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {BannedIP, IPEntry, SecurityStats} from '@/api/types'
import {errMsg} from './adminUtils'

// ── Tab: Security (Feature 10) ────────────────────────────────────────────────

type IPSortKey = 'ip' | 'comment' | 'added_by' | 'added_at'
type BanSortKey = 'ip' | 'reason' | 'banned_at' | 'expires_at'

const SECURITY_STATS_KEY = ['admin-security-stats'] as const
const SECURITY_WHITELIST_KEY = ['admin-security-whitelist'] as const
const SECURITY_BLACKLIST_KEY = ['admin-security-blacklist'] as const
const SECURITY_BANNED_KEY = ['admin-security-banned'] as const

const inputBaseStyle: React.CSSProperties = {
    padding: '6px 10px',
    border: '1px solid var(--border-color)',
    borderRadius: 6,
    background: 'var(--input-bg)',
    color: 'var(--text-color)',
    fontSize: 13,
}

const mutedStyle: React.CSSProperties = { fontSize: 12, color: 'var(--text-muted)' }
const thSortStyle: React.CSSProperties = { cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap' }

function sortIndicator<T extends string>(col: T, sortBy: T, sortOrder: 'asc' | 'desc') {
    if (sortBy !== col) return <span style={{ opacity: 0.3, marginLeft: 4 }}>&#x21C5;</span>
    return <span style={{ marginLeft: 4 }}>{sortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
}

export function SecurityTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

    // Whitelist state
    const [wlIp, setWlIp] = useState('')
    const [wlComment, setWlComment] = useState('')
    // Blacklist state
    const [blIp, setBlIp] = useState('')
    const [blComment, setBlComment] = useState('')
    // Ban state
    const [banIp, setBanIp] = useState('')
    const [banDuration, setBanDuration] = useState(60)

    // Sort state for IP tables
    const [wlSortBy, setWlSortBy] = useState<IPSortKey>('ip')
    const [wlSortOrder, setWlSortOrder] = useState<'asc' | 'desc'>('asc')
    const [blSortBy, setBlSortBy] = useState<IPSortKey>('ip')
    const [blSortOrder, setBlSortOrder] = useState<'asc' | 'desc'>('asc')
    const [banSortBy, setBanSortBy] = useState<BanSortKey>('banned_at')
    const [banSortOrder, setBanSortOrder] = useState<'asc' | 'desc'>('desc')
    const [ipSearch, setIpSearch] = useState('')

    const {data: secStats} = useQuery<SecurityStats>({
        queryKey: SECURITY_STATS_KEY,
        queryFn: () => adminApi.getSecurityStats(),
        refetchInterval: 30000,
    })

    const {data: whitelist = []} = useQuery<IPEntry[]>({
        queryKey: SECURITY_WHITELIST_KEY,
        queryFn: () => adminApi.getWhitelist(),
    })

    const {data: blacklist = []} = useQuery<IPEntry[]>({
        queryKey: SECURITY_BLACKLIST_KEY,
        queryFn: () => adminApi.getBlacklist(),
    })

    const {data: bannedIPs = []} = useQuery<BannedIP[]>({
        queryKey: SECURITY_BANNED_KEY,
        queryFn: () => adminApi.getBannedIPs(),
        refetchInterval: 30000,
    })

    const sortedWl = useMemo(() => whitelist
        .filter(e => !ipSearch || e.ip.includes(ipSearch) || (e.comment || '').toLowerCase().includes(ipSearch.toLowerCase()))
        .sort((a, b) => {
            let cmp = 0
            switch (wlSortBy) {
                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                case 'comment': cmp = (a.comment || '').localeCompare(b.comment || ''); break
                case 'added_by': cmp = (a.added_by || '').localeCompare(b.added_by || ''); break
                case 'added_at': cmp = a.added_at.localeCompare(b.added_at); break
            }
            return wlSortOrder === 'desc' ? -cmp : cmp
        }), [whitelist, ipSearch, wlSortBy, wlSortOrder])

    const sortedBl = useMemo(() => blacklist
        .filter(e => !ipSearch || e.ip.includes(ipSearch) || (e.comment || '').toLowerCase().includes(ipSearch.toLowerCase()))
        .sort((a, b) => {
            let cmp = 0
            switch (blSortBy) {
                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                case 'comment': cmp = (a.comment || '').localeCompare(b.comment || ''); break
                case 'added_by': cmp = (a.added_by || '').localeCompare(b.added_by || ''); break
                case 'added_at': cmp = a.added_at.localeCompare(b.added_at); break
            }
            return blSortOrder === 'desc' ? -cmp : cmp
        }), [blacklist, ipSearch, blSortBy, blSortOrder])

    const sortedBans = useMemo(() => bannedIPs
        .filter(b => !ipSearch || b.ip.includes(ipSearch) || (b.reason || '').toLowerCase().includes(ipSearch.toLowerCase()))
        .sort((a, b) => {
            let cmp = 0
            switch (banSortBy) {
                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                case 'reason': cmp = (a.reason || '').localeCompare(b.reason || ''); break
                case 'banned_at': cmp = a.banned_at.localeCompare(b.banned_at); break
                case 'expires_at': cmp = (a.expires_at || 'z').localeCompare(b.expires_at || 'z'); break
            }
            return banSortOrder === 'desc' ? -cmp : cmp
        }), [bannedIPs, ipSearch, banSortBy, banSortOrder])

    async function handleAddWhitelist(e: React.FormEvent) {
        e.preventDefault()
        if (!wlIp.trim()) return
        try {
            await adminApi.addToWhitelist(wlIp.trim(), wlComment.trim() || undefined)
            setMsg({type: 'success', text: `${wlIp} added to whitelist`})
            setWlIp('');
            setWlComment('')
            await queryClient.invalidateQueries({ queryKey: SECURITY_WHITELIST_KEY })
            await queryClient.invalidateQueries({ queryKey: SECURITY_STATS_KEY })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleAddBlacklist(e: React.FormEvent) {
        e.preventDefault()
        if (!blIp.trim()) return
        try {
            await adminApi.addToBlacklist(blIp.trim(), blComment.trim() || undefined)
            setMsg({type: 'success', text: `${blIp} added to blacklist`})
            setBlIp('');
            setBlComment('')
            await queryClient.invalidateQueries({queryKey: ['admin-security-blacklist']})
            await queryClient.invalidateQueries({ queryKey: SECURITY_STATS_KEY })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleBan(e: React.FormEvent) {
        e.preventDefault()
        if (!banIp.trim()) return
        try {
            await adminApi.banIP(banIp.trim(), banDuration)
            setMsg({type: 'success', text: `${banIp} banned for ${banDuration} minutes`})
            setBanIp('')
            await queryClient.invalidateQueries({ queryKey: SECURITY_BANNED_KEY })
            await queryClient.invalidateQueries({ queryKey: SECURITY_STATS_KEY })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
            {secStats && (
                <div className="admin-stats-grid" style={{marginBottom: 16}}>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.banned_ips}</span><span className="admin-stat-label">Banned IPs</span>
                    </div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.whitelisted_ips}</span><span
                        className="admin-stat-label">Whitelisted</span></div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.blacklisted_ips}</span><span
                        className="admin-stat-label">Blacklisted</span></div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.active_rate_limits}</span><span
                        className="admin-stat-label">Active Rate Limits</span></div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.total_blocks_today}</span><span
                        className="admin-stat-label">Blocks Today</span></div>
                </div>
            )}

            {/* IP search across all lists */}
            <div style={{marginBottom: 16}}>
                <input type="text" placeholder="Search IPs across all lists..." value={ipSearch}
                       onChange={e => { setIpSearch(e.target.value); }}
                       style={{ width: '100%', maxWidth: 300, ...inputBaseStyle }} />
            </div>

            {/* Whitelist */}
            <div className="admin-card">
                <h3>Whitelist <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({whitelist.length} IPs)</span>
                </h3>
                <form onSubmit={handleAddWhitelist} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={wlIp} onChange={e => { setWlIp(e.target.value); }} placeholder="IP address"
                           style={{flex: 1, ...inputBaseStyle}}/>
                    <input type="text" value={wlComment} onChange={e => { setWlComment(e.target.value); }}
                           placeholder="Comment (optional)" style={{flex: 1, ...inputBaseStyle}}/>
                    <button type="submit" className="admin-btn admin-btn-primary"><i className="bi bi-plus-lg"/> Add
                    </button>
                </form>
                {whitelist.length > 0 && (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thSortStyle} onClick={() => { if (wlSortBy === 'ip') setWlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setWlSortBy('ip'); setWlSortOrder('asc'); } }}>IP{sortIndicator('ip', wlSortBy, wlSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (wlSortBy === 'comment') setWlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setWlSortBy('comment'); setWlSortOrder('asc'); } }}>Comment{sortIndicator('comment', wlSortBy, wlSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (wlSortBy === 'added_by') setWlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setWlSortBy('added_by'); setWlSortOrder('asc'); } }}>Added By{sortIndicator('added_by', wlSortBy, wlSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (wlSortBy === 'added_at') setWlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setWlSortBy('added_at'); setWlSortOrder('asc'); } }}>Added{sortIndicator('added_at', wlSortBy, wlSortOrder)}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedWl.map(entry => (
                                <tr key={entry.ip}>
                                    <td><code>{entry.ip}</code></td>
                                    <td style={mutedStyle}>{entry.comment || '—'}</td>
                                    <td style={mutedStyle}>{entry.added_by || '—'}</td>
                                    <td style={mutedStyle}>{new Date(entry.added_at).toLocaleDateString()}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-danger" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.removeFromWhitelist(entry.ip).then(() => queryClient.invalidateQueries({ queryKey: SECURITY_WHITELIST_KEY })).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-trash-fill"/>
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>

            {/* Blacklist */}
            <div className="admin-card">
                <h3>Blacklist <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({blacklist.length} IPs)</span>
                </h3>
                <form onSubmit={handleAddBlacklist} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={blIp} onChange={e => { setBlIp(e.target.value); }} placeholder="IP address"
                           style={{flex: 1, ...inputBaseStyle}}/>
                    <input type="text" value={blComment} onChange={e => { setBlComment(e.target.value); }}
                           placeholder="Comment (optional)" style={{flex: 1, ...inputBaseStyle}}/>
                    <button type="submit" className="admin-btn admin-btn-danger"><i className="bi bi-plus-lg"/> Block
                    </button>
                </form>
                {blacklist.length > 0 && (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thSortStyle} onClick={() => { if (blSortBy === 'ip') setBlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBlSortBy('ip'); setBlSortOrder('asc'); } }}>IP{sortIndicator('ip', blSortBy, blSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (blSortBy === 'comment') setBlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBlSortBy('comment'); setBlSortOrder('asc'); } }}>Comment{sortIndicator('comment', blSortBy, blSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (blSortBy === 'added_by') setBlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBlSortBy('added_by'); setBlSortOrder('asc'); } }}>Added By{sortIndicator('added_by', blSortBy, blSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (blSortBy === 'added_at') setBlSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBlSortBy('added_at'); setBlSortOrder('asc'); } }}>Added{sortIndicator('added_at', blSortBy, blSortOrder)}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedBl.map(entry => (
                                <tr key={entry.ip}>
                                    <td><code>{entry.ip}</code></td>
                                    <td style={mutedStyle}>{entry.comment || '—'}</td>
                                    <td style={mutedStyle}>{entry.added_by || '—'}</td>
                                    <td style={mutedStyle}>{new Date(entry.added_at).toLocaleDateString()}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.removeFromBlacklist(entry.ip).then(() => queryClient.invalidateQueries({ queryKey: SECURITY_BLACKLIST_KEY })).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-check-lg"/> Unblock
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>

            {/* Banned IPs */}
            <div className="admin-card">
                <h3>Banned IPs <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({bannedIPs.length} active)</span></h3>
                <form onSubmit={handleBan} style={{display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap'}}>
                    <input type="text" value={banIp} onChange={e => { setBanIp(e.target.value); }} placeholder="IP address"
                           style={{flex: 1, minWidth: 140, ...inputBaseStyle}}/>
                    <select value={banDuration} onChange={e => { setBanDuration(Number(e.target.value)); }} style={inputBaseStyle}>
                        <option value={15}>15 min</option>
                        <option value={60}>1 hour</option>
                        <option value={1440}>24 hours</option>
                        <option value={10080}>7 days</option>
                    </select>
                    <button type="submit" className="admin-btn admin-btn-danger"><i className="bi bi-ban"/> Ban</button>
                </form>
                {bannedIPs.length > 0 && (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thSortStyle} onClick={() => { if (banSortBy === 'ip') setBanSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBanSortBy('ip'); setBanSortOrder('asc'); } }}>IP{sortIndicator('ip', banSortBy, banSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (banSortBy === 'reason') setBanSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBanSortBy('reason'); setBanSortOrder('asc'); } }}>Reason{sortIndicator('reason', banSortBy, banSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (banSortBy === 'banned_at') setBanSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBanSortBy('banned_at'); setBanSortOrder('asc'); } }}>Banned At{sortIndicator('banned_at', banSortBy, banSortOrder)}</th>
                                <th style={thSortStyle} onClick={() => { if (banSortBy === 'expires_at') setBanSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setBanSortBy('expires_at'); setBanSortOrder('asc'); } }}>Expires{sortIndicator('expires_at', banSortBy, banSortOrder)}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedBans.map(ban => (
                                <tr key={ban.ip}>
                                    <td><code>{ban.ip}</code></td>
                                    <td style={mutedStyle}>{ban.reason || '—'}</td>
                                    <td style={mutedStyle}>{new Date(ban.banned_at).toLocaleString()}</td>
                                    <td style={mutedStyle}>{ban.expires_at ? new Date(ban.expires_at).toLocaleString() : 'Permanent'}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.unbanIP(ban.ip).then(() => queryClient.invalidateQueries({ queryKey: SECURITY_BANNED_KEY })).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-check-lg"/> Unban
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    )
}
