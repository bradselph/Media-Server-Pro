import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/api/endpoints'
import type { CategorizedItem, CategoryStats } from '@/api/types'
import { errMsg } from './adminUtils'

const inputStyle = {
    flex: 1,
    padding: '6px 10px',
    border: '1px solid var(--border-color)',
    borderRadius: 6,
    background: 'var(--input-bg)',
    color: 'var(--text-color)',
    fontSize: 13,
} as const

interface Message { type: 'success' | 'error'; text: string }

function useCategorizer() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<Message | null>(null)
    const [catPath, setCatPath] = useState('')
    const [categorizing, setCategorizing] = useState(false)
    const [catResult, setCatResult] = useState<CategorizedItem | null>(null)
    const [browseCat, setBrowseCat] = useState('')
    const [browseResults, setBrowseResults] = useState<CategorizedItem[] | null>(null)
    const [setPath, setSetPath] = useState('')
    const [setCategory, setSetCategoryValue] = useState('')
    const [cleaning, setCleaning] = useState(false)

    const { data: catStats } = useQuery<CategoryStats>({
        queryKey: ['admin-category-stats'],
        queryFn: () => adminApi.getCategoryStats(),
    })

    const categories = catStats ? Object.keys(catStats.by_category).sort() : []

    async function handleCategorize(e: React.FormEvent) {
        e.preventDefault()
        if (!catPath.trim()) return
        setCategorizing(true)
        setCatResult(null)
        setMsg(null)
        try {
            const result = await adminApi.categorizeFile(catPath.trim())
            setCatResult(result)
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setCategorizing(false)
        }
    }

    async function handleBrowseCategory() {
        if (!browseCat) return
        try {
            const results = await adminApi.getByCategory(browseCat)
            setBrowseResults(results)
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        }
    }

    async function handleSetCategory(e: React.FormEvent) {
        e.preventDefault()
        if (!setPath.trim() || !setCategory.trim()) return
        try {
            await adminApi.setMediaCategory(setPath.trim(), setCategory.trim())
            setMsg({
                type: 'success',
                text: `Category set to "${setCategory}" for "${setPath.split(/[\\/]/).pop()}"`,
            })
            setSetPath('')
            setSetCategoryValue('')
            await queryClient.invalidateQueries({ queryKey: ['admin-category-stats'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        }
    }

    async function handleClean() {
        if (!window.confirm('Remove stale category entries?')) return
        setCleaning(true)
        try {
            const res = await adminApi.cleanStaleCategories()
            setMsg({ type: 'success', text: `Cleaned ${res.removed} stale entries.` })
            await queryClient.invalidateQueries({ queryKey: ['admin-category-stats'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setCleaning(false)
        }
    }

    return {
        msg,
        catStats,
        categories,
        catPath,
        setCatPath,
        categorizing,
        catResult,
        browseCat,
        setBrowseCat,
        browseResults,
        setPath,
        setSetPath,
        setCategory,
        setSetCategoryValue,
        cleaning,
        handleCategorize,
        handleBrowseCategory,
        handleSetCategory,
        handleClean,
    }
}

function MessageAlert({ msg }: { msg: Message }) {
    return (
        <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>
            {msg.text}
        </div>
    )
}

function CategoryStatsCard({
    catStats,
    onClean,
    cleaning,
}: {
    catStats: CategoryStats
    onClean: () => void
    cleaning: boolean
}) {
    return (
        <div className="admin-card">
            <div
                style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginBottom: 12,
                }}
            >
                <h2 style={{ margin: 0 }}>Category Stats</h2>
                <button className="admin-btn admin-btn-warning" onClick={onClean} disabled={cleaning}>
                    <i className="bi bi-trash" /> {cleaning ? 'Cleaning...' : 'Clean Stale'}
                </button>
            </div>
            <div className="admin-stats-grid" style={{ marginBottom: 12 }}>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{catStats.total_items.toLocaleString()}</span>
                    <span className="admin-stat-label">Categorized</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{catStats.manual_overrides.toLocaleString()}</span>
                    <span className="admin-stat-label">Manual Overrides</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{Object.keys(catStats.by_category).length}</span>
                    <span className="admin-stat-label">Categories</span>
                </div>
            </div>
            {Object.keys(catStats.by_category).length > 0 && (
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                            <tr>
                                <th>Category</th>
                                <th>Count</th>
                            </tr>
                        </thead>
                        <tbody>
                            {Object.entries(catStats.by_category)
                                .sort((a, b) => b[1] - a[1])
                                .map(([cat, count]) => (
                                    <tr key={cat}>
                                        <td>{cat}</td>
                                        <td>{count.toLocaleString()}</td>
                                    </tr>
                                ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    )
}

function CategorizeFileCard({
    catPath,
    setCatPath,
    categorizing,
    catResult,
    onSubmit,
}: {
    catPath: string
    setCatPath: (v: string) => void
    categorizing: boolean
    catResult: CategorizedItem | null
    onSubmit: (e: React.FormEvent) => void
}) {
    return (
        <div className="admin-card">
            <h3>Categorize File</h3>
            <form onSubmit={onSubmit} style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
                <input
                    type="text"
                    value={catPath}
                    onChange={(e) => { setCatPath(e.target.value); }}
                    placeholder="Media file path..."
                    style={inputStyle}
                />
                <button
                    type="submit"
                    className="admin-btn admin-btn-primary"
                    disabled={categorizing || !catPath.trim()}
                >
                    <i className="bi bi-tag" /> {categorizing ? 'Analyzing...' : 'Categorize'}
                </button>
            </form>
            {catResult && (
                <div
                    style={{
                        padding: '10px 12px',
                        background: 'var(--card-bg)',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        fontSize: 13,
                    }}
                >
                    <p>
                        <strong>Category:</strong> {catResult.category}
                    </p>
                    <p>
                        <strong>Confidence:</strong> {(catResult.confidence * 100).toFixed(0)}%
                    </p>
                    <p>
                        <strong>Manual Override:</strong> {catResult.manual_override ? 'Yes' : 'No'}
                    </p>
                </div>
            )}
        </div>
    )
}

function SetCategoryCard({
    setPath,
    setSetPath,
    setCategory,
    setSetCategoryValue,
    onSubmit,
}: {
    setPath: string
    setSetPath: (v: string) => void
    setCategory: string
    setSetCategoryValue: (v: string) => void
    onSubmit: (e: React.FormEvent) => void
}) {
    return (
        <div className="admin-card">
            <h3>Set Category Manually</h3>
            <form onSubmit={onSubmit} style={{ display: 'flex', gap: 8 }}>
                <input
                    type="text"
                    value={setPath}
                    onChange={(e) => { setSetPath(e.target.value); }}
                    placeholder="Media file path..."
                    style={{ ...inputStyle, flex: 2 }}
                />
                <input
                    type="text"
                    value={setCategory}
                    onChange={(e) => { setSetCategoryValue(e.target.value); }}
                    placeholder="Category name..."
                    style={inputStyle}
                />
                <button
                    type="submit"
                    className="admin-btn admin-btn-primary"
                    disabled={!setPath.trim() || !setCategory.trim()}
                >
                    <i className="bi bi-check-lg" /> Set
                </button>
            </form>
        </div>
    )
}

function BrowseByCategoryCard({
    categories,
    browseCat,
    setBrowseCat,
    browseResults,
    onBrowse,
}: {
    categories: string[]
    browseCat: string
    setBrowseCat: (v: string) => void
    browseResults: CategorizedItem[] | null
    onBrowse: () => void
}) {
    return (
        <div className="admin-card">
            <h3>Browse by Category</h3>
            <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
                <select value={browseCat} onChange={(e) => { setBrowseCat(e.target.value); }} style={inputStyle}>
                    <option value="">Select category...</option>
                    {categories.map((c) => (
                        <option key={c} value={c}>
                            {c}
                        </option>
                    ))}
                </select>
                <button
                    className="admin-btn admin-btn-primary"
                    onClick={onBrowse}
                    disabled={!browseCat}
                >
                    <i className="bi bi-search" /> Browse
                </button>
            </div>
            {browseResults && (
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                            <tr>
                                <th>File</th>
                                <th>Category</th>
                                <th>Confidence</th>
                                <th>Manual</th>
                            </tr>
                        </thead>
                        <tbody>
                            {browseResults.length === 0 ? (
                                <tr>
                                    <td colSpan={4} style={{ textAlign: 'center', color: 'var(--text-muted)' }}>
                                        No items in this category
                                    </td>
                                </tr>
                            ) : (
                                [...browseResults]
                                    .sort((a, b) => a.name.localeCompare(b.name))
                                    .map((item) => (
                                        <tr key={item.id}>
                                            <td
                                                style={{
                                                    maxWidth: 200,
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                }}
                                                title={item.name}
                                            >
                                                {item.name}
                                            </td>
                                            <td>{item.category}</td>
                                            <td>{(item.confidence * 100).toFixed(0)}%</td>
                                            <td>{item.manual_override ? 'Yes' : 'No'}</td>
                                        </tr>
                                    ))
                            )}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    )
}

export function CategorizerTab() {
    const {
        msg,
        catStats,
        categories,
        catPath,
        setCatPath,
        categorizing,
        catResult,
        browseCat,
        setBrowseCat,
        browseResults,
        setPath,
        setSetPath,
        setCategory,
        setSetCategoryValue,
        cleaning,
        handleCategorize,
        handleBrowseCategory,
        handleSetCategory,
        handleClean,
    } = useCategorizer()

    return (
        <div>
            {msg && <MessageAlert msg={msg} />}
            {catStats && (
                <CategoryStatsCard catStats={catStats} onClean={handleClean} cleaning={cleaning} />
            )}
            <CategorizeFileCard
                catPath={catPath}
                setCatPath={setCatPath}
                categorizing={categorizing}
                catResult={catResult}
                onSubmit={handleCategorize}
            />
            <SetCategoryCard
                setPath={setPath}
                setSetPath={setSetPath}
                setCategory={setCategory}
                setSetCategoryValue={setSetCategoryValue}
                onSubmit={handleSetCategory}
            />
            <BrowseByCategoryCard
                categories={categories}
                browseCat={browseCat}
                setBrowseCat={setBrowseCat}
                browseResults={browseResults}
                onBrowse={handleBrowseCategory}
            />
        </div>
    )
}
