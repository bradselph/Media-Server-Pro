import { render, screen, fireEvent } from '@testing-library/react'
import { ErrorBoundary, SectionErrorBoundary, PageLoader } from './ErrorBoundary'

// Helper component that conditionally throws
let throwCount = 0

function ThrowingComponent({ shouldThrow }: { shouldThrow: boolean }) {
    if (shouldThrow) throw new Error('Test error')
    return <div>Content loaded</div>
}

// Component that throws only on the first render, then succeeds on retry
function ThrowOnceComponent() {
    throwCount++
    if (throwCount === 1) throw new Error('First render error')
    return <div>Recovered content</div>
}

// Suppress console.error noise from React error boundaries
let consoleSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    throwCount = 0
})

afterEach(() => {
    consoleSpy.mockRestore()
})

describe('ErrorBoundary', () => {
    it('renders children when no error', () => {
        render(
            <ErrorBoundary>
                <div>Child content</div>
            </ErrorBoundary>,
        )
        expect(screen.getByText('Child content')).toBeInTheDocument()
    })

    it('shows error UI when child throws', () => {
        render(
            <ErrorBoundary>
                <ThrowingComponent shouldThrow={true} />
            </ErrorBoundary>,
        )
        expect(screen.getByText('Something went wrong')).toBeInTheDocument()
        expect(screen.getByText('Test error')).toBeInTheDocument()
        expect(screen.getByText(/Reload page/)).toBeInTheDocument()
    })
})

describe('SectionErrorBoundary', () => {
    it('renders children normally', () => {
        render(
            <SectionErrorBoundary>
                <div>Section content</div>
            </SectionErrorBoundary>,
        )
        expect(screen.getByText('Section content')).toBeInTheDocument()
    })

    it('shows error card with custom title', () => {
        render(
            <SectionErrorBoundary title="Custom Section">
                <ThrowingComponent shouldThrow={true} />
            </SectionErrorBoundary>,
        )
        expect(screen.getByText('Custom Section')).toBeInTheDocument()
        expect(screen.getByText('Test error')).toBeInTheDocument()
        expect(screen.getByText(/Retry/)).toBeInTheDocument()
    })

    it('shows default title when no title prop provided', () => {
        render(
            <SectionErrorBoundary>
                <ThrowingComponent shouldThrow={true} />
            </SectionErrorBoundary>,
        )
        expect(screen.getByText('Section unavailable')).toBeInTheDocument()
    })

    it('retry button resets error and re-renders children', () => {
        render(
            <SectionErrorBoundary>
                <ThrowOnceComponent />
            </SectionErrorBoundary>,
        )

        // First render throws, error UI should be shown
        expect(screen.getByText('First render error')).toBeInTheDocument()

        // Click Retry — component re-mounts and succeeds on second render
        fireEvent.click(screen.getByText(/Retry/))

        expect(screen.getByText('Recovered content')).toBeInTheDocument()
    })
})

describe('PageLoader', () => {
    it('renders spinner', () => {
        const { container } = render(<PageLoader />)
        // The spinner is a div with animation style inside the outer container div
        const spinnerDiv = container.querySelector('div > div')
        expect(spinnerDiv).toBeInTheDocument()
        expect(spinnerDiv).toHaveStyle({ borderRadius: '50%' })
    })
})
