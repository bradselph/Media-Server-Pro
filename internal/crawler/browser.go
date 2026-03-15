// Package crawler — browser-based stream detection via Chrome DevTools Protocol.
//
// This file adds headless-Chrome stream detection to the crawler, modelled on
// the Puppeteer-based approach used in the companion "downloader" project.
// Instead of fetching raw HTML (which misses JS-rendered streams), we:
//
//  1. Launch headless Chrome/Chromium
//  2. Open the target page with network event interception
//  3. Click play buttons and handle age-verification gates
//  4. Collect .m3u8 / .mp4 URLs observed on the network
//
// Communication with Chrome uses the DevTools Protocol (CDP) over WebSocket
// via the already-vendored gorilla/websocket package — no new dependencies.
package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- CDP message helpers ----------

type cdpMsg struct {
	ID     int             `json:"id"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// browserDetector drives headless Chrome via CDP to discover streams.
type browserDetector struct {
	log       loggerI
	timeout   time.Duration
	chromeBin string // path to chrome/chromium binary

	mu     sync.Mutex
	nextID int
}

// loggerI is satisfied by *logger.Logger.
type loggerI interface {
	Info(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// detectedStream represents a single stream found by the browser detector.
type detectedStream struct {
	URL             string `json:"url"`
	Type            string `json:"type"` // "m3u8" or "mp4"
	ContentType     string `json:"content_type"`
	IsMaster        bool   `json:"is_master"`
	Quality         int    `json:"quality"`
	DetectionMethod string `json:"detection_method"` // "browser-network", "browser-dom"
}

// browserProbeResult holds everything discovered from a single page visit.
type browserProbeResult struct {
	Streams []detectedStream
	Title   string
}

// newBrowserDetector creates a browser detector, locating Chrome on disk.
func newBrowserDetector(log loggerI, timeout time.Duration) *browserDetector {
	bin := findChromeBinary()
	return &browserDetector{
		log:       log,
		timeout:   timeout,
		chromeBin: bin,
	}
}

// available reports whether a usable Chrome binary was found.
func (bd *browserDetector) available() bool {
	return bd.chromeBin != ""
}

// probe opens pageURL in headless Chrome, clicks media elements, and returns
// any M3U8/MP4 streams observed on the network plus the page title.
func (bd *browserDetector) probe(ctx context.Context, pageURL string) (*browserProbeResult, error) {
	if bd.chromeBin == "" {
		return nil, fmt.Errorf("no Chrome/Chromium binary found")
	}

	ctx, cancel := context.WithTimeout(ctx, bd.timeout)
	defer cancel()

	// --- 1. Find a free port for CDP ---
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}

	// --- 2. Launch Chrome ---
	// Block private IP resolution to mitigate SSRF when --disable-web-security allows
	// malicious page JS to access local network. Maps RFC1918 ranges to unreachable address.
	hostRules := "MAP 10.0.0.0/8 0.0.0.0, MAP 172.16.0.0/12 0.0.0.0, MAP 192.168.0.0/16 0.0.0.0"
	args := []string{
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-web-security",
		"--disable-features=IsolateOrigins,site-per-process",
		"--disable-blink-features=AutomationControlled",
		"--host-resolver-rules=" + hostRules,
		"--no-first-run",
		"--disable-software-rasterizer",
		"--window-size=1920,1080",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"about:blank",
	}

	cmd := exec.CommandContext(ctx, bd.chromeBin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch chrome: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// --- 3. Connect to CDP WebSocket ---
	wsURL, err := waitForCDP(ctx, port)
	if err != nil {
		return nil, fmt.Errorf("connect CDP: %w", err)
	}

	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("dial CDP: %w", err)
	}
	defer conn.Close()

	// Channels for responses and events (read pump runs until conn closes).
	responses := make(map[int]chan cdpMsg)
	var responsesMu sync.Mutex
	events := make(chan cdpMsg, 256)
	pageLoaded := make(chan struct{}, 1)

	// Read pump
	go func() {
		for {
			var msg cdpMsg
			if err := conn.ReadJSON(&msg); err != nil {
				close(events)
				return
			}
			if msg.ID > 0 {
				responsesMu.Lock()
				ch, ok := responses[msg.ID]
				responsesMu.Unlock()
				if ok {
					ch <- msg
				}
			} else if msg.Method != "" {
				if msg.Method == "Page.loadEventFired" {
					select {
					case pageLoaded <- struct{}{}:
					default:
					}
				}
				select {
				case events <- msg:
				default:
				}
			}
		}
	}()

	send := func(method string, params interface{}) (json.RawMessage, error) {
		bd.mu.Lock()
		bd.nextID++
		id := bd.nextID
		bd.mu.Unlock()

		p, _ := json.Marshal(params)
		raw := map[string]interface{}{
			"id":     id,
			"method": method,
			"params": json.RawMessage(p),
		}

		ch := make(chan cdpMsg, 1)
		responsesMu.Lock()
		responses[id] = ch
		responsesMu.Unlock()
		defer func() {
			responsesMu.Lock()
			delete(responses, id)
			responsesMu.Unlock()
		}()

		if err := conn.WriteJSON(raw); err != nil {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case resp := <-ch:
			if resp.Error != nil {
				return nil, fmt.Errorf("CDP %s: %s", method, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

	// --- 4. Enable domains ---
	send("Network.enable", map[string]interface{}{})
	send("Page.enable", map[string]interface{}{})
	send("Runtime.enable", map[string]interface{}{})

	// --- 5. Collect network responses ---
	result := &browserProbeResult{}
	var streamsMu sync.Mutex
	seen := make(map[string]bool)

	// Ad domain patterns (from downloader's videoDetector.js)
	adDomains := []string{
		"trafficjunky", "trafficstars", "exosrv", "exoclick",
		"adskeeper", "juicyads", "adxpansion", "plugrush",
		"tsyndicate", "realsrv", "popads", "popcash",
		"propellerads", "adsterra", "clickadu", "hilltopads",
		"pushhouse", "richpush", "megapush", "mobidea",
		"clickdealer", "cpmstar", "bidvertiser", "adcash",
		"admaven", "ad-maven", "ero-advertising", "tubecorporate",
		"contentabc", "awempire", "livejasmin", "stripchat",
		"cam4", "cams.com", "imlive", "flirt4free", "streamate",
		"bongacams", "chaturbate.com/affiliates",
	}
	adPathPatterns := []string{"/ad_", "_ad_", "_ad.", "/ads_", "adframe", "/preroll"}

	isAd := func(u string) bool {
		lower := strings.ToLower(u)
		for _, d := range adDomains {
			if strings.Contains(lower, d) {
				return true
			}
		}
		for _, p := range adPathPatterns {
			if strings.Contains(lower, p) {
				return true
			}
		}
		return false
	}

	// Background goroutine to process network events
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range events {
			if evt.Method == "Network.responseReceived" {
				var p struct {
					Response struct {
						URL     string            `json:"url"`
						Status  int               `json:"status"`
						Headers map[string]string `json:"headers"`
					} `json:"response"`
				}
				if err := json.Unmarshal(evt.Params, &p); err != nil {
					continue
				}

				respURL := p.Response.URL
				status := p.Response.Status
				ct := strings.ToLower(p.Response.Headers["content-type"])

				if status != 200 && status != 206 {
					continue
				}
				if isAd(respURL) {
					continue
				}

				isM3U8 := strings.Contains(respURL, ".m3u8") ||
					strings.Contains(ct, "mpegurl") ||
					strings.Contains(ct, "x-mpegurl")
				isMP4 := (strings.Contains(respURL, ".mp4") || strings.Contains(ct, "video/mp4")) &&
					!strings.HasPrefix(respURL, "blob:") &&
					!strings.HasPrefix(respURL, "data:")

				if isM3U8 || isMP4 {
					streamsMu.Lock()
					base := strings.SplitN(respURL, "?", 2)[0]
					if !seen[base] {
						seen[base] = true
						st := detectedStream{
							URL:             respURL,
							ContentType:     ct,
							DetectionMethod: "browser-network",
						}
						if isM3U8 {
							st.Type = "m3u8"
						} else {
							st.Type = "mp4"
						}
						result.Streams = append(result.Streams, st)
						bd.log.Info("Browser detected %s: %s", st.Type, truncURL(respURL))
					}
					streamsMu.Unlock()
				}
			}
		}
	}()

	// --- 6. Navigate to target page ---
	bd.log.Info("Browser navigating to: %s", pageURL)
	navParams := map[string]interface{}{"url": pageURL}
	if _, err := send("Page.navigate", navParams); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	// Wait for page load
	waitForSignal(ctx, pageLoaded, 15*time.Second)

	// Small delay for JS to initialize
	sleep(ctx, 3*time.Second)

	// --- 7. Handle age verification (mirroring downloader approach) ---
	bd.handleAgeVerification(ctx, send)
	sleep(ctx, 1*time.Second)

	// --- 8. Get page title ---
	titleRes, err := send("Runtime.evaluate", map[string]interface{}{
		"expression": "document.title",
	})
	if err == nil {
		var tr struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(titleRes, &tr) == nil && tr.Result.Value != "" {
			result.Title = cleanPageTitle(tr.Result.Value)
		}
	}

	// --- 9. Click play buttons to trigger stream loading ---
	bd.triggerVideoPlayback(send)
	sleep(ctx, 4*time.Second)

	// Second round of clicking (some players need it)
	bd.triggerVideoPlayback(send)
	sleep(ctx, 2*time.Second)

	// --- 10. Extract embedded video URLs from DOM/scripts ---
	bd.extractEmbeddedURLs(send, result, &streamsMu, seen)

	// Let any final network events settle
	sleep(ctx, 1*time.Second)

	bd.log.Info("Browser probe complete: %d streams found on %s", len(result.Streams), pageURL)
	return result, nil
}

// handleAgeVerification clicks through common age gates.
// Mirrors the downloader's handleAgeVerification logic.
func (bd *browserDetector) handleAgeVerification(ctx context.Context, send func(string, interface{}) (json.RawMessage, error)) {
	bd.log.Debug("Checking for age verification popups...")

	// JavaScript to find and click age-verification / cookie-consent buttons.
	// This mirrors the comprehensive list from the downloader's videoDetector.js.
	js := `(function() {
		var buttonTexts = ['enter', 'i agree', 'agree', 'yes', 'accept', 'continue',
			'i am 18', 'i am over 18', "i'm over 18", 'verify'];

		// CSS selectors for common age gates
		var selectors = [
			'button[class*="enter"]', 'button[class*="agree"]',
			'button[class*="accept"]', 'button[class*="confirm"]',
			'button[class*="verify"]', 'button[class*="consent"]',
			'a[class*="enter"]', 'a[class*="agree"]',
			'#js-age-verification-box button',
			'.age-verification-modal button',
			'.disclaimer-exit-button', '#disclaimer-accept',
			'.modal button.btn-primary', '.popup button.accept',
			'.age-gate button', '.age-check button',
			'[class*="age-verif"] button', '[class*="age-gate"] button',
			'[class*="disclaimer"] button',
			'#onetrust-accept-btn-handler', '.cookie-consent-accept',
			'[class*="cookie"] button[class*="accept"]',
			'.fc-cta-consent'
		];

		// Try CSS selectors first
		for (var i = 0; i < selectors.length; i++) {
			var el = document.querySelector(selectors[i]);
			if (el && el.offsetParent !== null) {
				el.click();
				return 'css:' + selectors[i];
			}
		}

		// Try text-based matching on buttons and clickable elements
		var btns = document.querySelectorAll(
			'button, a.button, a.btn, input[type="button"], input[type="submit"], [role="button"], .btn, .button');
		for (var j = 0; j < btns.length; j++) {
			var text = (btns[j].textContent || btns[j].value || '').toLowerCase().trim();
			var visible = btns[j].offsetParent !== null;
			if (visible) {
				for (var k = 0; k < buttonTexts.length; k++) {
					if (text.indexOf(buttonTexts[k]) !== -1) {
						btns[j].click();
						return 'text:' + text;
					}
				}
			}
		}
		return '';
	})()`

	res, err := send("Runtime.evaluate", map[string]interface{}{
		"expression": js,
	})
	if err != nil {
		return
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res, &r) == nil && r.Result.Value != "" {
		bd.log.Info("Clicked age verification: %s", r.Result.Value)
		sleep(ctx, 2*time.Second)
	}
}

// triggerVideoPlayback finds and clicks play buttons and video elements.
// Mirrors the downloader's triggerVideoPlayback logic.
func (bd *browserDetector) triggerVideoPlayback(send func(string, interface{}) (json.RawMessage, error)) {
	// JavaScript to mute videos and click play buttons.
	// Selector list mirrors the downloader's videoDetector.js triggerVideoPlayback.
	js := `(function() {
		var clicked = [];

		// Mute and play all video elements
		var videos = document.querySelectorAll('video');
		for (var i = 0; i < videos.length; i++) {
			videos[i].muted = true;
			try { videos[i].play(); } catch(e) {}
		}

		// Common play button selectors (from downloader videoDetector.js)
		var selectors = [
			'button[class*="play"]',
			'button[aria-label*="Play"]', 'button[aria-label*="play"]',
			'button[title*="Play"]',
			'.play-button', '#play-button',
			'.vjs-big-play-button',
			'.video-play-button',
			'[class*="play-btn"]', '[class*="playButton"]',
			'.mgp_videoPlayBtnTxt',
			'.video-element-wrapper', '.player-wrapper', '#player',
			'.fp-play', '.jw-icon-playback', '.plyr__control--play'
		];

		for (var i = 0; i < selectors.length; i++) {
			var btn = document.querySelector(selectors[i]);
			if (btn && btn.offsetParent !== null) {
				try {
					btn.click();
					clicked.push(selectors[i]);
				} catch(e) {}
			}
		}

		return clicked.join(',');
	})()`

	res, err := send("Runtime.evaluate", map[string]interface{}{
		"expression": js,
	})
	if err != nil {
		return
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res, &r) == nil && r.Result.Value != "" {
		bd.log.Debug("Clicked play buttons: %s", r.Result.Value)
	}
}

// extractEmbeddedURLs pulls M3U8/MP4 URLs from the page's DOM and scripts.
// This handles sites that embed stream URLs in JS variables rather than
// loading them via network requests. Mirrors the downloader's site-specific
// extractors (PornHub flashvars, XVideos setVideoHLS, etc.) plus a generic
// fallback that scans all <script> contents.
func (bd *browserDetector) extractEmbeddedURLs(
	send func(string, interface{}) (json.RawMessage, error),
	result *browserProbeResult,
	mu *sync.Mutex,
	seen map[string]bool,
) {
	// Large JS snippet that extracts video URLs from various page structures.
	// Returns JSON array of {url, type} objects.
	js := `(function() {
		var found = [];
		var hostname = location.hostname.toLowerCase();

		// --- PornHub: flashvars_XXXXX.mediaDefinitions ---
		if (hostname.indexOf('pornhub') !== -1) {
			for (var key in window) {
				if (key.indexOf('flashvars_') === 0 && window[key] && window[key].mediaDefinitions) {
					var defs = window[key].mediaDefinitions;
					for (var i = 0; i < defs.length; i++) {
						var d = defs[i];
						if (d.videoUrl) {
							var t = (d.videoUrl.indexOf('.m3u8') !== -1 || d.format === 'hls') ? 'm3u8' : 'mp4';
							found.push({url: d.videoUrl, type: t});
						}
					}
					break;
				}
			}
		}

		// --- XVideos / XNXX: setVideoHLS / setVideoUrlHigh ---
		if (hostname.indexOf('xvideos') !== -1 || hostname.indexOf('xnxx') !== -1) {
			var scripts = document.querySelectorAll('script');
			for (var i = 0; i < scripts.length; i++) {
				var c = scripts[i].textContent || '';
				var hlsM = c.match(/setVideoHLS\(['"]([^'"]+)['"]\)/);
				if (hlsM) found.push({url: hlsM[1], type: 'm3u8'});
				var highM = c.match(/setVideoUrlHigh\(['"]([^'"]+)['"]\)/);
				if (highM) found.push({url: highM[1], type: 'mp4'});
				var lowM = c.match(/setVideoUrlLow\(['"]([^'"]+)['"]\)/);
				if (lowM) found.push({url: lowM[1], type: 'mp4'});
			}
		}

		// --- RedGifs: video source elements ---
		if (hostname.indexOf('redgifs') !== -1) {
			var sources = document.querySelectorAll('video source');
			for (var i = 0; i < sources.length; i++) {
				if (sources[i].src && sources[i].src.indexOf('redgifs') !== -1) {
					found.push({url: sources[i].src, type: 'mp4'});
				}
			}
		}

		// --- Generic: scan <video> src, <source> elements, and script contents ---
		var videos = document.querySelectorAll('video');
		for (var i = 0; i < videos.length; i++) {
			var src = videos[i].src || videos[i].currentSrc || '';
			if (src && src.indexOf('blob:') !== 0 && src.indexOf('data:') !== 0) {
				var t = src.indexOf('.m3u8') !== -1 ? 'm3u8' : 'mp4';
				found.push({url: src, type: t});
			}
			var srcs = videos[i].querySelectorAll('source');
			for (var j = 0; j < srcs.length; j++) {
				if (srcs[j].src && srcs[j].src.indexOf('blob:') !== 0) {
					var t2 = srcs[j].src.indexOf('.m3u8') !== -1 ? 'm3u8' : 'mp4';
					found.push({url: srcs[j].src, type: t2});
				}
			}
		}

		// Check player data attributes
		var players = document.querySelectorAll('#player, .video-wrapper, [class*="player"]');
		for (var i = 0; i < players.length; i++) {
			var p = players[i];
			var dUrl = p.dataset.videoUrl || p.dataset.src || p.dataset.hls || '';
			if (dUrl) {
				var t = dUrl.indexOf('.m3u8') !== -1 ? 'm3u8' : 'mp4';
				found.push({url: dUrl, type: t});
			}
		}

		// Scan all script tags for M3U8 URLs
		var allScripts = document.querySelectorAll('script');
		for (var i = 0; i < allScripts.length; i++) {
			var content = allScripts[i].textContent || '';
			var m3u8Matches = content.match(/https?:\/\/[^\s"'<>]+\.m3u8[^\s"'<>]*/g);
			if (m3u8Matches) {
				for (var j = 0; j < m3u8Matches.length; j++) {
					found.push({url: m3u8Matches[j], type: 'm3u8'});
				}
			}
		}

		// Deduplicate
		var seen = {};
		var unique = [];
		for (var i = 0; i < found.length; i++) {
			var key = found[i].url.split('?')[0];
			if (!seen[key]) {
				seen[key] = true;
				unique.push(found[i]);
			}
		}
		return JSON.stringify(unique);
	})()`

	res, err := send("Runtime.evaluate", map[string]interface{}{
		"expression": js,
	})
	if err != nil {
		bd.log.Warn("Failed to extract embedded URLs: %v", err)
		return
	}

	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &r); err != nil || r.Result.Value == "" {
		return
	}

	var embedded []struct {
		URL  string `json:"url"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(r.Result.Value), &embedded); err != nil {
		bd.log.Warn("Failed to parse embedded URLs: %v", err)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	for _, e := range embedded {
		// Clean HTML entities
		u := strings.ReplaceAll(e.URL, "&amp;", "&")
		u = strings.ReplaceAll(u, "\\u0026", "&")
		u = strings.ReplaceAll(u, "\\/", "/")

		base := strings.SplitN(u, "?", 2)[0]
		if seen[base] {
			continue
		}
		seen[base] = true

		result.Streams = append(result.Streams, detectedStream{
			URL:             u,
			Type:            e.Type,
			DetectionMethod: "browser-dom",
		})
		bd.log.Info("Browser DOM extracted %s: %s", e.Type, truncURL(u))
	}
}

// ---------- Chrome binary discovery ----------

func findChromeBinary() string {
	// Check environment variable first
	if p := os.Getenv("CHROME_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	var candidates []string
	switch runtime.GOOS {
	case "linux":
		candidates = []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			"/usr/bin/microsoft-edge",
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		}
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Fallback: check PATH
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}

	return ""
}

// ---------- CDP connection helpers ----------

// waitForCDP polls Chrome's /json/version endpoint to get the WebSocket URL.
func waitForCDP(ctx context.Context, port int) (string, error) {
	addr := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	deadline := time.After(15 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", fmt.Errorf("timeout waiting for Chrome CDP on port %d", port)
		default:
		}

		resp, err := httpGet(addr)
		if err == nil {
			var info struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if json.Unmarshal(resp, &info) == nil && info.WebSocketDebuggerURL != "" {
				return info.WebSocketDebuggerURL, nil
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// cdpHTTPClient is used only for polling the local CDP /json/version endpoint.
var cdpHTTPClient = http.Client{Timeout: 2 * time.Second}

// httpGet is a minimal HTTP GET for the CDP JSON endpoint.
func httpGet(rawURL string) ([]byte, error) {
	resp, err := cdpHTTPClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 32768))
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// ---------- Misc helpers ----------

func waitForSignal(ctx context.Context, ch <-chan struct{}, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-ch:
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func cleanPageTitle(title string) string {
	// Remove common site name suffixes
	for _, suffix := range []string{
		" - Pornhub.com", " - XVIDEOS.COM", " - YouPorn",
		" - YouTube", " - YouTube Music", " - RedGIFs",
	} {
		title = strings.TrimSuffix(title, suffix)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "Untitled"
	}
	return title
}

func truncURL(u string) string {
	if len(u) > 120 {
		return u[:120] + "..."
	}
	return u
}
