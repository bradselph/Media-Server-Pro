/** Keyboard shortcut handler for the media player. */

import { useEffect, useRef } from 'react'

/** Domain constants and type for player shortcut keys (avoids primitive string obsession). */
const PlayerKeys = {
    PLAY_PAUSE: [' ', 'k', 'K'] as const,
    SEEK_BACK_CHARS: ['j', 'J'] as const,
    SEEK_FWD_CHARS: ['l', 'L'] as const,
    ARROW_LEFT: 'ArrowLeft',
    ARROW_RIGHT: 'ArrowRight',
    HOME: 'Home',
    END: 'End',
    VOLUME_UP: 'ArrowUp',
    VOLUME_DOWN: 'ArrowDown',
    MUTE_CHARS: ['m', 'M'] as const,
    FULLSCREEN_CHARS: ['f', 'F'] as const,
    THEATER_CHARS: ['t', 'T'] as const,
    FRAME_BACK: ',',
    FRAME_FWD: '.',
    SPEED_DOWN: '<',
    SPEED_UP: '>',
    ESCAPE: 'Escape',
} as const

/** Union type for keys that trigger player shortcuts (replaces raw string). */
type PlayerShortcutKey =
    | (typeof PlayerKeys.PLAY_PAUSE)[number]
    | (typeof PlayerKeys.SEEK_BACK_CHARS)[number]
    | (typeof PlayerKeys.SEEK_FWD_CHARS)[number]
    | typeof PlayerKeys.ARROW_LEFT
    | typeof PlayerKeys.ARROW_RIGHT
    | typeof PlayerKeys.HOME
    | typeof PlayerKeys.END
    | typeof PlayerKeys.VOLUME_UP
    | typeof PlayerKeys.VOLUME_DOWN
    | (typeof PlayerKeys.MUTE_CHARS)[number]
    | (typeof PlayerKeys.FULLSCREEN_CHARS)[number]
    | (typeof PlayerKeys.THEATER_CHARS)[number]
    | typeof PlayerKeys.FRAME_BACK
    | typeof PlayerKeys.FRAME_FWD
    | typeof PlayerKeys.SPEED_DOWN
    | typeof PlayerKeys.SPEED_UP
    | typeof PlayerKeys.ESCAPE
    | '0' | '1' | '2' | '3' | '4' | '5' | '6' | '7' | '8' | '9'

const INPUT_LIKE_TAGS = ['INPUT', 'TEXTAREA', 'SELECT'] as const

export type PlayerKeyHandlers = {
    getActiveEl: () => HTMLMediaElement | null
    togglePlay: () => void
    setSpeed: (speed: number) => void
    setVolume: (v: number) => void
    setIsMuted: (m: boolean) => void
    handleFullscreen: () => void
    setTheaterMode: (fn: (t: boolean) => boolean) => void
    setShowSettings: (fn: (s: boolean) => boolean) => void
    showSettings: boolean
    playbackRate: number
}

function isInputField(target: HTMLElement): boolean {
    return (INPUT_LIKE_TAGS as readonly string[]).includes(target.tagName)
}

function isPlayPauseKey(key: string): boolean {
    return (PlayerKeys.PLAY_PAUSE as readonly string[]).includes(key)
}

function isSeekKey(key: string): boolean {
    return (
        (PlayerKeys.SEEK_BACK_CHARS as readonly string[]).includes(key) ||
        (PlayerKeys.SEEK_FWD_CHARS as readonly string[]).includes(key) ||
        key === PlayerKeys.ARROW_LEFT ||
        key === PlayerKeys.ARROW_RIGHT ||
        key === PlayerKeys.HOME ||
        key === PlayerKeys.END
    )
}

function isVolumeKey(key: string): boolean {
    return (
        key === PlayerKeys.VOLUME_UP ||
        key === PlayerKeys.VOLUME_DOWN ||
        (PlayerKeys.MUTE_CHARS as readonly string[]).includes(key)
    )
}

function applyPlayPauseKey(el: HTMLMediaElement | null, _handlers: PlayerKeyHandlers): void {
    if (!el) return
    if (el.paused) el.play().catch(() => {})
    else el.pause()
}

function applySeekKey(key: PlayerShortcutKey, el: HTMLMediaElement | null): void {
    if (!el) return
    if (key === PlayerKeys.HOME) el.currentTime = 0
    else if (key === PlayerKeys.END) el.currentTime = el.duration
    else if ((PlayerKeys.SEEK_BACK_CHARS as readonly string[]).includes(key)) el.currentTime = Math.max(0, el.currentTime - 10)
    else if ((PlayerKeys.SEEK_FWD_CHARS as readonly string[]).includes(key)) el.currentTime = Math.min(el.duration, el.currentTime + 10)
    else if (key === PlayerKeys.ARROW_LEFT) el.currentTime = Math.max(0, el.currentTime - 5)
    else if (key === PlayerKeys.ARROW_RIGHT) el.currentTime = Math.min(el.duration, el.currentTime + 5)
}

function applyVolumeKey(key: PlayerShortcutKey, el: HTMLMediaElement | null, handlers: PlayerKeyHandlers): void {
    if (!el) return
    if (key === PlayerKeys.VOLUME_UP) {
        el.volume = Math.min(1, el.volume + 0.05)
        handlers.setVolume(el.volume)
    } else if (key === PlayerKeys.VOLUME_DOWN) {
        el.volume = Math.max(0, el.volume - 0.05)
        handlers.setVolume(el.volume)
    } else if ((PlayerKeys.MUTE_CHARS as readonly string[]).includes(key)) {
        el.muted = !el.muted
        handlers.setIsMuted(el.muted)
    }
}

function applyFrameStepKey(key: PlayerShortcutKey, el: HTMLMediaElement | null): void {
    if (!el || !el.paused) return
    if (key === PlayerKeys.FRAME_BACK) el.currentTime = Math.max(0, el.currentTime - 1 / 30)
    else if (key === PlayerKeys.FRAME_FWD) el.currentTime = Math.min(el.duration, el.currentTime + 1 / 30)
}

function isDigitKey(key: string): key is PlayerShortcutKey {
    return key.length === 1 && key >= '0' && key <= '9'
}

type KeyHandler = (key: PlayerShortcutKey, el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent) => boolean

function handlePlayPause(key: PlayerShortcutKey, el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (!isPlayPauseKey(key)) return false
    e.preventDefault()
    applyPlayPauseKey(el, handlers)
    return true
}

function handleSeek(key: PlayerShortcutKey, el: HTMLMediaElement | null, _handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (!isSeekKey(key)) return false
    e.preventDefault()
    applySeekKey(key, el)
    return true
}

function handleVolume(key: PlayerShortcutKey, el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (!isVolumeKey(key)) return false
    e.preventDefault()
    applyVolumeKey(key, el, handlers)
    return true
}

function handleFullscreen(key: PlayerShortcutKey, _el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (!(PlayerKeys.FULLSCREEN_CHARS as readonly string[]).includes(key)) return false
    e.preventDefault()
    handlers.handleFullscreen()
    return true
}

function handleTheaterMode(key: PlayerShortcutKey, _el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (!(PlayerKeys.THEATER_CHARS as readonly string[]).includes(key)) return false
    e.preventDefault()
    handlers.setTheaterMode(t => !t)
    return true
}

function handleSettingsEscape(key: PlayerShortcutKey, _el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (key !== PlayerKeys.ESCAPE || !handlers.showSettings) return false
    e.preventDefault()
    handlers.setShowSettings(() => false)
    return true
}

function handleDigitSeek(key: PlayerShortcutKey, el: HTMLMediaElement | null, _handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (!isDigitKey(key)) return false
    e.preventDefault()
    if (el && el.duration) el.currentTime = (parseInt(key, 10) / 10) * el.duration
    return true
}

function handleFrameStep(key: PlayerShortcutKey, el: HTMLMediaElement | null, _handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (key !== PlayerKeys.FRAME_BACK && key !== PlayerKeys.FRAME_FWD) return false
    applyFrameStepKey(key, el)
    if (el?.paused) e.preventDefault()
    return true
}

function handleSpeedDown(key: PlayerShortcutKey, _el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (key !== PlayerKeys.SPEED_DOWN) return false
    e.preventDefault()
    handlers.setSpeed(Math.max(0.25, handlers.playbackRate - 0.25))
    return true
}

function handleSpeedUp(key: PlayerShortcutKey, _el: HTMLMediaElement | null, handlers: PlayerKeyHandlers, e: KeyboardEvent): boolean {
    if (key !== PlayerKeys.SPEED_UP) return false
    e.preventDefault()
    handlers.setSpeed(Math.min(2, handlers.playbackRate + 0.25))
    return true
}

const KEY_HANDLERS: KeyHandler[] = [
    handlePlayPause,
    handleSeek,
    handleVolume,
    handleFullscreen,
    handleTheaterMode,
    handleSettingsEscape,
    handleDigitSeek,
    handleFrameStep,
    handleSpeedDown,
    handleSpeedUp,
]

/** Attaches global keydown listener for player shortcuts. Extracted to reduce main hook complexity. */
export function usePlayerKeyboard(handlers: PlayerKeyHandlers): void {
    const ref = useRef(handlers)
    useEffect(() => {
        ref.current = handlers
    })
    useEffect(() => {
        const onKeyDown = (e: KeyboardEvent) => handlePlayerKeyDown(e, ref.current)
        document.addEventListener('keydown', onKeyDown)
        return () => document.removeEventListener('keydown', onKeyDown)
    }, [])
}

export function handlePlayerKeyDown(e: KeyboardEvent, handlers: PlayerKeyHandlers): void {
    const target = e.target as HTMLElement
    if (isInputField(target)) return

    const el = handlers.getActiveEl()
    const key = e.key as PlayerShortcutKey

    for (const fn of KEY_HANDLERS) {
        if (fn(key, el, handlers, e)) return
    }
}
