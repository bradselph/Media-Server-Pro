export function errMsg(err: unknown): string {
    return err instanceof Error ? err.message : String(err)
}

export function formatBytes(bytes: number): string {
    if (!bytes) return '0 B'
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`
}

export function formatUptime(secs: number): string {
    const d = Math.floor(secs / 86400)
    const h = Math.floor((secs % 86400) / 3600)
    const m = Math.floor((secs % 3600) / 60)
    if (d > 0) return `${d}d ${h}h ${m}m`
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
}

export function SubTabs({items, active, onChange}: {
    items: { id: string; label: string }[]
    active: string
    onChange: (id: string) => void
}) {
    return (
        <div className="admin-subtab-nav">
            {items.map(item => (
                <button key={item.id}
                        className={`admin-subtab-btn ${active === item.id ? 'active' : ''}`}
                        onClick={() => onChange(item.id)}>
                    {item.label}
                </button>
            ))}
        </div>
    )
}
