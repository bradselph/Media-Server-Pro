import {useCallback, useState} from 'react'
import {ToastContext, type ToastType} from '@/hooks/useToast'

interface Toast {
    id: number
    message: string
    type: ToastType
}

let nextId = 0

export function ToastProvider({children}: { children: React.ReactNode }) {
    const [toasts, setToasts] = useState<Toast[]>([])

    const showToast = useCallback((message: string, type: ToastType = 'info') => {
        const id = nextId++
        setToasts((prev) => [...prev, {id, message, type}])
        setTimeout(() => {
            setToasts((prev) => prev.filter((t) => t.id !== id))
        }, 4000)
    }, [])

    return (
        <ToastContext.Provider value={{showToast}}>
            {children}
            <div className="toast-container">
                {toasts.map((toast) => (
                    <div key={toast.id} className={`toast toast-${toast.type}`}>
                        {toast.message}
                    </div>
                ))}
            </div>
        </ToastContext.Provider>
    )
}
