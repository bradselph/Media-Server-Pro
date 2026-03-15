import { render, screen, fireEvent, act } from '@testing-library/react'
import { ToastProvider } from './Toast'
import { useToast } from '@/hooks/useToast'

// Test consumer component that triggers a toast on button click
function TestConsumer({ message = 'Test message', type = 'success' as const }) {
    const { showToast } = useToast()
    return <button onClick={() => showToast(message, type)}>Show Toast</button>
}

// Component that shows multiple toasts
function MultiToastConsumer() {
    const { showToast } = useToast()
    return (
        <div>
            <button onClick={() => {
                showToast('First toast', 'success')
                showToast('Second toast', 'error')
            }}>Show Multiple</button>
        </div>
    )
}

describe('ToastProvider', () => {
    it('renders children', () => {
        render(
            <ToastProvider>
                <div>App content</div>
            </ToastProvider>,
        )
        expect(screen.getByText('App content')).toBeInTheDocument()
    })

    it('showToast displays a toast message', () => {
        render(
            <ToastProvider>
                <TestConsumer message="Hello toast" />
            </ToastProvider>,
        )

        fireEvent.click(screen.getByText('Show Toast'))

        expect(screen.getByText('Hello toast')).toBeInTheDocument()
    })

    it('toast auto-dismisses after timeout', () => {
        vi.useFakeTimers()

        render(
            <ToastProvider>
                <TestConsumer message="Disappearing toast" />
            </ToastProvider>,
        )

        fireEvent.click(screen.getByText('Show Toast'))
        expect(screen.getByText('Disappearing toast')).toBeInTheDocument()

        // Advance past the 4000ms auto-dismiss timeout
        act(() => {
            vi.advanceTimersByTime(4000)
        })

        expect(screen.queryByText('Disappearing toast')).not.toBeInTheDocument()

        vi.useRealTimers()
    })

    it('multiple toasts display simultaneously', () => {
        render(
            <ToastProvider>
                <MultiToastConsumer />
            </ToastProvider>,
        )

        fireEvent.click(screen.getByText('Show Multiple'))

        expect(screen.getByText('First toast')).toBeInTheDocument()
        expect(screen.getByText('Second toast')).toBeInTheDocument()
    })
})

describe('useToast', () => {
    it('throws outside provider', () => {
        // Suppress console.error from React catching the render error
        const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

        function BadConsumer() {
            useToast()
            return <div>Should not render</div>
        }

        expect(() => render(<BadConsumer />)).toThrow(
            'useToast must be used within ToastProvider',
        )

        consoleSpy.mockRestore()
    })
})
