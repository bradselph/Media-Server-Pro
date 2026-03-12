package config

import (
	"strings"
	"time"
)

func (m *Manager) applyHuggingFaceEnvOverrides() {
	if val, ok := envGetBool("HUGGINGFACE_ENABLED"); ok {
		m.config.HuggingFace.Enabled = val
	}
	if val := envGetStr("HUGGINGFACE_API_KEY"); val != "" {
		m.config.HuggingFace.APIKey = val
	}
	if val := envGetStr("HUGGINGFACE_MODEL"); val != "" {
		m.config.HuggingFace.Model = val
	}
	if val := envGetStr("HUGGINGFACE_ENDPOINT_URL"); val != "" {
		m.config.HuggingFace.EndpointURL = val
	}
	if val, ok := envGetInt("HUGGINGFACE_MAX_FRAMES"); ok {
		m.config.HuggingFace.MaxFrames = val
	}
	if val, ok := envGetInt("HUGGINGFACE_TIMEOUT_SECS"); ok {
		m.config.HuggingFace.TimeoutSecs = val
	}
	if val, ok := envGetInt("HUGGINGFACE_RATE_LIMIT"); ok {
		m.config.HuggingFace.RateLimit = val
	}
	if val, ok := envGetInt("HUGGINGFACE_MAX_CONCURRENT"); ok {
		m.config.HuggingFace.MaxConcurrent = val
	}
}

func (m *Manager) applyMatureScannerEnvOverrides() {
	if val, ok := envGetBool("MATURE_SCANNER_ENABLED"); ok {
		m.config.MatureScanner.Enabled = val
	}
	if val, ok := envGetBool("MATURE_SCANNER_AUTO_FLAG"); ok {
		m.config.MatureScanner.AutoFlag = val
	}
	if val, ok := envGetFloat64("MATURE_SCANNER_HIGH_CONFIDENCE_THRESHOLD"); ok {
		m.config.MatureScanner.HighConfidenceThreshold = val
	}
	if val, ok := envGetFloat64("MATURE_SCANNER_MEDIUM_CONFIDENCE_THRESHOLD"); ok {
		m.config.MatureScanner.MediumConfidenceThreshold = val
	}
	if val := envGetStr("MATURE_SCANNER_HIGH_CONFIDENCE_KEYWORDS"); val != "" {
		m.config.MatureScanner.HighConfidenceKeywords = strings.Split(val, ",")
	}
	if val := envGetStr("MATURE_SCANNER_MEDIUM_CONFIDENCE_KEYWORDS"); val != "" {
		m.config.MatureScanner.MediumConfidenceKeywords = strings.Split(val, ",")
	}
	if val, ok := envGetBool("MATURE_SCANNER_REQUIRE_REVIEW"); ok {
		m.config.MatureScanner.RequireReview = val
	}
}

func (m *Manager) applyRemoteMediaEnvOverrides() {
	if val, ok := envGetBool("REMOTE_MEDIA_ENABLED"); ok {
		m.config.RemoteMedia.Enabled = val
	}
	if val, ok := envGetDuration(time.Minute, "REMOTE_MEDIA_SYNC_INTERVAL_MINUTES"); ok {
		m.config.RemoteMedia.SyncInterval = val
	}
	if val, ok := envGetBool("REMOTE_MEDIA_CACHE_ENABLED"); ok {
		m.config.RemoteMedia.CacheEnabled = val
	}
	if val, ok := envGetInt64("REMOTE_MEDIA_CACHE_SIZE"); ok {
		m.config.RemoteMedia.CacheSize = val
	} else if val, ok := envGetInt64("REMOTE_MEDIA_CACHE_SIZE_MB"); ok {
		m.config.RemoteMedia.CacheSize = val * 1024 * 1024
	}
	if val, ok := envGetDuration(time.Hour, "REMOTE_MEDIA_CACHE_TTL_HOURS"); ok {
		m.config.RemoteMedia.CacheTTL = val
	}
}

func (m *Manager) applyReceiverEnvOverrides() {
	if val, ok := envGetBool("RECEIVER_ENABLED"); ok {
		m.config.Receiver.Enabled = val
	}
	m.applyReceiverAPIKeysOverride()
	if val, ok := envGetInt("RECEIVER_MAX_PROXY_CONNS"); ok {
		m.config.Receiver.MaxProxyConns = val
	}
	if val, ok := envGetDuration(time.Second, "RECEIVER_PROXY_TIMEOUT_SECONDS"); ok {
		m.config.Receiver.ProxyTimeout = val
	}
	if val, ok := envGetDuration(time.Second, "RECEIVER_HEALTH_CHECK_SECONDS"); ok {
		m.config.Receiver.HealthCheck = val
	}
}

func (m *Manager) applyReceiverAPIKeysOverride() {
	val := envGetStr("RECEIVER_API_KEY", "RECEIVER_API_KEYS")
	if val == "" {
		return
	}
	keys := strings.Split(val, ",")
	trimmed := make([]string, 0, len(keys))
	for _, k := range keys {
		if s := strings.TrimSpace(k); s != "" {
			trimmed = append(trimmed, s)
		}
	}
	if len(trimmed) > 0 {
		m.config.Receiver.APIKeys = trimmed
	}
}

func (m *Manager) applyExtractorEnvOverrides() {
	if val, ok := envGetBool("EXTRACTOR_ENABLED"); ok {
		m.config.Extractor.Enabled = val
	}
	if val, ok := envGetDuration(time.Second, "EXTRACTOR_PROXY_TIMEOUT_SECONDS"); ok {
		m.config.Extractor.ProxyTimeout = val
	}
	if val, ok := envGetInt("EXTRACTOR_MAX_ITEMS"); ok {
		m.config.Extractor.MaxItems = val
	}
}

func (m *Manager) applyCrawlerEnvOverrides() {
	if val, ok := envGetBool("CRAWLER_ENABLED"); ok {
		m.config.Crawler.Enabled = val
	}
	if val, ok := envGetBool("CRAWLER_BROWSER_ENABLED"); ok {
		m.config.Crawler.BrowserEnabled = val
	}
	if val, ok := envGetInt("CRAWLER_MAX_PAGES"); ok {
		m.config.Crawler.MaxPages = val
	}
	if val, ok := envGetDuration(time.Second, "CRAWLER_TIMEOUT_SECONDS"); ok {
		m.config.Crawler.CrawlTimeout = val
	}
}
