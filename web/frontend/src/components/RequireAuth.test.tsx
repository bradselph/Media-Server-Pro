import { render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation, Routes, Route } from 'react-router-dom'
import { RequireAuth } from './RequireAuth'
import { useAuthStore } from '@/stores/authStore'

vi.mock('@/stores/authStore', () => ({
    useAuthStore: vi.fn(),
}))

const mockUseAuthStore = vi.mocked(useAuthStore)

// Helper component to display the current location after any Navigate redirects
function LocationDisplay() {
    const location = useLocation()
    return <div data-testid="location">{location.pathname}{location.search}</div>
}

// Render RequireAuth within a router that also renders LocationDisplay on all routes
function renderWithRouter(
    ui: React.ReactNode,
    { initialEntries = ['/'] }: { initialEntries?: string[] } = {},
) {
    return render(
        <MemoryRouter initialEntries={initialEntries}>
            <Routes>
                <Route path="*" element={<>{ui}<LocationDisplay /></>} />
            </Routes>
        </MemoryRouter>,
    )
}

describe('RequireAuth', () => {
    afterEach(() => {
        vi.restoreAllMocks()
    })

    it('shows loading state when isLoading is true', () => {
        mockUseAuthStore.mockReturnValue({
            isLoading: true,
            isAuthenticated: false,
            isAdmin: false,
        } as unknown as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth>
                <div>Protected content</div>
            </RequireAuth>,
        )

        expect(screen.getByText('Loading...')).toBeInTheDocument()
        expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
    })

    it('renders children when authenticated', () => {
        mockUseAuthStore.mockReturnValue({
            isLoading: false,
            isAuthenticated: true,
            isAdmin: false,
        } as unknown as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth>
                <div>Protected content</div>
            </RequireAuth>,
        )

        expect(screen.getByText('Protected content')).toBeInTheDocument()
    })

    it('redirects to login when not authenticated', () => {
        mockUseAuthStore.mockReturnValue({
            isLoading: false,
            isAuthenticated: false,
            isAdmin: false,
        } as unknown as ReturnType<typeof useAuthStore>)

        render(
            <MemoryRouter initialEntries={['/profile']}>
                <Routes>
                    <Route
                        path="/profile"
                        element={
                            <RequireAuth>
                                <div>Protected content</div>
                            </RequireAuth>
                        }
                    />
                    <Route path="*" element={<LocationDisplay />} />
                </Routes>
            </MemoryRouter>,
        )

        expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
        const locationEl = screen.getByTestId('location')
        expect(locationEl.textContent).toBe('/login?redirect=%2Fprofile')
    })

    it('redirects non-admin from adminOnly route', () => {
        mockUseAuthStore.mockReturnValue({
            isLoading: false,
            isAuthenticated: true,
            isAdmin: false,
        } as unknown as ReturnType<typeof useAuthStore>)

        render(
            <MemoryRouter initialEntries={['/admin']}>
                <Routes>
                    <Route
                        path="/admin"
                        element={
                            <RequireAuth adminOnly>
                                <div>Admin content</div>
                            </RequireAuth>
                        }
                    />
                    <Route path="*" element={<LocationDisplay />} />
                </Routes>
            </MemoryRouter>,
        )

        expect(screen.queryByText('Admin content')).not.toBeInTheDocument()
        const locationEl = screen.getByTestId('location')
        expect(locationEl.textContent).toBe('/login')
    })

    it('renders children for admin on adminOnly route', () => {
        mockUseAuthStore.mockReturnValue({
            isLoading: false,
            isAuthenticated: true,
            isAdmin: true,
        } as unknown as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth adminOnly>
                <div>Admin content</div>
            </RequireAuth>,
            { initialEntries: ['/admin'] },
        )

        expect(screen.getByText('Admin content')).toBeInTheDocument()
    })
})
