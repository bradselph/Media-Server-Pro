import {useState, useRef, useEffect, useCallback} from 'react'
import type {HLSQuality} from '@/hooks/useHLS'
import {formatBitrate} from '@/hooks/useHLS'
type SettingsView = 'main' | 'quality' | 'speed'
interface PlayerSettingsPanelProps {
/** Available HLS qualities (empty if HLS not active) */
qualities: HLSQuality[]
/** Current selected quality index (-1 = auto) */
currentQuality: number
/** Level chosen by ABR when in auto mode */
autoLevel: number
/** Callback to change quality */
onSelectQuality: (index: number) => void
/** Current playback speed */
playbackRate: number
/** Callback to change speed */
onSetSpeed: (speed: number) => void
/** Whether loop is enabled */
isLooping: boolean
/** Toggle loop */
onToggleLoop: () => void
/** Whether PiP is available (video only) */
showPiP?: boolean
/** PiP handler */
onPiP?: () => void
/** Current bandwidth estimate (bps) */
bandwidth?: number
/** Close the panel */
onClose: () => void
}
const SPEEDS = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
export function PlayerSettingsPanel({
qualities,
currentQuality,
autoLevel,
onSelectQuality,
playbackRate,
onSetSpeed,
isLooping,
onToggleLoop,
showPiP,
onPiP,
bandwidth,
onClose,
}: PlayerSettingsPanelProps) {
const [view, setView] = useState<SettingsView>('main')
const panelRef = useRef<HTMLDivElement>(null)
// Close on outside click
useEffect(() => {
function handleClick(e: MouseEvent) {
if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
onClose()
}
}
document.addEventListener('mousedown', handleClick)
return () => document.removeEventListener('mousedown', handleClick)
}, [onClose])
const handleSpeedSelect = useCallback((speed: number) => {
onSetSpeed(speed)
setView('main')
}, [onSetSpeed])
const handleQualitySelect = useCallback((index: number) => {
onSelectQuality(index)
setView('main')
}, [onSelectQuality])
// Derive display labels
const currentQualityLabel = currentQuality === -1
? 'Auto' + (autoLevel >= 0 && qualities[autoLevel] ? ` (${qualities[autoLevel].name})` : '')
: qualities.find(q => q.index === currentQuality)?.name ?? 'Auto'
const speedLabel = playbackRate === 1 ? 'Normal' : `${playbackRate}x`
return (
<div className="settings-panel" ref={panelRef} onClick={e => e.stopPropagation()}>
{view === 'main' && (
<div className="settings-panel__menu">
{/* Quality */}
{qualities.length > 0 && (
<button
className="settings-panel__item"
onClick={() => setView('quality')}
>
<span className="settings-panel__item-left">
<i className="bi bi-badge-hd"/>
<span>Quality</span>
</span>
<span className="settings-panel__item-right">
<span className="settings-panel__value">{currentQualityLabel}</span>
<i className="bi bi-chevron-right"/>
</span>
</button>
)}
{/* Speed */}
<button
className="settings-panel__item"
onClick={() => setView('speed')}
>
<span className="settings-panel__item-left">
<i className="bi bi-speedometer2"/>
<span>Speed</span>
</span>
<span className="settings-panel__item-right">
<span className="settings-panel__value">{speedLabel}</span>
<i className="bi bi-chevron-right"/>
</span>
</button>
{/* Loop */}
<button
className="settings-panel__item"
onClick={onToggleLoop}
>
<span className="settings-panel__item-left">
<i className="bi bi-repeat"/>
<span>Loop</span>
</span>
<span className="settings-panel__item-right">
<span className={`settings-panel__toggle ${isLooping ? 'settings-panel__toggle--on' : ''}`}>
<span className="settings-panel__toggle-knob"/>
</span>
</span>
</button>
{/* PiP */}
{showPiP && onPiP && (
<button
className="settings-panel__item"
onClick={() => { onPiP(); onClose() }}
>
<span className="settings-panel__item-left">
<i className="bi bi-pip"/>
<span>Picture-in-Picture</span>
</span>
<span className="settings-panel__item-right">
<i className="bi bi-box-arrow-up-right" style={{fontSize: 12, opacity: 0.6}}/>
</span>
</button>
)}
</div>
)}
{view === 'quality' && (
<div className="settings-panel__menu">
<button
className="settings-panel__back"
onClick={() => setView('main')}
>
<i className="bi bi-chevron-left"/>
<span>Quality</span>
</button>
<div className="settings-panel__divider"/>
{/* Auto option */}
<button
className={`settings-panel__item ${currentQuality === -1 ? 'settings-panel__item--active' : ''}`}
onClick={() => handleQualitySelect(-1)}
>
<span className="settings-panel__item-left">
{currentQuality === -1 && <i className="bi bi-check-lg"/>}
<span>Auto</span>
{currentQuality === -1 && autoLevel >= 0 && qualities[autoLevel] && (
<span className="settings-panel__auto-badge">
{qualities[autoLevel].name}
</span>
)}
</span>
</button>
{/* Individual qualities — highest first */}
{[...qualities].sort((a, b) => b.height - a.height).map(q => (
<button
key={q.index}
className={`settings-panel__item ${currentQuality === q.index ? 'settings-panel__item--active' : ''}`}
onClick={() => handleQualitySelect(q.index)}
>
<span className="settings-panel__item-left">
{currentQuality === q.index && <i className="bi bi-check-lg"/>}
<span>{q.name}</span>
{q.height >= 1080 && (
<span className="settings-panel__hd-badge">HD</span>
)}
</span>
<span className="settings-panel__item-right">
<span className="settings-panel__bitrate">{formatBitrate(q.bitrate)}</span>
</span>
</button>
))}
{bandwidth > 0 && (
<>
<div className="settings-panel__divider"/>
<div className="settings-panel__bandwidth">
<i className="bi bi-speedometer"/>
<span>Connection: {formatBitrate(bandwidth)}</span>
</div>
</>
)}
</div>
)}
{view === 'speed' && (
<div className="settings-panel__menu">
<button
className="settings-panel__back"
onClick={() => setView('main')}
>
<i className="bi bi-chevron-left"/>
<span>Speed</span>
</button>
<div className="settings-panel__divider"/>
{SPEEDS.map(speed => (
<button
key={speed}
className={`settings-panel__item ${playbackRate === speed ? 'settings-panel__item--active' : ''}`}
onClick={() => handleSpeedSelect(speed)}
>
<span className="settings-panel__item-left">
{playbackRate === speed && <i className="bi bi-check-lg"/>}
<span>{speed === 1 ? 'Normal' : `${speed}x`}</span>
</span>
</button>
))}
</div>
)}
</div>
)
}