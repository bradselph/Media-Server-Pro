import {describe, expect, it, vi, beforeEach} from 'vitest'
import {ApiError, apiRequest} from './client'

// Stub the global fetch for unit tests
const mockFetch = vi.fn<typeof fetch>()
vi.stubGlobal('fetch', mockFetch)

function makeResponse(body: unknown, status = 200): Response {
    return {
        status,
        json: async () => body,
    } as Response
}

beforeEach(() => {
    mockFetch.mockReset()
})

describe('apiRequest', () => {
    it('unwraps the Go envelope and returns data on success', async () => {
        mockFetch.mockResolvedValueOnce(makeResponse({success: true, data: {id: 42}}))

        const result = await apiRequest<{id: number}>('/api/test')
        expect(result).toEqual({id: 42})
    })

    it('throws ApiError when success is false', async () => {
        mockFetch.mockResolvedValueOnce(
            makeResponse({success: false, error: 'not found'}, 404),
        )

        const err = await apiRequest('/api/missing').catch((e: unknown) => e)
        expect(err).toBeInstanceOf(ApiError)
        expect((err as ApiError).status).toBe(404)
        expect((err as ApiError).message).toBe('not found')
    })

    it('returns undefined for 204 No Content without parsing body', async () => {
        mockFetch.mockResolvedValueOnce({status: 204} as Response)

        const result = await apiRequest<undefined>('/api/no-content')
        expect(result).toBeUndefined()
    })

    it('forwards the AbortSignal to fetch', async () => {
        const controller = new AbortController()
        mockFetch.mockResolvedValueOnce(makeResponse({success: true, data: null}))

        await apiRequest('/api/test', {signal: controller.signal})

        expect(mockFetch).toHaveBeenCalledWith(
            '/api/test',
            expect.objectContaining({signal: controller.signal}),
        )
    })

    it('uses a fallback message when error field is absent', async () => {
        mockFetch.mockResolvedValueOnce(
            makeResponse({success: false}, 500),
        )

        expect(apiRequest('/api/fail')).rejects.toMatchObject({
            message: 'Unknown error',
            status: 500,
        })
    })
})

describe('ApiError', () => {
    it('has correct name, status, and message', () => {
        const err = new ApiError('oops', 403, 'FORBIDDEN')
        expect(err.name).toBe('ApiError')
        expect(err.status).toBe(403)
        expect(err.code).toBe('FORBIDDEN')
        expect(err.message).toBe('oops')
        expect(err).toBeInstanceOf(Error)
    })
})
