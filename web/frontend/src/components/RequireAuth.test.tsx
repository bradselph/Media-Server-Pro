import { render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { RequireAuth } from './RequireAuth'
import { useAuthStore } from '@/stores/authStore'

vi.mock('@/stores/authStore', () => ({
    useAuthStore: vi.fn(),
}))

// Helper component to display current location for redirect assertions
function LocationDisplay() {
    const location = useLocation()
    return <div data-testid="location">{location.pathname}{location.search}</div>
}

function renderWithRouter(ui: React.ReactNode, initialEntries: string[] = ['/']) {
    return render(
        <MemoryRouter initialEntries={initialEntries}>
            {ui}
            <LocationDisplay />
        </MemoryRouter>,
    )
}

describe('RequireAuth', () => {
    it('shows loading state when isLoading is true', () => {
        vi.mocked(useAuthStore).mockReturnValue({
            isLoading: true,
            isAuthenticated: false,
            isAdmin: false,
        } as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth>
                <div>Protected content</div>
            </RequireAuth>,
        )

        expect(screen.getByText('Loading...')).toBeInTheDocument()
        expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
    })

    it('renders children when authenticated', () => {
        vi.mocked(useAuthStore).mockReturnValue({
            isLoading: false,
            isAuthenticated: true,
            isAdmin: false,
        } as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth>
                <div>Protected content</div>
            </RequireAuth>,
        )

        expect(screen.getByText('Protected content')).toBeInTheDocument()
    })

    it('redirects to login when not authenticated', () => {
        vi.mocked(useAuthStore).mockReturnValue({
            isLoading: false,
            isAuthenticated: false,
            isAdmin: false,
        } as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth>
                <div>Protected content</div>
            </RequireAuth>,
            ['/profile'],
        )

        expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
        // Navigate redirects, so the LocationDisplay should reflect the login path
        const locationEl = screen.getByTestId('location')
        expect(locationEl.textContent).toBe('/login?redirect=%2Fprofile')
    })

    it('redirects to login with search and hash in redirect param', () => {
        vi.mocked(useAuthStore).mockReturnValue({
            isLoading: false,
            isAuthenticated: false,
            isAdmin: false,
        } as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth>
                <div>Protected content</div>
            </RequireAuth>,
            ['/player?id=42#time=120'],
        )

        const locationEl = screen.getByTestId('location')
        expect(locationEl.textContent).toContain('/login?redirect=')
        expect(locationEl.textContent).toContain(encodeURIComponent('/player?id=42#time=120'))
    })

    it('redirects non-admin from adminOnly route', () => {
        vi.mocked(useAuthStore).mockReturnValue({
            isLoading: false,
            isAuthenticated: true,
            isAdmin: false,
        } as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth adminOnly>
                <div>Admin content</div>
            </RequireAuth>,
            ['/admin'],
        )

        expect(screen.queryByText('Admin content')).not.toBeInTheDocument()
        const locationEl = screen.getByTestId('location')
        expect(locationEl.textContent).toBe('/login')
    })

    it('renders children for admin on adminOnly route', () => {
        vi.mocked(useAuthStore).mockReturnValue({
            isLoading: false,
            isAuthenticated: true,
            isAdmin: true,
        } as ReturnType<typeof useAuthStore>)

        renderWithRouter(
            <RequireAuth adminOnly>
                <div>Admin content</div>
            </RequireAuth>,
            ['/admin'],
        )

        expect(screen.getByText('Admin content')).toBeInTheDocument()
    })
})
