import type {useEqualizer} from '@/hooks/useEqualizer'
import '@/styles/equalizer.css'

type EqualizerProps = ReturnType<typeof useEqualizer> & {
    visible: boolean
    onClose: () => void
}

export function EqualizerPanel({
                                   visible,
                                   onClose,
                                   bands,
                                   setBandGain,
                                   setPreset,
                                   currentPreset,
                                   presets,
                                   currentMode,
                                   switchMode,
                                   saveCustomPreset,
                                   deleteCustomPreset,
                                   customPresets,
                               }: EqualizerProps) {
    if (!visible) return null

    function handleSavePreset() {
        const name = prompt('Preset name:')
        if (name?.trim()) saveCustomPreset(name.trim())
    }

    return (
        <div className="eq-panel">
            <div className="eq-panel__header">
                <h3>Equalizer</h3>
                <div className="eq-panel__mode">
                    <button
                        className={currentMode === '10' ? 'active' : ''}
                        onClick={() => switchMode('10')}
                    >
                        10-Band
                    </button>
                    <button
                        className={currentMode === '31' ? 'active' : ''}
                        onClick={() => switchMode('31')}
                    >
                        31-Band
                    </button>
                </div>
                <button className="eq-panel__close" onClick={onClose}>
                    <i className="bi bi-x-lg"/>
                </button>
            </div>

            <div className="eq-panel__presets">
                {presets.map(name => (
                    <button
                        key={name}
                        className={`eq-preset ${currentPreset === name ? 'eq-preset--active' : ''}`}
                        onClick={() => setPreset(name)}
                    >
                        {name}
                        {customPresets[name] && (
                            <span
                                className="eq-preset__delete"
                                onClick={e => {
                                    e.stopPropagation();
                                    deleteCustomPreset(name)
                                }}
                            >
                x
              </span>
                        )}
                    </button>
                ))}
                <button className="eq-preset eq-preset--save" onClick={handleSavePreset}>
                    + Save
                </button>
            </div>

            <div className={`eq-panel__sliders ${currentMode === '31' ? 'eq-panel__sliders--31' : ''}`}>
                {bands.map((band, i) => (
                    <div key={band.frequency} className="eq-band">
                        <input
                            type="range"
                            className="eq-band__slider"
                            min="-20"
                            max="20"
                            step="0.5"
                            value={band.gain}
                            onChange={e => setBandGain(i, parseFloat(e.target.value))}
                            orient="vertical"
                        />
                        <span className="eq-band__value">{band.gain > 0 ? '+' : ''}{band.gain.toFixed(0)}</span>
                        <span className="eq-band__label">{band.label}</span>
                    </div>
                ))}
            </div>
        </div>
    )
}
