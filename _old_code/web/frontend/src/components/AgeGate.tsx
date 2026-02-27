import {useCallback, useState} from 'react'
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query'
import {ageGateApi} from '@/api/endpoints'
import '@/styles/agegate.css'

/**
 * AgeGateProvider wraps the entire app.
 * When the server has age gate enabled and the visitor is not verified,
 * this renders a full-screen overlay that blocks access to the underlying content.
 * After the visitor confirms they are 18+, the overlay is dismissed and the
 * backend records their consent via cookie + IP cache.
 */
export function AgeGateProvider({children}: { children: React.ReactNode }) {
    const queryClient = useQueryClient()
    const [dismissed, setDismissed] = useState(false)

    const {data, isLoading} = useQuery({
        queryKey: ['age-gate-status'],
        queryFn: () => ageGateApi.getStatus(),
        // Fail open — if the status endpoint is unreachable, don't block the user
        retry: false,
        staleTime: 5 * 60 * 1000,
    })

    const mutation = useMutation({
        mutationFn: () => ageGateApi.verify(),
        onSuccess: () => {
            // Invalidate status so subsequent checks reflect the verified state
            queryClient.invalidateQueries({queryKey: ['age-gate-status']})
            setDismissed(true)
        },
    })

    const handleConfirm = useCallback(() => {
        mutation.mutate()
    }, [mutation])

    const handleDecline = useCallback(() => {
        // Redirect to a neutral page (e.g. a search engine) when the user declines
        window.location.href = 'https://www.google.com'
    }, [])

    // Determine whether to show the gate
    const showGate = !isLoading && !dismissed && data?.enabled === true && data?.verified === false

    if (!showGate) {
        return <>{children}</>
    }

    return (
        <>
            {/* Render children underneath so the DOM is ready — gate sits on top */}
            <div aria-hidden="true" style={{visibility: 'hidden', pointerEvents: 'none'}}>
                {children}
            </div>

            <div className="age-gate-overlay" role="dialog" aria-modal="true" aria-labelledby="age-gate-title">
                <div className="age-gate-card">
                    <div className="age-gate-badge">18+</div>

                    <h1 id="age-gate-title" className="age-gate-title">
                        Age Verification Required
                    </h1>

                    <p className="age-gate-body">
                        This website contains adult content intended for viewers aged 18 and over.
                        You must confirm your age to continue.
                    </p>

                    <p className="age-gate-sub">
                        By clicking <strong>I am 18 or older</strong> you confirm that you are of
                        legal age in your jurisdiction and agree to view age-restricted content.
                    </p>

                    <div className="age-gate-actions">
                        <button
                            className="age-gate-btn age-gate-btn--confirm"
                            onClick={handleConfirm}
                            disabled={mutation.isPending}
                        >
                            {mutation.isPending ? 'Verifying…' : 'I am 18 or older'}
                        </button>

                        <button
                            className="age-gate-btn age-gate-btn--decline"
                            onClick={handleDecline}
                        >
                            I am under 18 — leave
                        </button>
                    </div>

                    {mutation.isError && (
                        <p className="age-gate-error">
                            Verification failed. Please try again.
                        </p>
                    )}
                </div>
            </div>
        </>
    )
}
