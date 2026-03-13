import { Link } from 'react-router-dom'
import { PlayerSettingsPanel } from '@/components/PlayerSettingsPanel'
import { mediaApi } from '@/api/endpoints'
import { formatDuration, formatFileSize, formatTitle } from '@/utils/formatters'
import {
    createSeekBack,
    createSeekForward,
    cyclePlaybackSpeed,
    getVolumeIconClass,
} from './playerHelpers'
import { ICON_ARROW_LEFT, LABEL_BACK_TO_LIBRARY, CSS_TEXT_COLOR, CSS_TEXT_MUTED } from './playerConstants'
import { MatureWarningModal } from './MatureWarningModal'
import { PlayerSidebar } from './PlayerSidebar'
import type { PlayerPageState } from './usePlayerPageState'

// Sub-components to keep PlayerPageContent under cognitive-complexity limit.
// They receive explicit props (no full state object) to satisfy react-hooks/refs:
// refs must not be read during render; passing a state object that contains refs
// causes the linter to flag every state.* access.

interface PlayerVideoAudioBlockProps {
    isAudio: boolean
    isVideo: boolean
    audioRef: React.RefObject<HTMLAudioElement | null>
    videoRef: React.RefObject<HTMLVideoElement | null>
    isLoading: boolean
    hlsIsLoading: boolean
    showControls: boolean
    isPlaying: boolean
    buffered: number
    progress: number
    hoverTime: number | null
    hoverPos: number
    hasPlaylist: boolean
    isMuted: boolean
    volume: number
    currentTime: number
    duration: number
    qualityBadge: string | null
    playbackRate: number
    showSettings: boolean
    hlsQualities: PlayerPageState['hlsQualities']
    currentQuality: number
    autoLevel: number
    isLooping: boolean
    bandwidth: number | null
    handleTimeUpdate: () => void
    handleLoadedMetadata: () => void
    handleDurationChange: () => void
    handlePlay: () => void
    handlePause: () => void
    handleEnded: () => void
    handleSeeked: () => void
    handleWaiting: () => void
    handleCanPlay: () => void
    handleProgressClick: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressHover: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressLeave: () => void
    handleProgressTouch: (e: React.TouchEvent<HTMLDivElement>) => void
    handlePrevTrack: () => void
    handleNextTrack: () => void
    togglePlay: () => void
    toggleMute: () => void
    handleVolumeChange: (e: React.ChangeEvent<HTMLInputElement>) => void
    setShowSettings: (fn: (s: boolean) => boolean) => void
    handleSelectQualityWithAnalytics: (index: number) => void
    setSpeed: (speed: number) => void
    toggleLoop: () => void
    handlePiP: () => void
    handleFullscreen: () => void
    mediaName: string
    seekBack: () => void
    seekForward: () => void
}

interface PlayerCustomControlsProps {
    showControls: boolean
    isPlaying: boolean
    buffered: number
    progress: number
    hoverTime: number | null
    hoverPos: number
    hasPlaylist: boolean
    isMuted: boolean
    volume: number
    currentTime: number
    duration: number
    qualityBadge: string | null
    playbackRate: number
    showSettings: boolean
    isVideo: boolean
    hlsQualities: PlayerPageState['hlsQualities']
    currentQuality: number
    autoLevel: number
    isLooping: boolean
    bandwidth: number | null
    handleProgressClick: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressHover: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressLeave: () => void
    handleProgressTouch: (e: React.TouchEvent<HTMLDivElement>) => void
    handlePrevTrack: () => void
    handleNextTrack: () => void
    togglePlay: () => void
    toggleMute: () => void
    handleVolumeChange: (e: React.ChangeEvent<HTMLInputElement>) => void
    setShowSettings: (fn: (s: boolean) => boolean) => void
    handleSelectQualityWithAnalytics: (index: number) => void
    setSpeed: (speed: number) => void
    toggleLoop: () => void
    handlePiP: () => void
    handleFullscreen: () => void
    seekBack: () => void
    seekForward: () => void
}

function PlayerMediaElements(props: {
    isAudio: boolean
    isVideo: boolean
    audioRef: React.RefObject<HTMLAudioElement | null>
    videoRef: React.RefObject<HTMLVideoElement | null>
    mediaName: string
    handleTimeUpdate: () => void
    handleLoadedMetadata: () => void
    handleDurationChange: () => void
    handlePlay: () => void
    handlePause: () => void
    handleEnded: () => void
    handleSeeked: () => void
    handleWaiting: () => void
    handleCanPlay: () => void
}) {
    const {
        isAudio,
        isVideo,
        audioRef,
        videoRef,
        mediaName,
        handleTimeUpdate,
        handleLoadedMetadata,
        handleDurationChange,
        handlePlay,
        handlePause,
        handleEnded,
        handleSeeked,
        handleWaiting,
        handleCanPlay,
    } = props
    return (
        <>
            {isAudio && (
                <>
                    <audio
                        ref={audioRef}
                        onTimeUpdate={handleTimeUpdate}
                        onLoadedMetadata={handleLoadedMetadata}
                        onDurationChange={handleDurationChange}
                        onPlay={handlePlay}
                        onPause={handlePause}
                        onEnded={handleEnded}
                        onSeeked={handleSeeked}
                        onWaiting={handleWaiting}
                        onCanPlay={handleCanPlay}
                        preload="auto"
                    />
                    <div className="audio-visualizer">
                        <div className="audio-visualizer-icon">
                            <i className="bi bi-music-note-beamed" />
                        </div>
                        <div className="audio-visualizer-title">{formatTitle({ value: mediaName })}</div>
                    </div>
                </>
            )}
            {isVideo && (
                <video
                    ref={videoRef}
                    onTimeUpdate={handleTimeUpdate}
                    onLoadedMetadata={handleLoadedMetadata}
                    onDurationChange={handleDurationChange}
                    onPlay={handlePlay}
                    onPause={handlePause}
                    onEnded={handleEnded}
                    onSeeked={handleSeeked}
                    onWaiting={handleWaiting}
                    onCanPlay={handleCanPlay}
                    preload="auto"
                    playsInline
                    webkit-playsinline=""
                />
            )}
        </>
    )
}

function PlayerLoadingOverlay(props: { isLoading: boolean; hlsIsLoading: boolean }) {
    const { isLoading, hlsIsLoading } = props
    if (!isLoading && !hlsIsLoading) return null
    return (
        <div className="player-loading">
            <div className="player-loading-spinner" />
        </div>
    )
}

function PlayerProgressBar(props: {
    buffered: number
    progress: number
    hoverTime: number | null
    hoverPos: number
    handleProgressClick: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressHover: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressLeave: () => void
    handleProgressTouch: (e: React.TouchEvent<HTMLDivElement>) => void
}) {
    const { buffered, progress, hoverTime, hoverPos, handleProgressClick, handleProgressHover, handleProgressLeave, handleProgressTouch } = props
    return (
        <div
            className="ctrl-progress-container"
            onClick={handleProgressClick}
            onMouseMove={handleProgressHover}
            onMouseLeave={handleProgressLeave}
            onTouchStart={handleProgressTouch}
            onTouchMove={handleProgressTouch}
        >
            <div className="ctrl-buffer-bar" style={{ width: `${buffered}%` }} />
            <div className="ctrl-progress-fill" style={{ width: `${progress}%` }} />
            {hoverTime !== null && (
                <div className="ctrl-progress-tooltip" style={{ left: `${hoverPos}px` }}>
                    {formatDuration({ seconds: hoverTime })}
                </div>
            )}
        </div>
    )
}

function PlayerTransportButtons(props: {
    hasPlaylist: boolean
    isPlaying: boolean
    handlePrevTrack: () => void
    handleNextTrack: () => void
    togglePlay: () => void
    seekBack: () => void
    seekForward: () => void
}) {
    const { hasPlaylist, isPlaying, handlePrevTrack, handleNextTrack, togglePlay, seekBack, seekForward } = props
    return (
        <>
            {hasPlaylist && (
                <button className="ctrl-btn" onClick={handlePrevTrack} title="Previous track">
                    <i className="bi bi-skip-start-fill" />
                </button>
            )}
            <button className="ctrl-btn" onClick={togglePlay} title="Play/Pause (K)">
                {isPlaying ? <i className="bi bi-pause-fill" /> : <i className="bi bi-play-fill" />}
            </button>
            {hasPlaylist && (
                <button className="ctrl-btn" onClick={handleNextTrack} title="Next track">
                    <i className="bi bi-skip-end-fill" />
                </button>
            )}
            <button className="ctrl-btn" onClick={seekBack} title="Back 10s (J)">
                <i className="bi bi-skip-backward-fill" />
                <span className="ctrl-btn-label">10</span>
            </button>
            <button className="ctrl-btn" onClick={seekForward} title="Forward 10s (L)">
                <span className="ctrl-btn-label">10</span>
                <i className="bi bi-skip-forward-fill" />
            </button>
        </>
    )
}

function PlayerVolumeControls(props: {
    isMuted: boolean
    volume: number
    toggleMute: () => void
    handleVolumeChange: (e: React.ChangeEvent<HTMLInputElement>) => void
}) {
    const { isMuted, volume, toggleMute, handleVolumeChange } = props
    return (
        <div className="ctrl-volume-wrapper">
            <button className="ctrl-btn" onClick={toggleMute} title="Mute (M)">
                <i className={getVolumeIconClass(isMuted, volume)} />
            </button>
            <input
                type="range"
                className="ctrl-volume-slider"
                min="0"
                max="1"
                step="0.05"
                value={isMuted ? 0 : volume}
                onChange={handleVolumeChange}
                onClick={(e) => e.stopPropagation()}
            />
        </div>
    )
}

function PlayerControlBadges(props: {
    qualityBadge: string | null
    playbackRate: number
}) {
    const { qualityBadge, playbackRate } = props
    return (
        <>
            {qualityBadge && (
                <span className="ctrl-quality-badge" title="Current quality">
                    {qualityBadge}
                </span>
            )}
            {playbackRate !== 1 && (
                <span className="ctrl-speed-badge" title="Playback speed">
                    {playbackRate}x
                </span>
            )}
        </>
    )
}

function PlayerSettingsAndFullscreen(props: {
    showSettings: boolean
    isVideo: boolean
    hlsQualities: PlayerPageState['hlsQualities']
    currentQuality: number
    autoLevel: number
    playbackRate: number
    isLooping: boolean
    bandwidth: number | null
    setShowSettings: (fn: (s: boolean) => boolean) => void
    handleSelectQualityWithAnalytics: (index: number) => void
    setSpeed: (speed: number) => void
    toggleLoop: () => void
    handlePiP: () => void
    handleFullscreen: () => void
}) {
    const {
        showSettings,
        isVideo,
        hlsQualities,
        currentQuality,
        autoLevel,
        playbackRate,
        isLooping,
        bandwidth,
        setShowSettings,
        handleSelectQualityWithAnalytics,
        setSpeed,
        toggleLoop,
        handlePiP,
        handleFullscreen,
    } = props
    return (
        <>
            <div className="ctrl-settings-wrapper">
                <button
                    className={`ctrl-btn ${showSettings ? 'active' : ''}`}
                    onClick={() => setShowSettings((s: boolean) => !s)}
                    title="Settings"
                >
                    <i className={`bi bi-gear-fill ${showSettings ? 'ctrl-gear-spin' : ''}`} />
                </button>
                {showSettings && (
                    <PlayerSettingsPanel
                        qualities={hlsQualities}
                        currentQuality={currentQuality}
                        autoLevel={autoLevel}
                        onSelectQuality={handleSelectQualityWithAnalytics}
                        playbackRate={playbackRate}
                        onSetSpeed={setSpeed}
                        isLooping={isLooping}
                        onToggleLoop={toggleLoop}
                        showPiP={isVideo}
                        onPiP={handlePiP}
                        bandwidth={bandwidth ?? undefined}
                        onClose={() => setShowSettings(() => false)}
                    />
                )}
            </div>
            {isVideo && (
                <button className="ctrl-btn" onClick={handleFullscreen} title="Fullscreen (F)">
                    <i className="bi bi-fullscreen" />
                </button>
            )}
        </>
    )
}

function PlayerCustomControls(props: PlayerCustomControlsProps) {
    const {
        showControls,
        isPlaying,
        buffered,
        progress,
        hoverTime,
        hoverPos,
        hasPlaylist,
        isMuted,
        volume,
        currentTime,
        duration,
        qualityBadge,
        playbackRate,
        showSettings,
        isVideo,
        hlsQualities,
        currentQuality,
        autoLevel,
        isLooping,
        bandwidth,
        handleProgressClick,
        handleProgressHover,
        handleProgressLeave,
        handleProgressTouch,
        handlePrevTrack,
        handleNextTrack,
        togglePlay,
        toggleMute,
        handleVolumeChange,
        setShowSettings,
        handleSelectQualityWithAnalytics,
        setSpeed,
        toggleLoop,
        handlePiP,
        handleFullscreen,
        seekBack,
        seekForward,
    } = props
    return (
        <div
            className={`custom-controls ${showControls || !isPlaying ? 'show' : ''}`}
            onClick={(e) => e.stopPropagation()}
        >
            <PlayerProgressBar
                buffered={buffered}
                progress={progress}
                hoverTime={hoverTime}
                hoverPos={hoverPos}
                handleProgressClick={handleProgressClick}
                handleProgressHover={handleProgressHover}
                handleProgressLeave={handleProgressLeave}
                handleProgressTouch={handleProgressTouch}
            />
            <div className="ctrl-row">
                <PlayerTransportButtons
                    hasPlaylist={hasPlaylist}
                    isPlaying={isPlaying}
                    handlePrevTrack={handlePrevTrack}
                    handleNextTrack={handleNextTrack}
                    togglePlay={togglePlay}
                    seekBack={seekBack}
                    seekForward={seekForward}
                />
                <PlayerVolumeControls
                    isMuted={isMuted}
                    volume={volume}
                    toggleMute={toggleMute}
                    handleVolumeChange={handleVolumeChange}
                />
                <span className="ctrl-time">
                    {formatDuration({ seconds: currentTime })} / {formatDuration({ seconds: duration })}
                </span>
                <div className="ctrl-spacer" />
                <PlayerControlBadges qualityBadge={qualityBadge} playbackRate={playbackRate} />
                <PlayerSettingsAndFullscreen
                    showSettings={showSettings}
                    isVideo={isVideo}
                    hlsQualities={hlsQualities}
                    currentQuality={currentQuality}
                    autoLevel={autoLevel}
                    playbackRate={playbackRate}
                    isLooping={isLooping}
                    bandwidth={bandwidth}
                    setShowSettings={setShowSettings}
                    handleSelectQualityWithAnalytics={handleSelectQualityWithAnalytics}
                    setSpeed={setSpeed}
                    toggleLoop={toggleLoop}
                    handlePiP={handlePiP}
                    handleFullscreen={handleFullscreen}
                />
            </div>
        </div>
    )
}

function PlayerVideoAudioBlock(props: PlayerVideoAudioBlockProps) {
    const {
        isAudio,
        isVideo,
        audioRef,
        videoRef,
        isLoading,
        hlsIsLoading,
        mediaName,
        handleTimeUpdate,
        handleLoadedMetadata,
        handleDurationChange,
        handlePlay,
        handlePause,
        handleEnded,
        handleSeeked,
        handleWaiting,
        handleCanPlay,
        ...controlsProps
    } = props

    return (
        <>
            <PlayerMediaElements
                isAudio={isAudio}
                isVideo={isVideo}
                audioRef={audioRef}
                videoRef={videoRef}
                mediaName={mediaName}
                handleTimeUpdate={handleTimeUpdate}
                handleLoadedMetadata={handleLoadedMetadata}
                handleDurationChange={handleDurationChange}
                handlePlay={handlePlay}
                handlePause={handlePause}
                handleEnded={handleEnded}
                handleSeeked={handleSeeked}
                handleWaiting={handleWaiting}
                handleCanPlay={handleCanPlay}
            />
            <PlayerLoadingOverlay isLoading={isLoading} hlsIsLoading={hlsIsLoading} />
            <PlayerCustomControls {...controlsProps} isVideo={isVideo} />
        </>
    )
}

interface PlayerAudioCardProps {
    progress: number
    hoverTime: number | null
    hoverPos: number
    hasPlaylist: boolean
    isPlaying: boolean
    currentTime: number
    duration: number
    isLooping: boolean
    playbackRate: number
    isMuted: boolean
    volume: number
    handleProgressClick: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressHover: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressLeave: () => void
    handleProgressTouch: (e: React.TouchEvent<HTMLDivElement>) => void
    handlePrevTrack: () => void
    handleNextTrack: () => void
    togglePlay: () => void
    toggleLoop: () => void
    toggleMute: () => void
    handleVolumeChange: (e: React.ChangeEvent<HTMLInputElement>) => void
    seekBack: () => void
    seekForward: () => void
    cycleSpeed: () => void
}

function AudioProgressBar(props: {
    progress: number
    hoverTime: number | null
    hoverPos: number
    handleProgressClick: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressHover: (e: React.MouseEvent<HTMLDivElement>) => void
    handleProgressLeave: () => void
    handleProgressTouch: (e: React.TouchEvent<HTMLDivElement>) => void
}) {
    const { progress, hoverTime, hoverPos, handleProgressClick, handleProgressHover, handleProgressLeave, handleProgressTouch } = props
    return (
        <div style={{ marginBottom: 10 }}>
            <div
                className="ctrl-progress-container audio-progress"
                onClick={handleProgressClick}
                onMouseMove={handleProgressHover}
                onMouseLeave={handleProgressLeave}
                onTouchStart={handleProgressTouch}
                onTouchMove={handleProgressTouch}
            >
                <div className="ctrl-progress-fill" style={{ width: `${progress}%`, background: '#667eea' }} />
                {hoverTime !== null && (
                    <div
                        className="ctrl-progress-tooltip ctrl-progress-tooltip--audio"
                        style={{ left: `${hoverPos}px` }}
                    >
                        {formatDuration({ seconds: hoverTime })}
                    </div>
                )}
            </div>
        </div>
    )
}

function AudioControlsRow(props: {
    hasPlaylist: boolean
    isPlaying: boolean
    currentTime: number
    duration: number
    isLooping: boolean
    playbackRate: number
    isMuted: boolean
    volume: number
    handlePrevTrack: () => void
    handleNextTrack: () => void
    togglePlay: () => void
    toggleLoop: () => void
    toggleMute: () => void
    handleVolumeChange: (e: React.ChangeEvent<HTMLInputElement>) => void
    seekBack: () => void
    seekForward: () => void
    cycleSpeed: () => void
}) {
    const {
        hasPlaylist,
        isPlaying,
        currentTime,
        duration,
        isLooping,
        playbackRate,
        isMuted,
        volume,
        handlePrevTrack,
        handleNextTrack,
        togglePlay,
        toggleLoop,
        toggleMute,
        handleVolumeChange,
        seekBack,
        seekForward,
        cycleSpeed,
    } = props
    return (
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
            {hasPlaylist && (
                <button className="ctrl-btn" style={{ color: CSS_TEXT_COLOR }} onClick={handlePrevTrack} title="Previous track">
                    <i className="bi bi-skip-start-fill" />
                </button>
            )}
            <button className="ctrl-btn" style={{ color: CSS_TEXT_COLOR }} onClick={seekBack} title="Back 10s">
                <i className="bi bi-skip-backward-fill" />
            </button>
            <button className="ctrl-btn audio-play-btn" style={{ color: CSS_TEXT_COLOR }} onClick={togglePlay} title="Play/Pause (Space)">
                {isPlaying ? <i className="bi bi-pause-fill" /> : <i className="bi bi-play-fill" />}
            </button>
            <button className="ctrl-btn" style={{ color: CSS_TEXT_COLOR }} onClick={seekForward} title="Forward 10s">
                <i className="bi bi-skip-forward-fill" />
            </button>
            {hasPlaylist && (
                <button className="ctrl-btn" style={{ color: CSS_TEXT_COLOR }} onClick={handleNextTrack} title="Next track">
                    <i className="bi bi-skip-end-fill" />
                </button>
            )}
            <span style={{ fontSize: 12, color: CSS_TEXT_MUTED, fontFamily: 'monospace', whiteSpace: 'nowrap', marginLeft: 4 }}>
                {formatDuration({ seconds: currentTime })} / {formatDuration({ seconds: duration })}
            </span>
            <div style={{ flex: 1 }} />
            <button
                className={`ctrl-btn ${isLooping ? 'active' : ''}`}
                style={{ color: isLooping ? '#667eea' : CSS_TEXT_COLOR, fontSize: 15 }}
                onClick={toggleLoop}
                title="Loop"
            >
                <i className="bi bi-repeat" />
            </button>
            <button
                className="ctrl-btn"
                style={{
                    color: playbackRate !== 1 ? '#667eea' : CSS_TEXT_COLOR,
                    fontSize: 11,
                    fontFamily: 'monospace',
                    minWidth: 38,
                    fontWeight: 600,
                }}
                onClick={cycleSpeed}
                title="Playback speed"
            >
                {playbackRate}x
            </button>
            <button className="ctrl-btn" style={{ color: CSS_TEXT_COLOR }} onClick={toggleMute} title="Mute (M)">
                <i className={getVolumeIconClass(isMuted, volume)} />
            </button>
            <input
                type="range"
                min="0"
                max="1"
                step="0.05"
                value={isMuted ? 0 : volume}
                onChange={handleVolumeChange}
                style={{ width: 70, cursor: 'pointer', accentColor: '#667eea' }}
            />
        </div>
    )
}

function PlayerAudioCard(props: PlayerAudioCardProps) {
    const {
        progress,
        hoverTime,
        hoverPos,
        hasPlaylist,
        isPlaying,
        currentTime,
        duration,
        isLooping,
        playbackRate,
        isMuted,
        volume,
        handleProgressClick,
        handleProgressHover,
        handleProgressLeave,
        handleProgressTouch,
        handlePrevTrack,
        handleNextTrack,
        togglePlay,
        toggleLoop,
        toggleMute,
        handleVolumeChange,
        seekBack,
        seekForward,
        cycleSpeed,
    } = props

    return (
        <div
            style={{
                background: 'var(--card-bg)',
                border: '1px solid var(--border-color)',
                borderRadius: 10,
                padding: '12px 16px',
            }}
        >
            <AudioProgressBar
                progress={progress}
                hoverTime={hoverTime}
                hoverPos={hoverPos}
                handleProgressClick={handleProgressClick}
                handleProgressHover={handleProgressHover}
                handleProgressLeave={handleProgressLeave}
                handleProgressTouch={handleProgressTouch}
            />
            <AudioControlsRow
                hasPlaylist={hasPlaylist}
                isPlaying={isPlaying}
                currentTime={currentTime}
                duration={duration}
                isLooping={isLooping}
                playbackRate={playbackRate}
                isMuted={isMuted}
                volume={volume}
                handlePrevTrack={handlePrevTrack}
                handleNextTrack={handleNextTrack}
                togglePlay={togglePlay}
                toggleLoop={toggleLoop}
                toggleMute={toggleMute}
                handleVolumeChange={handleVolumeChange}
                seekBack={seekBack}
                seekForward={seekForward}
                cycleSpeed={cycleSpeed}
            />
        </div>
    )
}

function PlayerHLSBanners(props: {
    hlsAvailable: boolean
    hlsReadyUrl: string | null
    activeHlsUrl: string | null
    hlsJob: PlayerPageState['hlsJob']
    setActiveHlsUrl: (u: string | null) => void
    setHlsAvailable: (v: boolean) => void
}) {
    const { hlsAvailable, hlsReadyUrl, activeHlsUrl, hlsJob, setActiveHlsUrl, setHlsAvailable } = props
    const showAvailableBanner = hlsAvailable && hlsReadyUrl && !activeHlsUrl
    const showProgressBanner = hlsJob?.status === 'running'
    if (!showAvailableBanner && !showProgressBanner) return null
    return (
        <>
            {showAvailableBanner && (
                <div className="hls-available-banner">
                    <span>
                        <i className="bi bi-lightning-fill" /> HLS adaptive stream ready
                    </span>
                    <div className="hls-banner-actions">
                        <button className="hls-switch-btn" onClick={() => { setActiveHlsUrl(hlsReadyUrl); setHlsAvailable(false) }}>
                            <i className="bi bi-play-circle" /> Switch to HLS
                        </button>
                        <button className="hls-dismiss-btn" onClick={() => setHlsAvailable(false)}>
                            Dismiss
                        </button>
                    </div>
                </div>
            )}
            {showProgressBanner && hlsJob && (
                <div className="hls-progress-wrapper">
                    <div style={{ color: CSS_TEXT_MUTED, fontSize: 13 }}>
                        <i className="bi bi-arrow-repeat" /> Generating HLS adaptive stream...
                    </div>
                    <div className="hls-bar-bg">
                        <div className="hls-bar-fill" style={{ width: `${hlsJob.progress}%` }} />
                    </div>
                    <div style={{ fontSize: 12, color: CSS_TEXT_MUTED }}>{hlsJob.progress}% complete</div>
                </div>
            )}
        </>
    )
}

function PlayerMediaInfoCard(props: {
    media: NonNullable<PlayerPageState['media']>
    permissions: PlayerPageState['permissions']
    showToast: PlayerPageState['showToast']
}) {
    const { media, permissions, showToast } = props
    return (
        <div className="media-info-card">
            <h1 className="media-page-title">{formatTitle({ value: media.name })}</h1>
            <div className="media-page-stats">
                <span><i className="bi bi-eye" /> {media.views} views</span>
                {media.date_added && (
                    <span>
                        <i className="bi bi-calendar3" /> {new Date(media.date_added).toLocaleDateString()}
                    </span>
                )}
                <span><i className="bi bi-hdd-fill" /> {formatFileSize({ bytes: media.size })}</span>
                <span><i className="bi bi-clock" /> {formatDuration({ seconds: media.duration })}</span>
                {media.width && media.height && (
                    <span><i className="bi bi-aspect-ratio" /> {media.width}x{media.height}</span>
                )}
                {media.container && (
                    <span><i className="bi bi-file-play" /> {media.container}</span>
                )}
            </div>
            <div className="media-action-buttons">
                {permissions.can_download && (
                    <a href={mediaApi.getDownloadUrl(media.id)} className="media-action-btn">
                        <i className="bi bi-download" /> Download
                    </a>
                )}
                <button
                    className="media-action-btn"
                    onClick={() => {
                        const url = `${window.location.origin}/player?id=${encodeURIComponent(media.id)}`
                        navigator.clipboard.writeText(url).then(
                            () => showToast('Link copied to clipboard', 'success'),
                            () => showToast('Could not copy link', 'error'),
                        )
                    }}
                >
                    <i className="bi bi-share-fill" /> Share
                </button>
            </div>
            {media.category && (
                <div className="media-category">
                    <strong>Category:</strong> {media.category}
                    {media.is_mature && (
                        <span className="media-card-type-badge badge-mature" style={{ marginLeft: 8 }}>18+</span>
                    )}
                </div>
            )}
        </div>
    )
}

function PlayerPageHeader(props: {
    theaterMode: boolean
    setTheaterMode: (fn: (t: boolean) => boolean) => void
    isVideo: boolean
}) {
    const { theaterMode, setTheaterMode, isVideo } = props
    return (
        <div className="player-header">
            <Link to="/" className="player-back-btn">
                <i className={ICON_ARROW_LEFT} /> {LABEL_BACK_TO_LIBRARY}
            </Link>
            {isVideo && (
                <button
                    className={`player-theater-btn ${theaterMode ? 'player-theater-btn--active' : ''}`}
                    onClick={() => setTheaterMode((t) => !t)}
                    title="Theater mode (T)"
                >
                    <i className={theaterMode ? 'bi bi-arrows-angle-contract' : 'bi bi-arrows-angle-expand'} />
                </button>
            )}
        </div>
    )
}

interface PlayerMainColumnProps {
    videoWrapperClass: string
    isVideo: boolean
    isAudio: boolean
    resetControlsTimer: () => void
    isPlaying: boolean
    setShowControls: (fn: (s: boolean) => boolean) => void
    handleVideoClick: () => void
    media: NonNullable<PlayerPageState['media']>
    videoBlockProps: PlayerVideoAudioBlockProps
    audioCardProps: PlayerAudioCardProps
    hlsAvailable: boolean
    hlsReadyUrl: string | null
    activeHlsUrl: string | null
    hlsJob: PlayerPageState['hlsJob']
    setActiveHlsUrl: (u: string | null) => void
    setHlsAvailable: (v: boolean) => void
    permissions: PlayerPageState['permissions']
    showToast: PlayerPageState['showToast']
}

function PlayerMainColumn(props: PlayerMainColumnProps) {
    const {
        videoWrapperClass,
        isVideo,
        isAudio,
        resetControlsTimer,
        isPlaying,
        setShowControls,
        handleVideoClick,
        media,
        videoBlockProps,
        audioCardProps,
        hlsAvailable,
        hlsReadyUrl,
        activeHlsUrl,
        hlsJob,
        setActiveHlsUrl,
        setHlsAvailable,
        permissions,
        showToast,
    } = props

    return (
        <div className="player-main">
            <div
                className={videoWrapperClass}
                onMouseMove={isVideo ? resetControlsTimer : undefined}
                onMouseLeave={isVideo && isPlaying ? () => setShowControls(() => false) : undefined}
                onClick={isVideo ? handleVideoClick : undefined}
                onTouchEnd={isVideo ? (e) => {
                    // On mobile, tap should toggle controls visibility, not bubble to onClick
                    // which would toggle play/pause. Only handle taps on the video area itself.
                    if (e.target === e.currentTarget || (e.target as HTMLElement).tagName === 'VIDEO') {
                        e.preventDefault()
                        resetControlsTimer()
                    }
                } : undefined}
            >
                <PlayerVideoAudioBlock {...videoBlockProps} />
            </div>
            {isAudio && <PlayerAudioCard {...audioCardProps} />}
            <PlayerHLSBanners
                hlsAvailable={hlsAvailable}
                hlsReadyUrl={hlsReadyUrl}
                activeHlsUrl={activeHlsUrl}
                hlsJob={hlsJob}
                setActiveHlsUrl={setActiveHlsUrl}
                setHlsAvailable={setHlsAvailable}
            />
            <PlayerMediaInfoCard media={media} permissions={permissions} showToast={showToast} />
        </div>
    )
}

function buildVideoBlockProps(state: PlayerPageState): PlayerVideoAudioBlockProps | null {
    const { media, getActiveEl } = state
    if (!media) return null
    const seekBack = createSeekBack(getActiveEl)
    const seekForward = createSeekForward(getActiveEl)
    return {
        isAudio: state.isAudio,
        isVideo: state.isVideo,
        audioRef: state.audioRef,
        videoRef: state.videoRef,
        isLoading: state.isLoading,
        hlsIsLoading: state.hlsIsLoading,
        showControls: state.showControls,
        isPlaying: state.isPlaying,
        buffered: state.buffered,
        progress: state.progress,
        hoverTime: state.hoverTime,
        hoverPos: state.hoverPos,
        hasPlaylist: state.hasPlaylist,
        isMuted: state.isMuted,
        volume: state.volume,
        currentTime: state.currentTime,
        duration: state.duration,
        qualityBadge: state.qualityBadge,
        playbackRate: state.playbackRate,
        showSettings: state.showSettings,
        hlsQualities: state.hlsQualities,
        currentQuality: state.currentQuality,
        autoLevel: state.autoLevel,
        isLooping: state.isLooping,
        bandwidth: state.bandwidth,
        handleTimeUpdate: state.handleTimeUpdate,
        handleLoadedMetadata: state.handleLoadedMetadata,
        handleDurationChange: state.handleDurationChange,
        handlePlay: state.handlePlay,
        handlePause: state.handlePause,
        handleEnded: state.handleEnded,
        handleSeeked: state.handleSeeked,
        handleWaiting: state.handleWaiting,
        handleCanPlay: state.handleCanPlay,
        handleProgressClick: state.handleProgressClick,
        handleProgressHover: state.handleProgressHover,
        handleProgressLeave: state.handleProgressLeave,
        handleProgressTouch: state.handleProgressTouch,
        handlePrevTrack: state.handlePrevTrack,
        handleNextTrack: state.handleNextTrack,
        togglePlay: state.togglePlay,
        toggleMute: state.toggleMute,
        handleVolumeChange: state.handleVolumeChange,
        setShowSettings: state.setShowSettings,
        handleSelectQualityWithAnalytics: state.handleSelectQualityWithAnalytics,
        setSpeed: state.setSpeed,
        toggleLoop: state.toggleLoop,
        handlePiP: state.handlePiP,
        handleFullscreen: state.handleFullscreen,
        mediaName: media.name,
        seekBack,
        seekForward,
    }
}

function buildAudioCardProps(
    state: PlayerPageState,
    seekBack: () => void,
    seekForward: () => void,
): PlayerAudioCardProps {
    const { playbackRate, setSpeed } = state
    const cycleSpeed = () => cyclePlaybackSpeed(playbackRate, setSpeed)
    return {
        progress: state.progress,
        hoverTime: state.hoverTime,
        hoverPos: state.hoverPos,
        hasPlaylist: state.hasPlaylist,
        isPlaying: state.isPlaying,
        currentTime: state.currentTime,
        duration: state.duration,
        isLooping: state.isLooping,
        playbackRate: state.playbackRate,
        isMuted: state.isMuted,
        volume: state.volume,
        handleProgressClick: state.handleProgressClick,
        handleProgressHover: state.handleProgressHover,
        handleProgressLeave: state.handleProgressLeave,
        handleProgressTouch: state.handleProgressTouch,
        handlePrevTrack: state.handlePrevTrack,
        handleNextTrack: state.handleNextTrack,
        togglePlay: state.togglePlay,
        toggleLoop: state.toggleLoop,
        toggleMute: state.toggleMute,
        handleVolumeChange: state.handleVolumeChange,
        seekBack,
        seekForward,
        cycleSpeed,
    }
}

function buildPlayerContentProps(state: PlayerPageState): {
    videoBlockProps: PlayerVideoAudioBlockProps
    audioCardProps: PlayerAudioCardProps
    videoWrapperClass: string
} | null {
    const { media, getActiveEl, isVideo, isPlaying, showControls } = state
    if (!media) return null

    const seekBack = createSeekBack(getActiveEl)
    const seekForward = createSeekForward(getActiveEl)
    const videoWrapperClass = `video-wrapper${isVideo && isPlaying && !showControls ? ' playing-idle' : ''}`

    const videoBlockProps = buildVideoBlockProps(state)
    if (!videoBlockProps) return null

    const audioCardProps = buildAudioCardProps(state, seekBack, seekForward)

    return { videoBlockProps, audioCardProps, videoWrapperClass }
}

export function PlayerPageContent(state: PlayerPageState) {
    const {
        media,
        showMatureWarning,
        setMatureAccepted,
        theaterMode,
        setTheaterMode,
        isVideo,
        isAudio,
        isPlaying,
        resetControlsTimer,
        setShowControls,
        handleVideoClick,
        hlsAvailable,
        hlsReadyUrl,
        activeHlsUrl,
        hlsJob,
        setActiveHlsUrl,
        setHlsAvailable,
        permissions,
        showToast,
        relatedLabel,
        relatedStillLoading,
        similarError,
        similarRefetch,
        related,
        canViewMature,
        user,
        handleRate,
        ratingHover,
        setRatingHover,
        userRating,
    } = state

    if (!media) return null

    const contentProps = buildPlayerContentProps(state)
    if (!contentProps) return null

    const { videoBlockProps, audioCardProps, videoWrapperClass } = contentProps

    return (
        <div className="player-page">
            {showMatureWarning && (
                <MatureWarningModal
                    onAccept={() => setMatureAccepted(true)}
                />
            )}
            <div className={`player-page-container ${theaterMode ? 'player-page-container--theater' : ''}`}>
                <PlayerPageHeader
                    theaterMode={theaterMode}
                    setTheaterMode={setTheaterMode}
                    isVideo={isVideo}
                />
                <div className={`player-layout ${theaterMode ? 'player-layout--theater' : ''}`}>
                    <PlayerMainColumn
                        videoWrapperClass={videoWrapperClass}
                        isVideo={isVideo}
                        isAudio={isAudio}
                        resetControlsTimer={resetControlsTimer}
                        isPlaying={isPlaying}
                        setShowControls={setShowControls}
                        handleVideoClick={handleVideoClick}
                        media={media}
                        videoBlockProps={videoBlockProps}
                        audioCardProps={audioCardProps}
                        hlsAvailable={hlsAvailable}
                        hlsReadyUrl={hlsReadyUrl}
                        activeHlsUrl={activeHlsUrl}
                        hlsJob={hlsJob}
                        setActiveHlsUrl={setActiveHlsUrl}
                        setHlsAvailable={setHlsAvailable}
                        permissions={permissions}
                        showToast={showToast}
                    />
                    <PlayerSidebar
                        relatedLabel={relatedLabel}
                        relatedStillLoading={relatedStillLoading}
                        similarError={similarError}
                        similarRefetch={similarRefetch}
                        related={related}
                        canViewMature={canViewMature}
                        user={user}
                        handleRate={handleRate}
                        ratingHover={ratingHover}
                        setRatingHover={setRatingHover}
                        userRating={userRating}
                    />
                </div>
            </div>
        </div>
    )
}
