import {useCallback, useEffect, useRef, useState} from 'react'
import {ToastContext, type ToastType} from '@/hooks/useToast'

interface Toast {
    id: number
    message: string
    type: ToastType
}

let nextId = 0

export function ToastProvider({children}: { children: React.ReactNode }) {
    const [toasts, setToasts] = useState<Toast[]>([])
    const timersRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

    useEffect(() => () => {
        timersRef.current.forEach((t) => clearTimeout(t))
        timersRef.current.clear()
    }, [])

    const showToast = useCallback((message: string, type: ToastType = 'info') => {
        const id = nextId++
        setToasts((prev) => [...prev, {id, message, type}])
        const t = setTimeout(() => {
            timersRef.current.delete(id)
            setToasts((prev) => prev.filter((toast) => toast.id !== id))
        }, 4000)
        timersRef.current.set(id, t)
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
