import {useCallback, useEffect, useRef, useState} from 'react'
import type {Dispatch, MutableRefObject, SetStateAction} from 'react'
import type {DownloaderProgress} from '@/api/types'

export interface UseDownloaderWebSocketOptions {
    /** Called when a download reaches complete/error/cancelled so the UI can refetch files/import lists */
    onDownloadComplete?: () => void
}

interface UseDownloaderWebSocketResult {
    connected: boolean
    clientId: string | null
    activeDownloads: Map<string, DownloaderProgress>
    clearDownload: (id: string) => void
}

function scheduleTerminalDownloadCleanup(
    downloadId: string,
    completionTimers: MutableRefObject<Set<ReturnType<typeof setTimeout>>>,
    onComplete: (() => void) | undefined,
    setActiveDownloads: Dispatch<SetStateAction<Map<string, DownloaderProgress>>>,
): void {
    onComplete?.()
    const timer = setTimeout(() => {
        completionTimers.current.delete(timer)
        setActiveDownloads((p) => {
            const n = new Map(p)
            n.delete(downloadId)
            return n
        })
    }, 10000)
    completionTimers.current.add(timer)
}

export function useDownloaderWebSocket(options?: UseDownloaderWebSocketOptions): UseDownloaderWebSocketResult {
    const onCompleteRef = useRef(options?.onDownloadComplete)
    onCompleteRef.current = options?.onDownloadComplete
    const [connected, setConnected] = useState(false)
    const [clientId, setClientId] = useState<string | null>(null)
    const [activeDownloads, setActiveDownloads] = useState(() => new Map<string, DownloaderProgress>())
    const wsRef = useRef<WebSocket | null>(null)
    const reconnectTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
    const completionTimers = useRef<Set<ReturnType<typeof setTimeout>>>(new Set())
    const backoffRef = useRef(1000)
    const connectRef = useRef<() => void>(() => {})

    const connect = useCallback(() => {
        if (wsRef.current?.readyState === WebSocket.OPEN || wsRef.current?.readyState === WebSocket.CONNECTING) {
            return
        }

        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
        const ws = new WebSocket(`${proto}//${location.host}/ws/admin/downloader`)
        wsRef.current = ws

        ws.onopen = () => {
            setConnected(true)
            backoffRef.current = 1000
        }

        ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data)

                if (msg.type === 'connected' && msg.clientId) {
                    setClientId(msg.clientId)
                    return
                }

                if (msg.type === 'error') {
                    return
                }

                // Progress update
                if (msg.downloadId) {
                    setActiveDownloads((prev) => {
                        const next = new Map(prev)
                        next.set(msg.downloadId, msg as DownloaderProgress)
                        if (msg.status === 'complete' || msg.status === 'error' || msg.status === 'cancelled') {
                            scheduleTerminalDownloadCleanup(
                                msg.downloadId,
                                completionTimers,
                                onCompleteRef.current,
                                setActiveDownloads,
                            )
                        }
                        return next
                    })
                }
            } catch {
                // ignore non-JSON messages
            }
        }

        ws.onclose = () => {
            setConnected(false)
            setClientId(null)
            wsRef.current = null
            // Reconnect with exponential backoff
            reconnectTimer.current = setTimeout(() => {
                backoffRef.current = Math.min(backoffRef.current * 2, 30000)
                connectRef.current()
            }, backoffRef.current)
        }

        ws.onerror = () => {
            ws.close()
        }
    }, [])

    connectRef.current = connect

    useEffect(() => {
        connect()
        return () => {
            clearTimeout(reconnectTimer.current)
            const pending = completionTimers.current
            for (const t of pending) {
                clearTimeout(t)
            }
            pending.clear()
            wsRef.current?.close()
        }
    }, [connect])

    const clearDownload = useCallback((id: string) => {
        setActiveDownloads((prev) => {
            const next = new Map(prev)
            next.delete(id)
            return next
        })
    }, [])

    return {connected, clientId, activeDownloads, clearDownload}
}
