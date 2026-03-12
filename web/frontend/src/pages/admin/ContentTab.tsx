import { useState } from 'react'
import { SubTabs } from './helpers'
import { ContentReviewTab } from './ContentReviewTab'
import { CategorizerTab } from './CategorizerTab'
import { DiscoveryTab } from './DiscoveryTab'

export { ContentReviewTab } from './ContentReviewTab'
export { CategorizerTab } from './CategorizerTab'
export { DiscoveryTab } from './DiscoveryTab'
export { HuggingFaceTab } from './HuggingFaceTab'

// TODO(feature-gap): Dead code — `ContentTab` is never imported or rendered. AdminPage.tsx
// has no "Content" top-level tab; MediaTab renders Review/Categorizer/Hugging Face/Discovery
// as sub-tabs and imports those components from this file's re-exports. Either remove
// ContentTab and keep only the re-exports, or add a top-level Content tab in AdminPage
// that renders <ContentTab /> so this composite is reachable.
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
