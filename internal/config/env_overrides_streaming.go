package config

import "time"

func (m *Manager) applyStreamingEnvOverrides() {
	m.applyStreamingChunkOverrides()
	m.applyStreamingKeepAliveOverrides()
	m.applyStreamingAdaptiveOverrides()
	m.applyDownloadEnvOverrides()
}

func (m *Manager) applyStreamingChunkOverrides() {
	if val, ok := envGetInt64("STREAMING_CHUNK_SIZE"); ok {
		m.config.Streaming.DefaultChunkSize = val
	}
	if val, ok := envGetInt64("STREAMING_MAX_CHUNK_SIZE"); ok {
		m.config.Streaming.MaxChunkSize = val
	}
	if val, ok := envGetInt("STREAMING_BUFFER_SIZE"); ok {
		m.config.Streaming.BufferSize = val
	}
}

func (m *Manager) applyStreamingKeepAliveOverrides() {
	if val, ok := envGetBool("STREAMING_KEEP_ALIVE_ENABLED"); ok {
		m.config.Streaming.KeepAliveEnabled = val
	}
	if val, ok := envGetDuration(time.Second, "STREAMING_KEEP_ALIVE_TIMEOUT_SECONDS"); ok {
		m.config.Streaming.KeepAliveTimeout = val
	}
}

func (m *Manager) applyStreamingAdaptiveOverrides() {
	if val, ok := envGetBool("STREAMING_ADAPTIVE"); ok {
		m.config.Streaming.Adaptive = val
	}
	if val, ok := envGetBool("STREAMING_MOBILE_OPTIMIZATION"); ok {
		m.config.Streaming.MobileOptimization = val
	}
	if val, ok := envGetInt64("STREAMING_MOBILE_CHUNK_SIZE"); ok {
		m.config.Streaming.MobileChunkSize = val
	}
}

func (m *Manager) applyDownloadEnvOverrides() {
	if val, ok := envGetBool("DOWNLOAD_ENABLED"); ok {
		m.config.Download.Enabled = val
	}
	if val, ok := envGetInt("DOWNLOAD_CHUNK_SIZE_KB"); ok {
		m.config.Download.ChunkSizeKB = val
	}
	if val, ok := envGetBool("DOWNLOAD_REQUIRE_AUTH"); ok {
		m.config.Download.RequireAuth = val
	}
}
