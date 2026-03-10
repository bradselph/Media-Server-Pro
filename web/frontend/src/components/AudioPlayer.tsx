import {useCallback, useEffect, useRef, useState} from 'react'
import {getStreamUrl, usePlaybackStore} from '@/stores/playbackStore'
import {mediaApi} from '@/api/endpoints'
import {usePlaylistStore} from '@/stores/playlistStore'
import {useHLS} from '@/hooks/useHLS'
import {useMediaPosition} from '@/hooks/useMediaPosition'
import {useEqualizer} from '@/hooks/useEqualizer'
import '@/styles/audio-player.css'

interface AudioPlayerProps {
    onEqualizerToggle: () => void
    equalizerVisible: boolean
}

export function AudioPlayer({onEqualizerToggle, equalizerVisible}: AudioPlayerProps) {
    const audioRef = useRef<HTMLAudioElement>(null)
    const progressRef = useRef<HTMLDivElement>(null)
    const [audioReady, setAudioReady] = useState(false)

    const {
        isPlaying, currentMediaId, currentMediaTitle, currentMediaType,
        currentVolume, isMuted, currentTime, duration, playbackSpeed,
        hlsEnabled, hlsUrl,
        setPlaying, setVolume, toggleMute, setCurrentTime, setDuration,
        setPlaybackSpeed, stopPlayback,
    } = usePlaybackStore()

    const {playNext, playPrevious, currentPlaylist, currentIndex} = usePlaylistStore()

    // HLS hook
    const handleHLSFallback = useCallback(() => {
        usePlaybackStore.getState().setHLSUrl(null)
    }, [])

    const {qualities, currentQuality, autoLevel, selectQuality} = useHLS(
        audioRef,
        hlsEnabled ? hlsUrl : null,
        handleHLSFallback,
    )

    // Media position tracking
    const {resumeInfo, acceptResume, declineResume} = useMediaPosition(
        currentMediaId,
        audioRef.current,
    )

    // Equalizer — only connect once audio element exists (side-effect only)
    useEqualizer(audioRef, audioReady)

    // Mark audio ready after mount
    useEffect(() => {
        if (audioRef.current) setAudioReady(true)
    }, [])

    // Update audio source when media changes
    useEffect(() => {
        const audio = audioRef.current
        if (!audio || !currentMediaId) return

        if (!hlsEnabled) {
            audio.src = getStreamUrl(currentMediaId)
        }
        // HLS source is set by the useHLS hook
    }, [currentMediaId, hlsEnabled])

    // Sync play/pause state
    useEffect(() => {
        const audio = audioRef.current
        if (!audio || !currentMediaId) return

        if (isPlaying) {
            audio.play().catch(() => {
                setPlaying(false)
            })
        } else {
            audio.pause()
        }
    }, [isPlaying, currentMediaId, setPlaying])

    // Sync volume
    useEffect(() => {
        const audio = audioRef.current
        if (!audio) return
        audio.volume = isMuted ? 0 : currentVolume
    }, [currentVolume, isMuted])

    // Sync playback speed
    useEffect(() => {
        const audio = audioRef.current
        if (!audio) return
        audio.playbackRate = playbackSpeed
    }, [playbackSpeed])

    // Audio event handlers
    useEffect(() => {
        const audio = audioRef.current
        if (!audio) return

        const onTimeUpdate = () => setCurrentTime(audio.currentTime)
        const onDurationChange = () => setDuration(audio.duration || 0)
        const onPlay = () => setPlaying(true)
        const onPause = () => setPlaying(false)
        const onEnded = () => {
            const nextPath = usePlaylistStore.getState().playNext()
            if (nextPath) {
                usePlaybackStore.getState().playMedia(nextPath)
            } else {
                setPlaying(false)
            }
        }

        audio.addEventListener('timeupdate', onTimeUpdate)
        audio.addEventListener('durationchange', onDurationChange)
        audio.addEventListener('play', onPlay)
        audio.addEventListener('pause', onPause)
        audio.addEventListener('ended', onEnded)

        return () => {
            audio.removeEventListener('timeupdate', onTimeUpdate)
            audio.removeEventListener('durationchange', onDurationChange)
            audio.removeEventListener('play', onPlay)
            audio.removeEventListener('pause', onPause)
            audio.removeEventListener('ended', onEnded)
        }
    }, [setCurrentTime, setDuration, setPlaying])

    // Progress bar click to seek
    const handleProgressClick = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
        const audio = audioRef.current
        const bar = progressRef.current
        if (!audio || !bar || !duration) return
        const rect = bar.getBoundingClientRect()
        const fraction = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
        audio.currentTime = fraction * duration
    }, [duration])

    const handlePrevious = useCallback(() => {
        const audio = audioRef.current
        if (audio && audio.currentTime > 3) {
            audio.currentTime = 0
            return
        }
        const prevPath = playPrevious()
        if (prevPath) {
            usePlaybackStore.getState().playMedia(prevPath)
        }
    }, [playPrevious])

    const handleNext = useCallback(() => {
        const nextPath = playNext()
        if (nextPath) {
            usePlaybackStore.getState().playMedia(nextPath)
        }
    }, [playNext])

    const handleDownload = useCallback(() => {
        if (!currentMediaId) return
        const a = document.createElement('a')
        a.href = mediaApi.getDownloadUrl(currentMediaId)
        a.download = ''
        a.click()
    }, [currentMediaId])

    function formatTime(seconds: number): string {
        if (!seconds || !isFinite(seconds)) return '0:00'
        const m = Math.floor(seconds / 60)
        const s = Math.floor(seconds % 60)
        return `${m}:${s.toString().padStart(2, '0')}`
    }

    const progress = duration > 0 ? (currentTime / duration) * 100 : 0

    if (!currentMediaId) return <audio ref={audioRef} preload="auto"/>

    return (
        <>
            <audio ref={audioRef} preload="auto"/>

            {/* Resume dialog */}
            {resumeInfo && (
                <div className="resume-dialog">
                    <span>Resume from {resumeInfo.formattedTime}?</span>
                    <button onClick={acceptResume}>Resume</button>
                    <button onClick={declineResume}>Start Over</button>
                </div>
            )}

            <div className={`audio-player ${currentMediaType === 'video' ? 'audio-player--video' : ''}`}>
                <div className="audio-player__info">
          <span className="audio-player__title" title={currentMediaTitle}>
            {currentMediaTitle}
          </span>
                    {currentPlaylist.length > 0 && (
                        <span className="audio-player__playlist-info">
              {currentIndex + 1} / {currentPlaylist.length}
            </span>
                    )}
                </div>

                <div className="audio-player__controls">
                    <button className="audio-player__btn" onClick={handlePrevious} title="Previous">
                        <i className="bi bi-skip-start-fill"/>
                    </button>
                    <button
                        className="audio-player__btn audio-player__btn--play"
                        onClick={() => usePlaybackStore.getState().togglePlayPause()}
                        title={isPlaying ? 'Pause' : 'Play'}
                    >
                        <i className={`bi ${isPlaying ? 'bi-pause-fill' : 'bi-play-fill'}`}/>
                    </button>
                    <button className="audio-player__btn" onClick={handleNext} title="Next">
                        <i className="bi bi-skip-end-fill"/>
                    </button>
                </div>

                <div className="audio-player__progress-area">
                    <span className="audio-player__time">{formatTime(currentTime)}</span>
                    <div className="audio-player__progress" ref={progressRef} onClick={handleProgressClick}>
                        <div className="audio-player__progress-fill" style={{width: `${progress}%`}}/>
                    </div>
                    <span className="audio-player__time">{formatTime(duration)}</span>
                </div>

                <div className="audio-player__right">
                    {/* Volume */}
                    <button className="audio-player__btn" onClick={toggleMute} title={isMuted ? 'Unmute' : 'Mute'}>
                        <i className={`bi ${isMuted || currentVolume === 0 ? 'bi-volume-mute-fill' : currentVolume < 0.5 ? 'bi-volume-down-fill' : 'bi-volume-up-fill'}`}/>
                    </button>
                    <input
                        className="audio-player__volume"
                        type="range"
                        min="0"
                        max="1"
                        step="0.01"
                        value={isMuted ? 0 : currentVolume}
                        onChange={e => setVolume(parseFloat(e.target.value))}
                    />

                    {/* Speed */}
                    <select
                        className="audio-player__speed"
                        value={playbackSpeed}
                        onChange={e => setPlaybackSpeed(parseFloat(e.target.value))}
                        title="Playback speed"
                    >
                        {[0.5, 0.75, 1, 1.25, 1.5, 1.75, 2].map(s => (
                            <option key={s} value={s}>{s}x</option>
                        ))}
                    </select>

                    {/* HLS quality selector */}
                    {qualities.length > 0 && (
                        <select
                            className="audio-player__quality"
                            value={currentQuality}
                            onChange={e => selectQuality(parseInt(e.target.value))}
                            title="Quality"
                        >
                            <option value={-1}>
                                Auto{currentQuality === -1 && autoLevel >= 0 && qualities[autoLevel]
                                    ? ` (${qualities[autoLevel].name})`
                                    : ''}
                            </option>
                            {qualities.map(q => (
                                <option key={q.index} value={q.index}>{q.name}</option>
                            ))}
                        </select>
                    )}

                    {/* EQ toggle */}
                    <button
                        className={`audio-player__btn ${equalizerVisible ? 'audio-player__btn--active' : ''}`}
                        onClick={onEqualizerToggle}
                        title="Equalizer"
                    >
                        <i className="bi bi-soundwave"/>
                    </button>

                    {/* Download */}
                    <button className="audio-player__btn" onClick={handleDownload} title="Download">
                        <i className="bi bi-download"/>
                    </button>

                    {/* Stop */}
                    <button className="audio-player__btn" onClick={stopPlayback} title="Stop">
                        <i className="bi bi-stop-fill"/>
                    </button>
                </div>
            </div>
        </>
    )
}

// Export eq hook result type for EqualizerPanel
export type {UseEqualizerResult} from '@/hooks/useEqualizer'
