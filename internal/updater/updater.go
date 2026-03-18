// Package updater provides automatic update checking and installation.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const (
	// GitHubOwner is the repository owner
	GitHubOwner = "bradselph"

	// GitHubRepo is the repository name
	GitHubRepo = "Media-Server-Pro"

	// GitHubAPI is the GitHub API base URL
	GitHubAPI = "https://api.github.com"
)

// Module handles update checking and installation
type Module struct {
	config         *config.Manager
	log            *logger.Logger
	httpClient     *http.Client
	healthy        bool
	healthMsg      string
	healthMu       sync.RWMutex
	mu             sync.RWMutex
	lastCheck      *UpdateCheckResult
	checkTicker    *time.Ticker
	backupDir      string
	currentVersion string

	// buildMu guards activeBuild — the live status of a running source update.
	// A copy of the status is stored here at every stage transition so the
	// polling endpoint can read progress without blocking on the build goroutine.
	buildMu     sync.RWMutex
	activeBuild *UpdateStatus

	// applyMu guards applyRunning to prevent concurrent binary update installs.
	// Unlike source builds (which are async), ApplyUpdate runs synchronously in
	// the HTTP handler, so we need our own guard separate from buildMu.
	applyMu      sync.RWMutex
	applyRunning bool

	stopOnce    sync.Once
	checkDone   chan struct{} // closed in Stop to signal checkLoop to exit
	checkExited chan struct{} // closed by checkLoop when it exits
}

// UpdateCheckResult holds the result of an update check
type UpdateCheckResult struct {
	CurrentVersion  string     `json:"current_version"`
	LatestVersion   string     `json:"latest_version"`
	UpdateAvailable bool       `json:"update_available"`
	ReleaseURL      string     `json:"release_url,omitempty"`
	ReleaseNotes    string     `json:"release_notes,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CheckedAt       time.Time  `json:"checked_at"`
	Error           string     `json:"error,omitempty"`
}

// UpdateStatus holds the status of an update operation
type UpdateStatus struct {
	InProgress bool      `json:"in_progress"`
	Stage      string    `json:"stage"`
	Progress   float64   `json:"progress"`
	StartedAt  time.Time `json:"started_at"`
	Error      string    `json:"error,omitempty"`
	BackupPath string    `json:"backup_path,omitempty"`
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int    `json:"size"`
	} `json:"assets"`
	Prerelease bool `json:"prerelease"`
	Draft      bool `json:"draft"`
}

// DefaultVersion is the fallback when no version is supplied via ldflags; must match server default.
const DefaultVersion = "4.0.0"

// NewModule creates a new updater module. version should be the build-time version
// string (e.g. from -ldflags), falling back to DefaultVersion if empty.
func NewModule(cfg *config.Manager, version string) *Module {
	if version == "" {
		version = DefaultVersion
	}
	return &Module{
		config:         cfg,
		log:            logger.New("updater"),
		currentVersion: version,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		checkDone:   make(chan struct{}),
		checkExited: make(chan struct{}),
		backupDir:   filepath.Join(cfg.Get().Directories.Data, "backups", "updates"),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "updater"
}

// isDirWritable returns true if the directory exists and can be written to.
func isDirWritable(dir string) bool {
	probe := filepath.Join(dir, ".write_probe")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}

// Start initializes the updater module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting updater module...")

	// Ensure backup directory exists
	if err := os.MkdirAll(m.backupDir, 0755); err != nil {
		m.log.Warn("Failed to create backup directory: %v", err)
	}

	// If the configured backup dir isn't writable (e.g. created by root on VPS),
	// fall back to a directory next to the server executable.
	if !isDirWritable(m.backupDir) {
		if ep, err := os.Executable(); err == nil {
			fallback := filepath.Join(filepath.Dir(ep), "backups")
			if mkErr := os.MkdirAll(fallback, 0755); mkErr == nil {
				m.log.Warn("Backup dir %s not writable — using fallback: %s", m.backupDir, fallback)
				m.backupDir = fallback
			}
		}
	}

	// Clean up old executable backups from previous updates
	execPath, err := os.Executable()
	if err == nil {
		oldPath := execPath + ".old"
		if _, statErr := os.Stat(oldPath); statErr == nil {
			if removeErr := os.Remove(oldPath); removeErr != nil {
				m.log.Warn("Failed to remove old executable backup %s: %v", oldPath, removeErr)
			} else {
				m.log.Info("Cleaned up old executable backup: %s", oldPath)
			}
		}
	}

	// Start periodic update checking (every 24 hours)
	m.checkTicker = time.NewTicker(24 * time.Hour)
	go m.checkLoop()

	// Do an initial check
	go func() {
		if _, err := m.CheckForUpdates(); err != nil {
			m.log.Warn("Initial update check failed: %v", err)
		}
	}()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Updater module started (version: %s)", m.currentVersion)
	return nil
}

// Stop gracefully stops the module. It stops the ticker, signals checkLoop to exit,
// and waits for checkLoop to finish. Safe to call multiple times (sync.Once).
// The initial update check goroutine started in Start is not awaited and may still run after return.
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping updater module...")

	m.stopOnce.Do(func() {
		if m.checkTicker != nil {
			m.checkTicker.Stop()
		}
		close(m.checkDone)
		<-m.checkExited
	})

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// checkLoop periodically checks for updates
func (m *Module) checkLoop() {
	defer close(m.checkExited)
	for {
		select {
		case <-m.checkTicker.C:
			if _, err := m.CheckForUpdates(); err != nil {
				m.log.Warn("Periodic update check failed: %v", err)
			}
		case <-m.checkDone:
			return
		}
	}
}

// CheckForUpdates checks GitHub for new releases
func (m *Module) CheckForUpdates() (*UpdateCheckResult, error) {
	m.log.Debug("Checking for updates...")

	result := &UpdateCheckResult{
		CurrentVersion: m.currentVersion,
		CheckedAt:      time.Now(),
	}

	// Get latest release from GitHub
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", GitHubAPI, GitHubOwner, GitHubRepo)
	req, err := m.newGitHubRequest("GET", apiURL)
	if err != nil {
		result.Error = err.Error()
		m.cacheResult(result)
		return result, err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to contact GitHub: %v", err)
		m.log.Error("Update check failed: %v", err)
		m.cacheResult(result)
		return result, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		// No releases yet
		result.LatestVersion = m.currentVersion
		result.UpdateAvailable = false
		m.log.Debug("No releases found on GitHub")
		m.cacheResult(result)
		return result, nil
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Error = fmt.Sprintf("GitHub API authentication failed (HTTP %d) — set a GitHub token in admin settings", resp.StatusCode)
		m.cacheResult(result)
		return result, fmt.Errorf("GitHub API auth error: %d", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("GitHub API returned status %d", resp.StatusCode)
		m.cacheResult(result)
		return result, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		result.Error = fmt.Sprintf("Failed to parse GitHub response: %v", err)
		m.cacheResult(result)
		return result, err
	}

	// Parse version from tag (remove 'v' prefix if present)
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	result.LatestVersion = latestVersion
	result.ReleaseURL = release.HTMLURL
	result.ReleaseNotes = release.Body
	if !release.PublishedAt.IsZero() {
		result.PublishedAt = new(release.PublishedAt)
	}

	// Compare versions
	result.UpdateAvailable = isNewerVersion(latestVersion, m.currentVersion)

	if result.UpdateAvailable {
		m.log.Info("Update available: %s -> %s", m.currentVersion, latestVersion)
	} else {
		m.log.Debug("Current version is up to date")
	}

	m.cacheResult(result)
	return result, nil
}

// isNewerVersion compares two semantic versions
func isNewerVersion(latest, current string) bool {
	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		// Extract numeric part only (handles versions like "3.0.0-alpha")
		latestNumStr := extractNumericPrefix(latestParts[i])
		currentNumStr := extractNumericPrefix(currentParts[i])

		latestNum, latestErr := strconv.Atoi(latestNumStr)
		currentNum, currentErr := strconv.Atoi(currentNumStr)

		// If either fails to parse, treat as 0 but log would be helpful
		if latestErr != nil {
			latestNum = 0
		}
		if currentErr != nil {
			currentNum = 0
		}

		if latestNum > currentNum {
			return true
		} else if latestNum < currentNum {
			return false
		}
	}

	return len(latestParts) > len(currentParts)
}

// extractNumericPrefix extracts the leading numeric portion of a version string
// e.g., "3" from "3", "0" from "0-alpha", "10" from "10beta"
func extractNumericPrefix(s string) string {
	var result strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result.WriteRune(c)
		} else {
			break
		}
	}
	if result.Len() == 0 {
		return "0"
	}
	return result.String()
}

func (m *Module) cacheResult(result *UpdateCheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCheck = result
}

// GetLastCheck returns the last update check result
func (m *Module) GetLastCheck() *UpdateCheckResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCheck
}

// ApplyUpdate downloads and installs an update
func (m *Module) ApplyUpdate(_ context.Context) (*UpdateStatus, error) {
	// Prevent concurrent installs — two simultaneous calls would corrupt the binary.
	m.applyMu.Lock()
	if m.applyRunning {
		m.applyMu.Unlock()
		return nil, fmt.Errorf("a binary update is already in progress")
	}
	m.applyRunning = true
	m.applyMu.Unlock()
	defer func() {
		m.applyMu.Lock()
		m.applyRunning = false
		m.applyMu.Unlock()
	}()

	status := &UpdateStatus{
		InProgress: true,
		Stage:      "initializing",
		StartedAt:  time.Now(),
	}

	// Check if update is available
	m.mu.RLock()
	lastCheck := m.lastCheck
	m.mu.RUnlock()

	if lastCheck == nil || !lastCheck.UpdateAvailable {
		status.Error = "No update available"
		status.InProgress = false
		return status, fmt.Errorf("no update available")
	}

	m.log.Info("Starting update to version %s", lastCheck.LatestVersion)

	// Create backup (non-fatal — backup is a safety net, not a prerequisite)
	status.Stage = "creating backup"
	status.Progress = 10
	var backupPath string
	if bp, berr := m.createBackup(); berr != nil {
		m.log.Warn("Could not create backup before binary update: %v", berr)
	} else {
		backupPath = bp
		status.BackupPath = backupPath
	}

	// Download the update — try gh CLI first (uses system auth), fall back to
	// direct HTTP with the admin-configured token.
	status.Stage = "downloading update"
	status.Progress = 30

	assetName := m.getAssetName()

	// Attempt 1: gh CLI (preferred — uses whatever auth is already configured on
	// the system, so no separate token setup is needed in admin settings).
	tmpFile, err := m.downloadWithGhCLI(lastCheck.LatestVersion, assetName)
	if err != nil {
		m.log.Warn("gh CLI download failed (%v) — falling back to direct HTTP", err)

		// Attempt 2: direct HTTP with optional admin-configured token.
		downloadURL, urlErr := m.getAssetURL(lastCheck.LatestVersion, assetName)
		if urlErr != nil {
			status.Error = fmt.Sprintf("Failed to get download URL: %v", urlErr)
			status.InProgress = false
			return status, urlErr
		}
		tmpFile, err = m.downloadUpdate(downloadURL)
		if err != nil {
			status.Error = fmt.Sprintf("Download failed: %v", err)
			status.InProgress = false
			return status, err
		}
	}
	defer func() {
		if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
			m.log.Warn("Failed to remove temporary update file %s: %v", tmpFile, err)
		}
	}()

	// Verify SHA256 checksum when a checksum file is available in the release.
	// This guards against supply-chain attacks (compromised release) or
	// network-level corruption (MITM even over HTTPS).
	status.Stage = "verifying checksum"
	status.Progress = 55
	if verifyErr := m.verifyBinaryChecksum(lastCheck.LatestVersion, assetName, tmpFile); verifyErr != nil {
		status.Error = fmt.Sprintf("Checksum verification failed: %v", verifyErr)
		status.InProgress = false
		m.log.Error("Binary update rejected: %v", verifyErr)
		return status, verifyErr
	}

	status.Stage = "installing update"
	status.Progress = 70

	// Install the update
	if err := m.installUpdate(tmpFile); err != nil {
		status.Error = fmt.Sprintf("Installation failed: %v", err)
		status.InProgress = false
		// Attempt restore from backup
		m.log.Warn("Update failed, attempting restore from backup")
		if restoreErr := m.restoreFromBackup(backupPath); restoreErr != nil {
			m.log.Error("Restore also failed: %v", restoreErr)
		}
		return status, err
	}

	status.Stage = "completed"
	status.Progress = 100
	status.InProgress = false

	m.log.Info("Update completed successfully. Restart required.")
	return status, nil
}

// createBackup saves a copy of the running executable before an update.
// Only the binary is backed up — config, data, and media files are not touched.
func (m *Module) createBackup() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}

	backupName := fmt.Sprintf("server_%s_%s.backup",
		m.currentVersion,
		time.Now().Format("20060102_150405"))
	backupPath := filepath.Join(m.backupDir, backupName)

	if err := copyFile(execPath, backupPath); err != nil {
		return "", fmt.Errorf("backup failed: %w", err)
	}

	m.log.Info("Created backup: %s", backupPath)
	return backupPath, nil
}

// getAssetName returns the expected asset name for current platform (must match GitHub Release artifacts).
func (m *Module) getAssetName() string {
	name := fmt.Sprintf("media-server-pro-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// getAssetURL gets the download URL for a specific release asset
func (m *Module) getAssetURL(version, assetName string) (string, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/v%s",
		GitHubAPI, GitHubOwner, GitHubRepo, version)

	req, err := m.newGitHubRequest("GET", apiURL)
	if err != nil {
		return "", err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("GitHub API authentication failed (HTTP %d) — configure a GitHub token in admin settings", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("release v%s not found on GitHub — it may not have been published yet", version)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned unexpected status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	// Find matching asset using exact prefix match to avoid ambiguous matches
	// This prevents "preview-media-server-windows-amd64.zip" from matching "media-server-windows-amd64"
	for _, asset := range release.Assets {
		// Check if asset name starts with assetName followed by a valid separator (., -, or end of string)
		if strings.HasPrefix(asset.Name, assetName) {
			remainder := asset.Name[len(assetName):]
			if remainder == "" || remainder[0] == '.' || remainder[0] == '-' {
				return asset.BrowserDownloadURL, nil
			}
		}
	}

	// No matching binary asset found — do NOT fall back to the source archive.
	// Installing a source .tar.gz as a server binary would crash the server on restart.
	return "", fmt.Errorf("no binary release asset matching %q found for v%s — check https://github.com/%s/%s/releases",
		assetName, version, GitHubOwner, GitHubRepo)
}

// downloadWithGhCLI attempts to download a release asset using the `gh` CLI.
// The `gh` CLI uses whatever auth is already configured on the system (token,
// OAuth, etc.) so no separate token configuration is needed in admin settings.
// Returns the path to the downloaded temp file, or an error if gh is unavailable
// or the download fails.
func (m *Module) downloadWithGhCLI(version, assetName string) (string, error) {
	ghPath, err := helpers.FindBinary("gh")
	if err != nil {
		return "", fmt.Errorf("gh CLI not found: %w", err)
	}

	// Download into a dedicated temp directory so we can find the file by name.
	tmpDir, err := os.MkdirTemp("", "media-server-gh-download-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// gh release download automatically selects matching assets by glob pattern.
	// --clobber overwrites any pre-existing file in tmpDir.
	cmd := exec.Command(ghPath, "release", "download", "v"+version,
		"--repo", GitHubOwner+"/"+GitHubRepo,
		"--pattern", assetName+"*",
		"--dir", tmpDir,
		"--clobber",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gh release download failed: %v — %s", err, strings.TrimSpace(string(out)))
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("gh download completed but no file found in temp dir")
	}

	// Move the downloaded file to a stable temp path outside tmpDir
	// (tmpDir is cleaned up in the deferred RemoveAll above).
	downloaded := filepath.Join(tmpDir, entries[0].Name())
	out, err := os.CreateTemp("", "media-server-update-*")
	if err != nil {
		return "", err
	}
	outName := out.Name()
	_ = out.Close()

	if err := copyFile(downloaded, outName); err != nil {
		_ = os.Remove(outName)
		return "", err
	}

	m.log.Info("Downloaded update via gh CLI: %s → %s", entries[0].Name(), outName)
	return outName, nil
}

// downloadUpdate downloads a release asset via direct HTTP, adding GitHub auth
// from admin settings when configured.
func (m *Module) downloadUpdate(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	// GitHub private release assets require authentication to download.
	if token := m.config.Get().Updater.GitHubToken; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.log.Warn("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "media-server-update-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err := tmpFile.Close(); err != nil {
			m.log.Warn("Failed to close temporary file: %v", err)
		}
	}()

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			m.log.Warn("Failed to remove temporary file %s: %v", tmpFile.Name(), removeErr)
		}
		return "", err
	}

	m.log.Info("Downloaded update to %s", tmpFile.Name())
	return tmpFile.Name(), nil
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func findChecksumAssetURL(assets []releaseAsset) string {
	for _, asset := range assets {
		lName := strings.ToLower(asset.Name)
		if lName == "sha256sums" || lName == "sha256sums.txt" || lName == "checksums.txt" || lName == "checksums-sha256.txt" {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func parseExpectedHashFromChecksum(checksumData []byte, assetName string) string {
	for _, line := range strings.Split(string(checksumData), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			// Second field may be path (e.g. artifacts/binaries-linux-amd64/media-server-pro-linux-amd64) or basename
			name := filepath.Base(parts[1])
			if strings.EqualFold(name, assetName) {
				return strings.ToLower(parts[0])
			}
		}
	}
	return ""
}

func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyBinaryChecksum downloads the SHA256SUMS file from the release and
// verifies that the downloaded binary matches the published checksum.
// If no SHA256SUMS asset is available in the release the check is skipped with
// a warning (for backward compatibility with older releases that predate
// checksum publishing).  A mismatch returns a non-nil error and the caller
// must reject the update.
func (m *Module) verifyBinaryChecksum(version, assetName, binaryPath string) error {
	checksumURL2, err := m.fetchChecksumAssetURL(version)
	if err != nil || checksumURL2 == "" {
		return nil // already logged
	}
	expectedHash, err := m.downloadAndParseChecksum(checksumURL2, assetName)
	if err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	if expectedHash == "" {
		m.log.Warn("Asset %q not listed in SHA256SUMS — skipping integrity check", assetName)
		return nil
	}
	actualHash, err := computeFileSHA256(binaryPath)
	if err != nil {
		return fmt.Errorf("open binary for hashing: %w", err)
	}
	if actualHash != expectedHash {
		return fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s — update rejected", assetName, expectedHash, actualHash)
	}
	m.log.Info("Binary integrity verified: SHA256 %s matches published checksum", actualHash[:16]+"…")
	return nil
}

func (m *Module) fetchChecksumAssetURL(version string) (string, error) {
	checksumURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/v%s",
		GitHubAPI, GitHubOwner, GitHubRepo, version)
	req, err := http.NewRequest("GET", checksumURL, nil)
	if err != nil {
		m.log.Warn("Could not build checksum request: %v — skipping", err)
		return "", nil
	}
	if token := m.config.Get().Updater.GitHubToken; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.log.Warn("Could not reach GitHub API for checksum lookup: %v — skipping integrity check", err)
		return "", nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		m.log.Warn("GitHub API returned %d for release lookup — skipping checksum check", resp.StatusCode)
		return "", nil
	}
	var release struct {
		Assets []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		m.log.Warn("Failed to decode release JSON for checksum: %v — skipping", err)
		return "", nil
	}
	url := findChecksumAssetURL(release.Assets)
	if url == "" {
		m.log.Warn("No SHA256SUMS asset found in release v%s — skipping integrity check (add SHA256SUMS to future releases)", version)
	}
	return url, nil
}

func (m *Module) downloadAndParseChecksum(checksumURL2, assetName string) (string, error) {
	cReq, err := http.NewRequest("GET", checksumURL2, nil)
	if err != nil {
		return "", fmt.Errorf("build checksum download request: %w", err)
	}
	if token := m.config.Get().Updater.GitHubToken; token != "" {
		cReq.Header.Set("Authorization", "Bearer "+token)
	}
	cResp, err := m.httpClient.Do(cReq)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	defer func() { _ = cResp.Body.Close() }()
	if cResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SHA256SUMS download returned HTTP %d", cResp.StatusCode)
	}
	checksumData, err := io.ReadAll(io.LimitReader(cResp.Body, 1*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read SHA256SUMS body: %w", err)
	}
	return parseExpectedHashFromChecksum(checksumData, assetName), nil
}

// isValidBinary checks the magic bytes of a file to confirm it is a native executable.
// This prevents accidentally replacing the server with a source archive or wrong asset type.
func isValidBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false
	}

	switch runtime.GOOS {
	case "windows":
		// PE/COFF executables start with "MZ"
		return magic[0] == 'M' && magic[1] == 'Z'
	default:
		// ELF executables start with 0x7f "ELF"
		return magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F'
	}
}

// installUpdate installs the downloaded update
func (m *Module) installUpdate(updateFile string) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	// Validate the file is a real executable before touching the running server.
	// Guards against source archives (.tar.gz) or wrong asset types being installed.
	if !isValidBinary(updateFile) {
		return fmt.Errorf("downloaded file is not a valid %s executable — refusing to replace running server", runtime.GOOS)
	}

	// Replace the current executable — backup old one first so we can restore on failure.
	oldPath := execPath + ".old"
	if err := os.Rename(execPath, oldPath); err != nil {
		return fmt.Errorf("failed to backup current executable: %w", err)
	}

	// Copy new file into place. os.Rename across filesystems fails with EXDEV
	// ("invalid cross-device link") when the tmp dir and exec dir are on different
	// mounts (e.g. /tmp on tmpfs vs /home on ext4), which is common on VPS hosts.
	if err := copyFile(updateFile, execPath); err != nil {
		// Restore old version on failure
		if restoreErr := os.Rename(oldPath, execPath); restoreErr != nil {
			m.log.Error("Failed to restore old executable: %v", restoreErr)
		}
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Apply executable permissions to the installed binary.
	// copyFile uses os.Create which does not copy source permissions.
	if err := os.Chmod(execPath, 0755); err != nil {
		m.log.Warn("Failed to set executable bit on installed binary: %v", err)
	}

	// Remove old version
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		m.log.Warn("Failed to remove old version %s: %v", oldPath, err)
	}

	m.log.Info("Update installed successfully")
	return nil
}

// restoreFromBackup restores from a backup. backupPath must be non-empty (caller should not pass "").
// createBackup produces single-file backups via copyFile only; .tar.gz is not supported.
func (m *Module) restoreFromBackup(backupPath string) error {
	if backupPath == "" {
		return fmt.Errorf("no backup path to restore from")
	}

	// Simple file restore
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	return copyFile(backupPath, execPath)
}

// writeGitAskPass creates a temporary GIT_ASKPASS helper script that echoes the
// token on stdout. The script is only readable by the current user (0700) and
// the caller is responsible for removing it when the git command finishes.
// This avoids embedding credentials in environment variables where they are
// visible to any local user via /proc/<pid>/environ (P0-6).
func writeGitAskPass(token string) (scriptPath string, err error) {
	f, err := os.CreateTemp("", "git-askpass-*.sh")
	if err != nil {
		return "", fmt.Errorf("create askpass script: %w", err)
	}
	// Write a POSIX shell script that prints the token and exits.
	_, err = fmt.Fprintf(f, "#!/bin/sh\necho '%s'\n", strings.ReplaceAll(token, "'", "'\\''"))
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write askpass script: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close askpass script: %w", err)
	}
	if err := os.Chmod(f.Name(), 0700); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("chmod askpass script: %w", err)
	}
	return f.Name(), nil
}

// gitAuthVars returns only the additional environment variables needed for git authentication
// (no base os.Environ). Used by both gitAuthEnv and goModEnv to avoid duplicating the environ.
// cleanup must be called after the git command finishes to remove any temp askpass script.
func (m *Module) gitAuthVars() (vars []string, cleanup func()) {
	cfg := m.config.Get()
	var cleanups []func()
	cleanup = func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	// SSH deploy key — for git@github.com: remote URLs
	if cfg.Updater.DeployKeyPath != "" {
		if _, err := os.Stat(cfg.Updater.DeployKeyPath); err == nil {
			sshCmd := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes",
				cfg.Updater.DeployKeyPath)
			vars = append(vars, "GIT_SSH_COMMAND="+sshCmd)
		} else {
			m.log.Warn("Deploy key not found at %s — SSH auth skipped", cfg.Updater.DeployKeyPath)
		}
	}

	// HTTPS token — use GIT_ASKPASS helper script so credentials never appear in
	// /proc/<pid>/environ (fixes P0-6). The askpass script is a temp file removed
	// by the returned cleanup function.
	if cfg.Updater.GitHubToken != "" {
		askPassPath, err := writeGitAskPass(cfg.Updater.GitHubToken)
		if err != nil {
			m.log.Warn("Failed to create GIT_ASKPASS script: %v — falling back to env vars", err)
			// Fallback: use extraheader (less secure but functional).
			vars = append(vars,
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
				"GIT_CONFIG_VALUE_0=Authorization: Bearer "+cfg.Updater.GitHubToken,
			)
		} else {
			cleanups = append(cleanups, func() { os.Remove(askPassPath) })
			username := cfg.Updater.GitHubUsername
			if username == "" {
				username = "x-access-token"
			}
			vars = append(vars,
				"GIT_ASKPASS="+askPassPath,
				"GIT_TERMINAL_PROMPT=0",
				// Rewrite github.com URLs to include the username so git prompts askpass for the password.
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=url.https://"+username+"@github.com/.insteadOf",
				"GIT_CONFIG_VALUE_0=https://github.com/",
			)
		}
	}

	return vars, cleanup
}

// gitAuthEnv returns os.Environ() augmented with git authentication variables.
// Either or both SSH deploy key and HTTPS token can be configured independently.
// The returned cleanup function removes any temporary askpass scripts.
func (m *Module) gitAuthEnv() ([]string, func()) {
	vars, cleanup := m.gitAuthVars()
	return append(os.Environ(), vars...), cleanup
}

// goModEnv returns environment variable overrides for Go module operations.
// When a GitHub token is configured, sets GOPRIVATE/GONOSUMDB to skip sum verification
// and injects git auth vars so `go mod download` / `go build` can fetch private modules.
// The returned cleanup function removes any temporary askpass scripts.
func (m *Module) goModEnv() ([]string, func()) {
	cfg := m.config.Get()
	if cfg.Updater.GitHubToken == "" {
		return nil, func() {}
	}
	vars := []string{
		"GOPRIVATE=github.com/bradselph/*",
		"GONOSUMDB=github.com/bradselph/*",
	}
	// Include git auth so go build/mod can fetch private GitHub modules.
	authVars, cleanup := m.gitAuthVars()
	vars = append(vars, authVars...)
	return vars, cleanup
}

// newGitHubRequest creates an HTTP request pre-configured with the GitHub API
// Accept header, User-Agent, and (when configured) an Authorization header.
func (m *Module) newGitHubRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "MediaServerPro/"+m.currentVersion)
	if token := m.config.Get().Updater.GitHubToken; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// appDir returns the configured app directory, falling back to the directory
// of the running binary when AppDir is not set.
func (m *Module) appDir() (string, error) {
	cfg := m.config.Get()
	if cfg.Updater.AppDir != "" {
		return cfg.Updater.AppDir, nil
	}
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine app directory: %w", err)
	}
	return filepath.Dir(execPath), nil
}

// CheckForSourceUpdates fetches remote refs for the configured branch and
// reports whether the remote has commits not yet present locally.
// Returns (updatesAvailable, remoteShortHash, error).
func (m *Module) CheckForSourceUpdates(ctx context.Context) (bool, string, error) {
	cfg := m.config.Get()
	dir, err := m.appDir()
	if err != nil {
		return false, "", err
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false, "", fmt.Errorf("not a git repository: %s", dir)
	}

	branch := cfg.Updater.Branch
	if branch == "" {
		branch = "main"
	}

	gitEnv, gitCleanup := m.gitAuthEnv()
	defer gitCleanup()

	// Fetch only the configured branch
	fetchCmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--quiet", "origin", branch)
	fetchCmd.Env = gitEnv
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return false, "", fmt.Errorf("git fetch failed: %w\n%s", err, string(out))
	}

	// Local HEAD commit
	localOut, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return false, "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}

	// Remote branch commit
	remoteOut, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "origin/"+branch).Output()
	if err != nil {
		return false, "", fmt.Errorf("git rev-parse origin/%s failed: %w", branch, err)
	}

	localHash := strings.TrimSpace(string(localOut))
	remoteHash := strings.TrimSpace(string(remoteOut))

	hasUpdates := localHash != remoteHash
	short := remoteHash
	if len(short) > 8 {
		short = short[:8]
	}
	return hasUpdates, short, nil
}

// publishBuildStatus stores a shallow copy of s in activeBuild so the
// polling endpoint always sees the latest stage without blocking on the build.
func (m *Module) publishBuildStatus(s *UpdateStatus) {
	snap := *s // value copy — caller may mutate s further
	m.buildMu.Lock()
	m.activeBuild = &snap
	m.buildMu.Unlock()
}

// GetActiveBuildStatus returns the live status of a running (or recently
// completed) source build, or nil if no build has been started yet.
func (m *Module) GetActiveBuildStatus() *UpdateStatus {
	m.buildMu.Lock()
	defer m.buildMu.Unlock()
	if m.activeBuild == nil {
		return nil
	}
	return new(*m.activeBuild)
}

// IsBuildRunning reports whether a source build is currently in progress.
func (m *Module) IsBuildRunning() bool {
	m.buildMu.RLock()
	defer m.buildMu.RUnlock()
	return m.activeBuild != nil && m.activeBuild.InProgress
}

// IsUpdateRunning reports whether a binary update install is currently in progress.
func (m *Module) IsUpdateRunning() bool {
	m.applyMu.RLock()
	defer m.applyMu.RUnlock()
	return m.applyRunning
}

// SourceUpdate performs a full source-based update:
//  1. git pull (using the deploy key if configured)
//  2. npm ci + npm run build  (rebuilds React frontend)
//  3. go build               (compiles new binary to a temp path)
//  4. atomic rename          (replaces running binary on disk)
//
// The caller is responsible for restarting the service after this returns.
// Only one SourceUpdate runs at a time; concurrent callers get "already in progress".
func (m *Module) SourceUpdate(ctx context.Context) (*UpdateStatus, error) {
	status := &UpdateStatus{
		InProgress: true,
		Stage:      "starting",
		Progress:   0,
		StartedAt:  time.Now(),
	}
	m.buildMu.Lock()
	if m.activeBuild != nil && m.activeBuild.InProgress {
		m.buildMu.Unlock()
		return &UpdateStatus{Error: "a source build is already in progress", InProgress: false},
			fmt.Errorf("a source build is already in progress")
	}
	m.activeBuild = status
	m.buildMu.Unlock()

	cfg := m.config.Get()
	dir, err := m.appDir()
	if err != nil {
		m.publishBuildStatus(&UpdateStatus{Error: err.Error(), InProgress: false})
		return &UpdateStatus{Error: err.Error(), InProgress: false}, err
	}

	branch := cfg.Updater.Branch
	if branch == "" {
		branch = "main"
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		e := fmt.Errorf("not a git repository: %s", dir)
		m.publishBuildStatus(&UpdateStatus{Error: e.Error(), InProgress: false})
		return &UpdateStatus{Error: e.Error(), InProgress: false}, e
	}

	status.Stage = "creating backup"
	status.Progress = 5
	m.publishBuildStatus(status)

	// Backup the current binary before making any changes
	if backupPath, berr := m.createBackup(); berr != nil {
		m.log.Warn("Could not create backup before source update: %v", berr)
	} else {
		status.BackupPath = backupPath
	}

	gitEnv, gitCleanup := m.gitAuthEnv()
	defer gitCleanup()

	// --- Step 1: fetch + checkout + reset ---
	// Using fetch → checkout → reset --hard instead of plain "git pull" so that:
	//  - The correct branch is always checked out (even if the repo has a different branch)
	//  - No merge conflicts are possible (hard reset discards local state)
	//  - config.json and other gitignored files are untouched
	status.Stage = "pulling source"
	status.Progress = 20
	m.publishBuildStatus(status)
	m.log.Info("Source update: fetching origin/%s in %s", branch, dir)

	fetchCmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "origin", branch)
	fetchCmd.Env = gitEnv
	fetchOut, err := fetchCmd.CombinedOutput()
	if err != nil {
		status.Error = fmt.Sprintf("git fetch failed: %v\n%s", err, string(fetchOut))
		status.InProgress = false
		m.publishBuildStatus(status)
		return status, fmt.Errorf("git fetch: %w", err)
	}
	m.log.Info("git fetch: %s", strings.TrimSpace(string(fetchOut)))

	// Compare local branch tip with origin/branch; if equal, no new commits — skip build.
	localOut, _ := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", branch).Output()
	remoteOut, _ := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "origin/"+branch).Output()
	if strings.TrimSpace(string(localOut)) == strings.TrimSpace(string(remoteOut)) {
		m.log.Info("Source update: already up to date")
		status.Stage = "already up to date"
		status.Progress = 100
		status.InProgress = false
		m.publishBuildStatus(status)
		return status, nil
	}

	// Switch to the target branch (create tracking branch if needed)
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "-B", branch, "origin/"+branch)
	checkoutCmd.Env = gitEnv
	if out, cerr := checkoutCmd.CombinedOutput(); cerr != nil {
		status.Error = fmt.Sprintf("git checkout failed: %v\n%s", cerr, string(out))
		status.InProgress = false
		m.publishBuildStatus(status)
		return status, fmt.Errorf("git checkout: %w", cerr)
	}

	// --- Step 2: npm build (frontend) ---
	status.Stage = "installing frontend dependencies"
	status.Progress = 35
	m.publishBuildStatus(status)

	frontendDir := filepath.Join(dir, "web", "frontend")
	if _, err := os.Stat(frontendDir); err == nil {
		m.log.Info("Source update: npm ci in %s", frontendDir)
		// Use npm ci without --prefer-offline so it works on fresh servers with no cache
		npmCi := exec.CommandContext(ctx, "npm", "ci", "--no-audit", "--no-fund")
		npmCi.Dir = frontendDir
		if out, err := npmCi.CombinedOutput(); err != nil {
			// Non-fatal: log and continue — frontend may already be built
			m.log.Warn("npm ci failed (continuing): %v\n%s", err, string(out))
		}

		status.Stage = "building frontend"
		status.Progress = 50
		m.publishBuildStatus(status)

		m.log.Info("Source update: npm run build")
		npmBuild := exec.CommandContext(ctx, "npm", "run", "build")
		npmBuild.Dir = frontendDir
		if out, err := npmBuild.CombinedOutput(); err != nil {
			status.Error = fmt.Sprintf("frontend build failed: %v", err)
			status.InProgress = false
			m.publishBuildStatus(status)
			m.log.Error("npm run build failed: %v\n%s", err, string(out))
			return status, fmt.Errorf("npm build: %w", err)
		}
		m.log.Info("Source update: frontend built successfully")
	}

	// --- Step 3: go build ---
	status.Stage = "building server"
	status.Progress = 65
	m.publishBuildStatus(status)

	execPath, err := os.Executable()
	if err != nil {
		status.Error = "cannot determine executable path"
		status.InProgress = false
		return status, err
	}

	// Resolve the Go binary — may not be on PATH for the server process
	goBin, err := exec.LookPath("go")
	if err != nil {
		for _, candidate := range []string{"/usr/local/go/bin/go", "/usr/bin/go"} {
			if _, serr := os.Stat(candidate); serr == nil {
				goBin = candidate
				err = nil
				break
			}
		}
		if err != nil {
			status.Error = "go binary not found in PATH or /usr/local/go/bin"
			status.InProgress = false
			m.publishBuildStatus(status)
			return status, fmt.Errorf("go not found: %w", err)
		}
	}

	tmpBin := execPath + ".new"
	buildDate := time.Now().Format("2006-01-02")

	// Read the VERSION file from the repo root to stamp the built binary correctly.
	newVersion := ""
	if vData, verr := os.ReadFile(filepath.Join(dir, "VERSION")); verr == nil {
		newVersion = strings.TrimSpace(string(vData))
	}
	if newVersion == "" {
		newVersion = buildDate // fallback: use build date if VERSION file missing
	}

	// Determine whether to use vendor/ or download modules
	goModFlag := ""
	if _, serr := os.Stat(filepath.Join(dir, "vendor")); serr == nil {
		goModFlag = "-mod=vendor"
		m.log.Info("Source update: using vendored Go dependencies")
	} else {
		// No vendor/ — download modules (works on any internet-connected host)
		status.Stage = "downloading Go dependencies"
		status.Progress = 60
		m.publishBuildStatus(status)
		goDownload := exec.CommandContext(ctx, goBin, "mod", "download")
		goDownload.Dir = dir
		goModVars, goModCleanup := m.goModEnv()
		goDownload.Env = append(os.Environ(), goModVars...)
		defer goModCleanup()
		if out, merr := goDownload.CombinedOutput(); merr != nil {
			m.log.Warn("go mod download failed (continuing): %v\n%s", merr, string(out))
		}
		status.Stage = "building server"
		status.Progress = 65
		m.publishBuildStatus(status)
	}

	buildArgs := []string{"build"}
	if goModFlag != "" {
		buildArgs = append(buildArgs, goModFlag)
	}
	buildArgs = append(buildArgs,
		"-ldflags", fmt.Sprintf("-s -w -X main.Version=%s -X main.BuildDate=%s", newVersion, buildDate),
		"-o", tmpBin,
		"./cmd/server",
	)

	m.log.Info("Source update: go build -o %s ./cmd/server", tmpBin)
	buildCmd := exec.CommandContext(ctx, goBin, buildArgs...)
	buildCmd.Dir = dir
	goBuildModVars, goBuildModCleanup := m.goModEnv()
	buildCmd.Env = append(os.Environ(), goBuildModVars...)
	defer goBuildModCleanup()
	if out, err := buildCmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmpBin)
		status.Error = fmt.Sprintf("go build failed: %v\n%s", err, string(out))
		status.InProgress = false
		m.publishBuildStatus(status)
		m.log.Error("go build failed: %v\n%s", err, string(out))
		return status, fmt.Errorf("go build: %w", err)
	}

	// --- Step 4: atomic replace ---
	status.Stage = "installing"
	status.Progress = 85
	m.publishBuildStatus(status)

	if err := os.Chmod(tmpBin, 0755); err != nil {
		_ = os.Remove(tmpBin)
		status.Error = "failed to set binary permissions"
		status.InProgress = false
		m.publishBuildStatus(status)
		return status, err
	}

	oldBin := execPath + ".old"
	if err := os.Rename(execPath, oldBin); err != nil {
		_ = os.Remove(tmpBin)
		status.Error = fmt.Sprintf("failed to rename current binary: %v", err)
		status.InProgress = false
		m.publishBuildStatus(status)
		return status, err
	}
	if err := os.Rename(tmpBin, execPath); err != nil {
		// Restore old binary on failure
		if rerr := os.Rename(oldBin, execPath); rerr != nil {
			m.log.Error("Critical: failed to restore old binary after install error: %v", rerr)
		}
		status.Error = fmt.Sprintf("failed to install new binary: %v", err)
		status.InProgress = false
		m.publishBuildStatus(status)
		return status, err
	}
	_ = os.Remove(oldBin)

	status.Stage = "completed — restart required"
	status.Progress = 100
	status.InProgress = false
	m.publishBuildStatus(status)
	m.log.Info("Source update completed successfully. Service restart required.")
	return status, nil
}

// GetVersion returns the current version info
func (m *Module) GetVersion() map[string]interface{} {
	return map[string]interface{}{
		"version":    m.currentVersion,
		"go_version": runtime.Version(),
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := source.Close(); err != nil {
			_ = err // best-effort close
		}
	}()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if err := dest.Close(); err != nil {
			_ = err // best-effort close
		}
	}()

	_, err = io.Copy(dest, source)
	return err
}
