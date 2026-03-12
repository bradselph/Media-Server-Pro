/** Media, similar, and trending queries. Extracted to reduce usePlayerPageState complexity. */
import { useQuery } from '@tanstack/react-query'
import { ApiError } from '@/api/client'
import { mediaApi, suggestionsApi } from '@/api/endpoints'
import type { Suggestion } from '@/api/types'

export interface MediaQueryContext {
    mediaId: string
    canViewMature: boolean
}

interface RelatedQueryInput {
    similarData: Suggestion[]
    similarError: boolean
    relatedLoading: boolean
    trendingData: Suggestion[]
    trendingLoading: boolean
}

function shouldRetryMediaQuery(failureCount: number, error: unknown): boolean {
    if (error instanceof ApiError && error.status === 503) return failureCount < 5
    return failureCount < 1
}

const mediaQueryRetryDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 10000)

function deriveRelatedData(opts: RelatedQueryInput) {
    const { similarData, similarError, relatedLoading, trendingData, trendingLoading } = opts
    const useFallback = similarData.length === 0 && !similarError && !relatedLoading
    const hasSimilar = similarData.length > 0
    return {
        related: hasSimilar ? similarData : trendingData,
        relatedLabel: hasSimilar ? 'Similar Media' : 'More to Explore',
        relatedStillLoading: relatedLoading || (useFallback && trendingLoading),
    }
}

export function usePlayerMediaQueries(ctx: MediaQueryContext) {
    const { mediaId, canViewMature } = ctx
    const { data: media, isLoading: mediaLoading, error: mediaError } = useQuery({
        queryKey: ['media-item', mediaId],
        queryFn: () => mediaApi.get(mediaId),
        enabled: !!mediaId,
        retry: shouldRetryMediaQuery,
        retryDelay: mediaQueryRetryDelay,
    })

    const {
        data: similarData = [],
        isLoading: relatedLoading,
        isError: similarError,
        refetch: similarRefetch,
    } = useQuery<Suggestion[]>({
        queryKey: ['media-similar', mediaId, canViewMature],
        queryFn: () => suggestionsApi.getSimilar(mediaId ?? ''),
        enabled: !!mediaId,
        retry: shouldRetryMediaQuery,
        retryDelay: mediaQueryRetryDelay,
        select: (data) => (data ?? []).slice(0, 8),
    })

    const trendingEnabled =
        !!mediaId && !relatedLoading && !similarError && similarData.length === 0
    const { data: trendingData = [], isLoading: trendingLoading } = useQuery<Suggestion[]>({
        queryKey: ['suggestions-trending', canViewMature],
        queryFn: () => suggestionsApi.getTrending(),
        enabled: trendingEnabled,
        staleTime: 60 * 1000,
        select: (data) => (data ?? []).slice(0, 8),
    })

    const { related, relatedLabel, relatedStillLoading } = deriveRelatedData({
        similarData,
        similarError,
        relatedLoading,
        trendingData,
        trendingLoading,
    })

    return {
        media,
        mediaLoading,
        mediaError,
        related,
        relatedLabel,
        relatedStillLoading,
        similarError,
        similarRefetch,
    }
}
