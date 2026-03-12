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
                        onClick={() => { onChange(item.id); }}>
                    {item.label}
                </button>
            ))}
        </div>
    )
}
