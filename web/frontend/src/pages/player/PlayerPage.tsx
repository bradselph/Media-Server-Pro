import { useSearchParams } from 'react-router-dom'
import { ApiError } from '@/api/client'
import { PlayerEmptyView } from './PlayerEmptyView'
import { PlayerPageContent } from './PlayerPageContent'
import { usePlayerPageState } from './usePlayerPageState'
import '@/styles/player.css'

export function PlayerPage() {
    const [searchParams] = useSearchParams()
    const mediaId = searchParams.get('id') ?? ''
    const state = usePlayerPageState(mediaId)

    if (!mediaId) return <PlayerEmptyView variant="no-id" />
    if (state.mediaLoading) return <PlayerEmptyView variant="loading" />
    if (state.mediaError || !state.media) {
        const is403 = state.mediaError instanceof ApiError && state.mediaError.status === 403
        const errMsg = state.mediaError instanceof ApiError ? state.mediaError.message : ''
        const playerUrl = `/player?id=${encodeURIComponent(mediaId)}`
        return (
            <PlayerEmptyView
                variant="error"
                is403={is403}
                errMsg={errMsg}
                playerUrl={playerUrl}
            />
        )
    }

    return <PlayerPageContent {...state} />
}
