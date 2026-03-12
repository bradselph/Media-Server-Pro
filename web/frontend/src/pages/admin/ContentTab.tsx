import { useState } from 'react'
import { SubTabs } from './helpers'
import { ContentReviewTab } from './ContentReviewTab'
import { CategorizerTab } from './CategorizerTab'
import { DiscoveryTab } from './DiscoveryTab'

export { ContentReviewTab } from './ContentReviewTab'
export { CategorizerTab } from './CategorizerTab'
export { DiscoveryTab } from './DiscoveryTab'
export { HuggingFaceTab } from './HuggingFaceTab'

// TODO: Dead code — `ContentTab` component is never imported or rendered anywhere.
// WHY: MediaTab.tsx imports the individual sub-tabs (ContentReviewTab, CategorizerTab,
// DiscoveryTab, HuggingFaceTab) directly from this file's re-exports, but never uses
// the `ContentTab` composite component itself. This component was likely superseded
// when the admin layout was restructured to nest content sub-tabs under MediaTab.
// FIX: Remove this dead component. Keep the re-exports above since MediaTab.tsx
// depends on them, or move those re-exports into a barrel index if preferred.
export function ContentTab() {
    const [sub, setSub] = useState('review')
    return (
        <>
            <SubTabs
                items={[
                    { id: 'review', label: 'Review' },
                    { id: 'categorizer', label: 'Categorizer' },
                    { id: 'discovery', label: 'Discovery' },
                ]}
                active={sub}
                onChange={setSub}
            />
            {sub === 'review' && <ContentReviewTab />}
            {sub === 'categorizer' && <CategorizerTab />}
            {sub === 'discovery' && <DiscoveryTab />}
        </>
    )
}
