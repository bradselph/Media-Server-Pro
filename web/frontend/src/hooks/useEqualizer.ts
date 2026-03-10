import type {RefObject} from 'react'
import {useCallback, useEffect, useRef, useState} from 'react'

const FREQ_10 = [31.25, 62.5, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
const LABELS_10 = ['31Hz', '62Hz', '125Hz', '250Hz', '500Hz', '1kHz', '2kHz', '4kHz', '8kHz', '16kHz']

const FREQ_31 = [
    20, 25, 31.5, 40, 50, 63, 80, 100, 125, 160, 200, 250, 315, 400, 500,
    630, 800, 1000, 1250, 1600, 2000, 2500, 3150, 4000, 5000, 6300, 8000,
    10000, 12500, 16000, 20000,
]
const LABELS_31 = [
    '20', '25', '31', '40', '50', '63', '80', '100', '125', '160', '200', '250',
    '315', '400', '500', '630', '800', '1k', '1.25k', '1.6k', '2k', '2.5k',
    '3.15k', '4k', '5k', '6.3k', '8k', '10k', '12.5k', '16k', '20k',
]

const PRESETS_10: Record<string, number[]> = {
    flat: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
    rock: [5, 4, 3, 1, -1, -1, 2, 3, 4, 4],
    pop: [-1, 1, 3, 4, 3, 1, -1, 1, 2, 2],
    jazz: [3, 2, 1, 2, -1, -1, 0, 1, 2, 3],
    classical: [4, 3, 2, 1, -1, -1, 0, 2, 3, 4],
    bass: [6, 5, 4, 3, 1, 0, 0, 0, 0, 0],
    treble: [0, 0, 0, 0, 0, 1, 2, 4, 5, 6],
    vocal: [-2, -1, 0, 2, 4, 4, 3, 1, 0, -1],
    electronic: [4, 3, 2, 0, -2, -1, 1, 3, 4, 5],
    acoustic: [3, 2, 1, 1, 0, 0, 1, 2, 2, 3],
    hiphop: [5, 4, 3, 1, 0, 0, 1, 0, 1, 2],
    metal: [5, 4, 2, 0, -2, -1, 2, 4, 5, 5],
    lounge: [2, 1, 0, 0, -1, 0, 0, 1, 1, 2],
    dance: [4, 3, 2, 0, -1, 0, 2, 3, 4, 4],
    reggae: [3, 2, 0, -1, 0, 1, 2, 2, 1, 1],
}

const PRESETS_31: Record<string, number[]> = {
    flat: Array(31).fill(0),
    rock: [4, 4, 5, 5, 4, 3, 3, 2, 1, 0, -1, -1, -1, -1, -1, -1, 0, 1, 2, 2, 3, 3, 3, 4, 4, 4, 4, 4, 3, 3, 2],
    pop: [-1, -1, 0, 1, 2, 3, 3, 4, 4, 3, 3, 2, 1, 0, -1, -1, -1, 0, 1, 1, 2, 2, 2, 1, 1, 1, 2, 2, 2, 1, 0],
    jazz: [3, 3, 3, 2, 2, 1, 1, 1, 0, 0, -1, -1, -1, -1, 0, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 2],
    classical: [4, 4, 3, 3, 2, 2, 1, 1, 0, 0, -1, -1, -1, -1, -1, 0, 0, 0, 1, 1, 2, 2, 2, 3, 3, 3, 3, 3, 4, 4, 3],
    bass: [6, 6, 5, 5, 5, 4, 4, 3, 3, 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
    treble: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 3, 4, 4, 5, 5, 5, 6, 6, 5],
    vocal: [-2, -2, -1, -1, -1, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 4, 3, 3, 2, 2, 1, 1, 0, 0, -1, -1, -1, 0, 0, -1, -1],
    electronic: [4, 4, 3, 3, 2, 2, 1, 0, 0, -1, -1, -2, -2, -1, -1, 0, 0, 1, 1, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5, 4, 3],
}

const STORAGE_KEY = 'media_streamer_settings'

interface StoredEQSettings {
    eqSettings?: number[]
    eqSettings31?: number[]
    eqPreset?: string
    eqBands?: '10' | '31'
}

interface CustomPreset {
    name: string
    values: number[]
    bands: '10' | '31'
    created: number
}

interface EQBand {
    frequency: number
    label: string
    gain: number
}

export interface UseEqualizerResult {
    bands: EQBand[]
    setBandGain: (index: number, gain: number) => void
    setPreset: (name: string) => void
    currentPreset: string
    presets: string[]
    currentMode: '10' | '31'
    switchMode: (mode: '10' | '31') => void
    customPresets: Record<string, CustomPreset>
    saveCustomPreset: (name: string) => void
    deleteCustomPreset: (name: string) => void
    analyser: AnalyserNode | null
}

function loadSettings(): StoredEQSettings {
    try {
        const raw = localStorage.getItem(STORAGE_KEY)
        return raw ? JSON.parse(raw) : {}
    } catch {
        return {}
    }
}

function saveSettings(settings: StoredEQSettings) {
    try {
        const existing = loadSettings()
        localStorage.setItem(STORAGE_KEY, JSON.stringify({...existing, ...settings}))
    } catch {
        // Storage may be full
    }
}

function loadCustomPresets(): Record<string, CustomPreset> {
    try {
        const raw = localStorage.getItem('custom_eq_presets')
        return raw ? JSON.parse(raw) : {}
    } catch {
        return {}
    }
}

function saveCustomPresets(presets: Record<string, CustomPreset>) {
    try {
        localStorage.setItem('custom_eq_presets', JSON.stringify(presets))
    } catch {
        // Storage may be full
    }
}

export function useEqualizer(
    audioRef: RefObject<HTMLAudioElement | null>,
    isReady: boolean,
): UseEqualizerResult {
    const audioCtxRef = useRef<AudioContext | null>(null)
    const sourceRef = useRef<MediaElementAudioSourceNode | null>(null)
    const filtersRef = useRef<BiquadFilterNode[]>([])
    const analyserRef = useRef<AnalyserNode | null>(null)
    const connectedRef = useRef(false)
    const audioElementRef = useRef<HTMLAudioElement | null>(null)

    const [bands, setBands] = useState<EQBand[]>([])
    const [currentPreset, setCurrentPreset] = useState(() => loadSettings().eqPreset || 'flat')
    const [currentMode, setCurrentMode] = useState<'10' | '31'>(() => loadSettings().eqBands || '10')
    const [customPresets, setCustomPresets] = useState<Record<string, CustomPreset>>(loadCustomPresets)
    const [analyser, setAnalyser] = useState<AnalyserNode | null>(null)

    // Initialize audio context and source (once per audio element)
    const ensureAudioContext = useCallback(() => {
        const audioElement = audioElementRef.current
        if (!audioElement) return null
        if (!audioCtxRef.current) {
            audioCtxRef.current = new AudioContext()
        }
        if (!sourceRef.current) {
            sourceRef.current = audioCtxRef.current.createMediaElementSource(audioElement)
            connectedRef.current = false
        }
        if (!analyserRef.current) {
            analyserRef.current = audioCtxRef.current.createAnalyser()
            analyserRef.current.fftSize = 256
            analyserRef.current.smoothingTimeConstant = 0.8
            setAnalyser(analyserRef.current)
        }
        return audioCtxRef.current
    }, [])

    // Build EQ filter chain
    const buildFilters = useCallback((mode: '10' | '31', gains?: number[]) => {
        const ctx = ensureAudioContext()
        if (!ctx) return

        // Disconnect old filters
        filtersRef.current.forEach(f => {
            try {
                f.disconnect()
            } catch { /* ignore */
            }
        })
        if (sourceRef.current) {
            try {
                sourceRef.current.disconnect()
            } catch { /* ignore */
            }
        }
        connectedRef.current = false

        const freqs = mode === '31' ? FREQ_31 : FREQ_10
        const labels = mode === '31' ? LABELS_31 : LABELS_10
        const savedSettings = loadSettings()
        const savedGains = gains || (mode === '31' ? savedSettings.eqSettings31 : savedSettings.eqSettings) || Array(freqs.length).fill(0)

        const filters: BiquadFilterNode[] = freqs.map((freq, i) => {
            const filter = ctx.createBiquadFilter()
            if (i === 0) filter.type = 'lowshelf'
            else if (i === freqs.length - 1) filter.type = 'highshelf'
            else filter.type = 'peaking'
            filter.frequency.value = freq
            filter.Q.value = mode === '31' ? 2 : 1
            filter.gain.value = savedGains[i] || 0
            return filter
        })

        // Chain: source → filters → analyser → destination
        if (sourceRef.current && analyserRef.current) {
            sourceRef.current.connect(filters[0])
            for (let i = 0; i < filters.length - 1; i++) {
                filters[i].connect(filters[i + 1])
            }
            filters[filters.length - 1].connect(analyserRef.current)
            analyserRef.current.connect(ctx.destination)
            connectedRef.current = true
        }

        filtersRef.current = filters
        setBands(freqs.map((freq, i) => ({
            frequency: freq,
            label: labels[i],
            gain: filters[i].gain.value,
        })))
    }, [ensureAudioContext])

    // Initialize on audio element change (read ref only inside effect to satisfy React rules)
    useEffect(() => {
        const audioElement = isReady ? audioRef.current : null
        if (!audioElement) return

        audioElementRef.current = audioElement
        const settings = loadSettings()
        const mode = settings.eqBands || '10'
        queueMicrotask(() => buildFilters(mode))

        // Resume audio context on play
        const handlePlay = () => {
            if (audioCtxRef.current?.state === 'suspended') {
                audioCtxRef.current.resume()
            }
        }
        audioElement.addEventListener('play', handlePlay)

        return () => {
            audioElement.removeEventListener('play', handlePlay)
            // Close the AudioContext to free resources when the audio element changes
            if (audioCtxRef.current) {
                audioCtxRef.current.close().catch(() => {
                })
                audioCtxRef.current = null
            }
            sourceRef.current = null
            analyserRef.current = null
            connectedRef.current = false
            filtersRef.current = []
            setAnalyser(null)
            audioElementRef.current = null
        }
    }, [audioRef, isReady, buildFilters])

    const setBandGain = useCallback((index: number, gain: number) => {
        const clamped = Math.max(-20, Math.min(20, gain))
        if (filtersRef.current[index]) {
            filtersRef.current[index].gain.value = clamped
        }
        setBands(prev => prev.map((b, i) => i === index ? {...b, gain: clamped} : b))
        setCurrentPreset('custom')

        // Persist
        const gains = filtersRef.current.map(f => f.gain.value)
        const key = currentMode === '31' ? 'eqSettings31' : 'eqSettings'
        saveSettings({[key]: gains, eqPreset: 'custom'})
    }, [currentMode])

    const setPreset = useCallback((name: string) => {
        const presetMap = currentMode === '31' ? PRESETS_31 : PRESETS_10
        let values: number[]

        if (presetMap[name]) {
            values = presetMap[name]
        } else if (customPresets[name]) {
            values = customPresets[name].values
        } else {
            values = Array(currentMode === '31' ? 31 : 10).fill(0)
        }

        filtersRef.current.forEach((f, i) => {
            f.gain.value = values[i] || 0
        })
        setBands(prev => prev.map((b, i) => ({...b, gain: values[i] || 0})))
        setCurrentPreset(name)

        const key = currentMode === '31' ? 'eqSettings31' : 'eqSettings'
        saveSettings({[key]: values, eqPreset: name})
    }, [currentMode, customPresets])

    const switchMode = useCallback((mode: '10' | '31') => {
        setCurrentMode(mode)
        saveSettings({eqBands: mode})
        buildFilters(mode)
        // Restore the saved preset label for this mode; fall back to 'flat'
        const saved = loadSettings()
        setCurrentPreset(saved.eqPreset || 'flat')
    }, [buildFilters])

    const saveCustomPreset = useCallback((name: string) => {
        const values = filtersRef.current.map(f => f.gain.value)
        const preset: CustomPreset = {
            name,
            values,
            bands: currentMode,
            created: Date.now(),
        }
        const updated = {...customPresets, [name]: preset}
        setCustomPresets(updated)
        saveCustomPresets(updated)
        setCurrentPreset(name)
    }, [currentMode, customPresets])

    const deleteCustomPreset = useCallback((name: string) => {
        const updated = {...customPresets}
        delete updated[name]
        setCustomPresets(updated)
        saveCustomPresets(updated)
        if (currentPreset === name) setCurrentPreset('flat')
    }, [customPresets, currentPreset])

    const presets = [
        ...Object.keys(currentMode === '31' ? PRESETS_31 : PRESETS_10),
        ...Object.keys(customPresets).filter(k => customPresets[k].bands === currentMode),
    ]

    return {
        bands,
        setBandGain,
        setPreset,
        currentPreset,
        presets,
        currentMode,
        switchMode,
        customPresets,
        saveCustomPreset,
        deleteCustomPreset,
        analyser,
    }
}
