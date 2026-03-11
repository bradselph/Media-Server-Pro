import { useState } from 'react'
import { SubTabs } from './helpers'
import { ContentReviewTab } from './ContentReviewTab'
import { CategorizerTab } from './CategorizerTab'
import { DiscoveryTab } from './DiscoveryTab'

export { ContentReviewTab } from './ContentReviewTab'
export { CategorizerTab } from './CategorizerTab'
export { DiscoveryTab } from './DiscoveryTab'
export { HuggingFaceTab } from './HuggingFaceTab'

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
