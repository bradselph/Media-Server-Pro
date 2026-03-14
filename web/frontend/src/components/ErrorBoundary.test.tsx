import { render, screen, fireEvent } from '@testing-library/react'
import { ErrorBoundary, SectionErrorBoundary, PageLoader } from './ErrorBoundary'

function ThrowingComponent({ shouldThrow }: { shouldThrow: boolean }) {
    if (shouldThrow) throw new Error('Test error')
    return <div>Content loaded</div>
}

// A component controlled by an external ref that determines whether to throw.
// The flag is toggled externally after the error boundary catches the first throw,
// so that after clicking Retry the component renders successfully.
const throwFlag = { current: true }

function ConditionalThrowComponent() {
    if (throwFlag.current) throw new Error('First render error')
    return <div>Recovered content</div>
}

// Suppress console.error noise from React error boundaries
let consoleSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    throwFlag.current = true
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
                <ConditionalThrowComponent />
            </SectionErrorBoundary>,
        )

        // Error boundary caught the throw, error UI should be shown
        expect(screen.getByText('First render error')).toBeInTheDocument()

        // Flip the flag so the component succeeds on next mount
        throwFlag.current = false

        // Click Retry — boundary resets state, child re-mounts successfully
        fireEvent.click(screen.getByText(/Retry/))

        expect(screen.getByText('Recovered content')).toBeInTheDocument()
    })
})

describe('PageLoader', () => {
    it('renders spinner', () => {
        const { container } = render(<PageLoader />)
        // The outer div is the full-page container; inside it is the spinner div
        const spinnerDiv = container.querySelector('div > div')
        expect(spinnerDiv).toBeInTheDocument()
        // Verify the spin keyframe animation style tag is injected
        const styleTag = container.querySelector('style')
        expect(styleTag).toBeInTheDocument()
        expect(styleTag!.textContent).toContain('@keyframes spin')
    })
})
