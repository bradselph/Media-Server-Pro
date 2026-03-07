import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {BannedIP, IPEntry, SecurityStats} from '@/api/types'
import {errMsg} from './helpers'

// ── Tab: Security (Feature 10) ────────────────────────────────────────────────

type IPSortKey = 'ip' | 'comment' | 'added_at'
type BanSortKey = 'ip' | 'reason' | 'banned_at' | 'expires_at'

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
        queryKey: ['admin-security-stats'],
        queryFn: () => adminApi.getSecurityStats(),
        refetchInterval: 30000,
    })

    const {data: whitelist = []} = useQuery<IPEntry[]>({
        queryKey: ['admin-security-whitelist'],
        queryFn: () => adminApi.getWhitelist(),
    })

    const {data: blacklist = []} = useQuery<IPEntry[]>({
        queryKey: ['admin-security-blacklist'],
        queryFn: () => adminApi.getBlacklist(),
    })

    const {data: bannedIPs = []} = useQuery<BannedIP[]>({
        queryKey: ['admin-security-banned'],
        queryFn: () => adminApi.getBannedIPs(),
        refetchInterval: 30000,
    })

    async function handleAddWhitelist(e: React.FormEvent) {
        e.preventDefault()
        if (!wlIp.trim()) return
        try {
            await adminApi.addToWhitelist(wlIp.trim(), wlComment.trim() || undefined)
            setMsg({type: 'success', text: `${wlIp} added to whitelist`})
            setWlIp('');
            setWlComment('')
            await queryClient.invalidateQueries({queryKey: ['admin-security-whitelist']})
            await queryClient.invalidateQueries({queryKey: ['admin-security-stats']})
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
            await queryClient.invalidateQueries({queryKey: ['admin-security-stats']})
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
            await queryClient.invalidateQueries({queryKey: ['admin-security-banned']})
            await queryClient.invalidateQueries({queryKey: ['admin-security-stats']})
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
                       onChange={e => setIpSearch(e.target.value)}
                       style={{width: '100%', maxWidth: 300, padding: '6px 10px', border: '1px solid var(--border-color)',
                               borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}} />
            </div>

            {/* Whitelist */}
            <div className="admin-card">
                <h3>Whitelist <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({whitelist.length} IPs)</span>
                </h3>
                <form onSubmit={handleAddWhitelist} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={wlIp} onChange={e => setWlIp(e.target.value)} placeholder="IP address"
                           style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                               background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <input type="text" value={wlComment} onChange={e => setWlComment(e.target.value)}
                           placeholder="Comment (optional)" style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)',
                        borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <button type="submit" className="admin-btn admin-btn-primary"><i className="bi bi-plus-lg"/> Add
                    </button>
                </form>
                {whitelist.length > 0 && (() => {
                    const sortedWl = whitelist
                        .filter(e => !ipSearch || e.ip.includes(ipSearch) || (e.comment || '').toLowerCase().includes(ipSearch.toLowerCase()))
                        .sort((a, b) => {
                            let cmp = 0
                            switch (wlSortBy) {
                                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                                case 'comment': cmp = (a.comment || '').localeCompare(b.comment || ''); break
                                case 'added_at': cmp = a.added_at.localeCompare(b.added_at); break
                            }
                            return wlSortOrder === 'desc' ? -cmp : cmp
                        })
                    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                    function wlInd(col: IPSortKey) {
                        if (wlSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                        return <span style={{marginLeft: 4}}>{wlSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    }
                    function handleWlSort(col: IPSortKey) {
                        if (wlSortBy === col) setWlSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                        else { setWlSortBy(col); setWlSortOrder('asc') }
                    }
                    return (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thS} onClick={() => handleWlSort('ip')}>IP{wlInd('ip')}</th>
                                <th style={thS} onClick={() => handleWlSort('comment')}>Comment{wlInd('comment')}</th>
                                <th style={thS} onClick={() => handleWlSort('added_at')}>Added{wlInd('added_at')}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedWl.map(entry => (
                                <tr key={entry.ip}>
                                    <td><code>{entry.ip}</code></td>
                                    <td style={{color: 'var(--text-muted)', fontSize: 12}}>{entry.comment || '—'}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{new Date(entry.added_at).toLocaleDateString()}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-danger" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.removeFromWhitelist(entry.ip).then(() => void queryClient.invalidateQueries({queryKey: ['admin-security-whitelist']})).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-trash-fill"/>
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>)
                })()}
            </div>

            {/* Blacklist */}
            <div className="admin-card">
                <h3>Blacklist <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({blacklist.length} IPs)</span>
                </h3>
                <form onSubmit={handleAddBlacklist} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={blIp} onChange={e => setBlIp(e.target.value)} placeholder="IP address"
                           style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                               background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <input type="text" value={blComment} onChange={e => setBlComment(e.target.value)}
                           placeholder="Comment (optional)" style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)',
                        borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <button type="submit" className="admin-btn admin-btn-danger"><i className="bi bi-plus-lg"/> Block
                    </button>
                </form>
                {blacklist.length > 0 && (() => {
                    const sortedBl = blacklist
                        .filter(e => !ipSearch || e.ip.includes(ipSearch) || (e.comment || '').toLowerCase().includes(ipSearch.toLowerCase()))
                        .sort((a, b) => {
                            let cmp = 0
                            switch (blSortBy) {
                                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                                case 'comment': cmp = (a.comment || '').localeCompare(b.comment || ''); break
                                case 'added_at': cmp = a.added_at.localeCompare(b.added_at); break
                            }
                            return blSortOrder === 'desc' ? -cmp : cmp
                        })
                    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                    function blInd(col: IPSortKey) {
                        if (blSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                        return <span style={{marginLeft: 4}}>{blSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    }
                    function handleBlSort(col: IPSortKey) {
                        if (blSortBy === col) setBlSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                        else { setBlSortBy(col); setBlSortOrder('asc') }
                    }
                    return (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thS} onClick={() => handleBlSort('ip')}>IP{blInd('ip')}</th>
                                <th style={thS} onClick={() => handleBlSort('comment')}>Comment{blInd('comment')}</th>
                                <th style={thS} onClick={() => handleBlSort('added_at')}>Added{blInd('added_at')}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedBl.map(entry => (
                                <tr key={entry.ip}>
                                    <td><code>{entry.ip}</code></td>
                                    <td style={{color: 'var(--text-muted)', fontSize: 12}}>{entry.comment || '—'}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{new Date(entry.added_at).toLocaleDateString()}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.removeFromBlacklist(entry.ip).then(() => void queryClient.invalidateQueries({queryKey: ['admin-security-blacklist']})).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-check-lg"/> Unblock
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>)
                })()}
            </div>

            {/* Banned IPs */}
            <div className="admin-card">
                <h3>Banned IPs <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({bannedIPs.length} active)</span></h3>
                <form onSubmit={handleBan} style={{display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap'}}>
                    <input type="text" value={banIp} onChange={e => setBanIp(e.target.value)} placeholder="IP address"
                           style={{flex: 1, minWidth: 140, padding: '6px 10px', border: '1px solid var(--border-color)',
                               borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <select value={banDuration} onChange={e => setBanDuration(Number(e.target.value))} style={{
                        padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                        background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}>
                        <option value={15}>15 min</option>
                        <option value={60}>1 hour</option>
                        <option value={1440}>24 hours</option>
                        <option value={10080}>7 days</option>
                    </select>
                    <button type="submit" className="admin-btn admin-btn-danger"><i className="bi bi-ban"/> Ban</button>
                </form>
                {bannedIPs.length > 0 && (() => {
                    const sortedBans = bannedIPs
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
                        })
                    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                    function banInd(col: BanSortKey) {
                        if (banSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                        return <span style={{marginLeft: 4}}>{banSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    }
                    function handleBanSort(col: BanSortKey) {
                        if (banSortBy === col) setBanSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                        else { setBanSortBy(col); setBanSortOrder('asc') }
                    }
                    return (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thS} onClick={() => handleBanSort('ip')}>IP{banInd('ip')}</th>
                                <th style={thS} onClick={() => handleBanSort('reason')}>Reason{banInd('reason')}</th>
                                <th style={thS} onClick={() => handleBanSort('banned_at')}>Banned At{banInd('banned_at')}</th>
                                <th style={thS} onClick={() => handleBanSort('expires_at')}>Expires{banInd('expires_at')}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedBans.map(ban => (
                                <tr key={ban.ip}>
                                    <td><code>{ban.ip}</code></td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{ban.reason || '—'}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{new Date(ban.banned_at).toLocaleString()}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{ban.expires_at ? new Date(ban.expires_at).toLocaleString() : 'Permanent'}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.unbanIP(ban.ip).then(() => void queryClient.invalidateQueries({queryKey: ['admin-security-banned']})).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-check-lg"/> Unban
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>)
                })()}
            </div>
        </div>
    )
}
