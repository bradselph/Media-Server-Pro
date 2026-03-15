import {describe, expect, it, vi, afterEach} from 'vitest'
import {ApiError, apiRequest, api} from './client'

// Stub the global fetch for unit tests
const mockFetch = vi.fn<typeof fetch>()
vi.stubGlobal('fetch', mockFetch)

function makeResponse(body: string, status = 200): Response {
    return {
        status,
        text: async () => body,
    } as Response
}

function jsonResponse(data: unknown, status = 200): Response {
    return makeResponse(JSON.stringify(data), status)
}

afterEach(() => {
    mockFetch.mockReset()
})

describe('apiRequest', () => {
    it('unwraps successful envelope', async () => {
        mockFetch.mockResolvedValueOnce(jsonResponse({success: true, data: {id: 1}}))

        const result = await apiRequest<{id: number}>('/api/test')
        expect(result).toEqual({id: 1})
    })

    it('throws ApiError on failure envelope', async () => {
        mockFetch.mockResolvedValueOnce(
            jsonResponse({success: false, error: 'Not found'}, 404),
        )

        const err = await apiRequest('/api/missing').catch((e: unknown) => e)
        expect(err).toBeInstanceOf(ApiError)
        expect((err as ApiError).status).toBe(404)
        expect((err as ApiError).message).toBe('Not found')
    })

    it('handles 204 No Content', async () => {
        mockFetch.mockResolvedValueOnce({status: 204} as Response)

        const result = await apiRequest<undefined>('/api/no-content')
        expect(result).toBeUndefined()
    })

    it('throws on invalid JSON', async () => {
        mockFetch.mockResolvedValueOnce(makeResponse('<html>Server Error</html>', 500))

        const err = await apiRequest('/api/bad').catch((e: unknown) => e)
        expect(err).toBeInstanceOf(ApiError)
        expect((err as ApiError).status).toBe(500)
        expect((err as ApiError).message).toContain('Invalid JSON')
    })

    it('sends credentials include', async () => {
        mockFetch.mockResolvedValueOnce(jsonResponse({success: true, data: null}))
        await apiRequest('/api/test')

        expect(mockFetch).toHaveBeenCalledWith(
            '/api/test',
            expect.objectContaining({credentials: 'include'}),
        )
    })
})

describe('api convenience methods', () => {
    it('api.get sends GET request', async () => {
        mockFetch.mockResolvedValueOnce(jsonResponse({success: true, data: []}))
        await api.get('/api/items')

        expect(mockFetch).toHaveBeenCalledWith(
            '/api/items',
            expect.objectContaining({credentials: 'include'}),
        )
        // GET is the default method — fetch omits it or sets undefined
        const callOptions = mockFetch.mock.calls[0][1] as RequestInit
        expect(callOptions.method).toBeUndefined()
    })

    it('api.post sends POST with JSON body', async () => {
        mockFetch.mockResolvedValueOnce(jsonResponse({success: true, data: {id: 2}}))
        await api.post('/api/items', {name: 'test'})

        const callOptions = mockFetch.mock.calls[0][1] as RequestInit
        expect(callOptions.method).toBe('POST')
        expect(callOptions.body).toBe(JSON.stringify({name: 'test'}))
        expect((callOptions.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    })

    it('api.delete sends DELETE request', async () => {
        mockFetch.mockResolvedValueOnce(jsonResponse({success: true, data: null}))
        await api.delete('/api/items/1')

        const callOptions = mockFetch.mock.calls[0][1] as RequestInit
        expect(callOptions.method).toBe('DELETE')
    })

    it('api.upload sends FormData without Content-Type header', async () => {
        mockFetch.mockResolvedValueOnce(jsonResponse({success: true, data: {url: '/file.jpg'}}))
        const formData = new FormData()
        formData.append('file', new Blob(['test']), 'test.txt')

        await api.upload('/api/upload', formData)

        const callOptions = mockFetch.mock.calls[0][1] as RequestInit
        expect(callOptions.method).toBe('POST')
        expect(callOptions.body).toBe(formData)
        // Content-Type should NOT be set (empty headers object) so the browser
        // can auto-set the multipart boundary
        const headers = callOptions.headers as Record<string, string>
        expect(headers['Content-Type']).toBeUndefined()
    })
})

describe('ApiError', () => {
    it('has correct name, status, message, and code', () => {
        const err = new ApiError('oops', 403, 'FORBIDDEN')
        expect(err.name).toBe('ApiError')
        expect(err.status).toBe(403)
        expect(err.code).toBe('FORBIDDEN')
        expect(err.message).toBe('oops')
        expect(err).toBeInstanceOf(Error)
    })
})
