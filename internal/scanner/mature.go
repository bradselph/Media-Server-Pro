// Package scanner provides content scanning including mature content detection.
package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/huggingface"
	"media-server-pro/pkg/models"
)

// High confidence keywords (0.90 boost) - explicit adult terms, studio/site names
// Increased boost from 0.85 to 0.90 for stricter detection
var highConfidenceKeywords = []string{
	// Explicit content terms
	"xxx", "porn", "porno", "pornstar", "pornographic",
	"hardcore", "gangbang", "creampie", "cumshot", "blowjob", "handjob",
	"anal sex", "deepthroat", "threesome", "foursome", "orgy", "orgasm",
	"bukkake", "facesitting", "footjob", "titjob", "titfuck",
	"doggystyle", "cowgirl", "reverse cowgirl",
	"squirting", "fisting", "pegging",
	"milf", "gilf", "dilf", "stepmom", "stepdad", "stepsis", "stepbro",
	"stepsister", "stepbrother", "stepmother", "stepfather",
	"cuckold", "hotwife", "swinger", "swingers",
	"bdsm", "bondage", "fetish", "domination", "submissive",
	"dominatrix", "mistress", "dungeon", "spanking",
	"hentai", "yaoi", "yuri", "ecchi", "ahegao", "doujin", "doujinshi",
	"futanari", "tentacle", "lolicon", "shotacon",
	"nsfw", "18+", "adults only", "18 only",

	// Additional explicit terms
	"double penetration", "triple penetration", "airtight",
	"facial", "money shot", "cumswap", "snowball",
	"dildo", "vibrator", "sex toy", "sextoy", "fleshlight",
	"gloryhole", "glory hole", "rimjob", "rimming", "anilingus",
	"strapon", "strap-on", "strap on",
	"threeway", "three way", "fourway", "four way",
	"swinging", "wife swap", "wife swapping", "partner swap",
	"rough sex", "rough", "choking", "slapping",
	"cumslut", "cum slut", "whore", "slut", "bitch",
	"ass to mouth", "atm",

	// Major adult sites and studios
	"pornhub", "xvideos", "xhamster", "xnxx", "redtube", "youporn",
	"spankbang", "eporner", "tube8", "xtube", "tnaflix", "drtuber",
	"txxx", "hqporner", "beeg", "thumbzilla",
	"brazzers", "bangbros", "realitykings", "naughtyamerica", "blacked",
	"tushy", "vixen", "mofos", "fakehub", "teamskeet",
	"digitalplayground", "evilangel",
	"publicagent", "faketaxi", "fakehospital",
	"girlsway", "sweetsinner", "puretaboo", "familystrokes",
	"sislovesme", "dontbreakme", "teenpies", "exploitedteens",
	"castingcouch", "backroomcastingcouch", "woodmancastingx",
	"legalporno", "gonzo", "julesjordan", "manuelferrara",
	"blowpass", "milehigh", "adulttime", "letsdoeit",
	"dorcelclub", "vivid", "penthouse",
	"metart", "sexart", "femjoy", "hegre", "atkgalleria",
	"twistys", "nubilefilms", "nubiles", "x-art", "xart",
	"japanesebeauties", "caribbeancom", "tokyohot", "heyzo",
	"javhd", "uncensored jav", "1pondo", "fc2",
	"onlyfans", "fansly", "manyvids", "chaturbate", "myfreecams",
	"cam4", "camsoda", "livejasmin", "bongacams", "stripchat",

	// Additional studios and sites
	"kink.com", "kink", "sexandsubmission",
	"wickedpictures", "wicked", "newsensations",
	"devilsfilm", "devils film", "girlfriendsfilms",
	"cherrypimps", "cherry pimps",
	"hustler", "hustlerworld",
}

// Medium confidence keywords (0.40 boost) - suggestive/contextual terms
// NOTE: Since matching uses strings.Contains(), avoid short/generic words
// that appear as substrings of common words (e.g., "ass" matches "class").
// Increased boost from 0.35 to 0.40 for stricter detection
var mediumConfidenceKeywords = []string{
	// Suggestive content descriptors
	"adult", "mature", "explicit", "erotic", "erotica", "sensual",
	"risque", "provocative", "raunchy", "sleazy", "smut", "smutty",
	"lewd", "lustful", "naughty", "kinky", "taboo", "forbidden",
	"indecent", "obscene", "vulgar", "crude", "filthy",
	"steamy", "sultry", "salacious",

	// Nudity-related
	"nude", "nudity", "naked", "topless", "bottomless", "fullnude",
	"nudist", "naturist", "naturism",
	"bare", "exposed", "undressed", "disrobed",

	// Suggestive actions/scenarios
	"sexy", "seduction", "seduce", "seductive",
	"stripper", "striptease", "stripclub", "lapdance", "lap dance",
	"pole dance", "poledance", "table dance",
	"escort", "callgirl", "call girl", "courtesan", "gigolo",
	"hookup", "booty call", "one night stand", "casual sex",
	"voyeur", "upskirt", "downblouse", "voyeurism",
	"exhibitionist", "exhibitionism", "flashing",
	"groping", "fondling", "caressing",
	"makeout", "making out", "heavy petting", "petting",
	"intimacy", "intimate", "passion", "passionate",

	// Publications and brands
	"playboy", "playmate", "hustler", "penthouse",
	"suicidegirls", "suicide girls", "zishy", "met-art",
	"barely legal", "barely18", "barely 18", "just18", "just 18",
	"eighteen plus", "18plus",

	// Clothing/appearance
	"lingerie", "bikini", "underwear", "panties", "bra",
	"thong", "g-string", "gstring", "corset", "garter",
	"fishnets", "stockings", "negligee", "babydoll", "chemise",
	"catsuit", "bodysuit", "see-through", "seethrough", "sheer", "transparent",
	"revealing", "skimpy", "tight", "form fitting",

	// Romantic/intimate
	"lovemaking", "love making", "intercourse", "coitus",
	"foreplay", "afterglow",

	// Release/cut markers
	"uncensored", "uncut", "unrated", "directors cut", "extended cut",
	"unedited", "unexpurgated", "full version", "complete version",

	// Body-focused terms (only those unlikely to be substrings of common words)
	"boobs", "booty", "cleavage", "busty", "curvy",
	"voluptuous", "thicc", "pawg",
	"hottie", "hotties", "bombshell", "stunner",
	"curves", "shapely", "buxom",

	// Rating/age markers
	"rated r", "rated x", "nc17", "nc-17", "r rated", "x rated",
	"age restricted", "age verification", "18 and over", "21 and over",
	"for adults", "adult content", "viewer discretion", "mature audiences",
	"parental advisory", "not for children", "adult only",

	// Additional suggestive terms
	"bedroom", "bedtime", "pillow talk",
	"affair", "infidelity", "cheating",
	"temptation", "seductress", "temptress",
	"pleasure", "satisfaction", "desire",
}

// compiledKeywordPatterns holds pre-compiled word-boundary regexp patterns for
// each keyword list so that scanning uses true word-boundary matching instead
// of strings.Contains().  Patterns are built once at package init time.
// Word-boundary matching prevents false positives like "ass" matching "class".
var (
	compiledHighConf []*compiledKeyword
	compiledMedConf  []*compiledKeyword
)

type compiledKeyword struct {
	raw     string
	pattern *regexp.Regexp
}

// buildKeywordPatterns compiles a keyword list into filename-aware boundary patterns.
//
// Two issues prevent standard \b from working correctly for filenames:
//  1. regexp.QuoteMeta does not escape spaces, so the `\ ` → `[\s_\-]?` replacement
//     never fired with the old code.
//  2. Go's regexp \b treats underscore as a word character, so \bxxx\b fails to
//     match "xxx_video.mp4" even though _ is used as a word separator in filenames.
//
// The fix uses explicit left/right boundary groups: `(?:^|[^a-z0-9])` and
// `(?:[^a-z0-9]|$)`.  These treat any non-alphanumeric character (_, -, ., space,
// and file-extension dots) as a token separator while still blocking substring
// matches inside longer words (e.g. "ass" does not match inside "grasslands").
//
// Phrase keywords ("lap dance") have their spaces replaced with `[\s_\-]?` so they
// match common filename variants: "lap dance", "lap-dance", "lap_dance", "lapdance".
func buildKeywordPatterns(keywords []string) []*compiledKeyword {
	compiled := make([]*compiledKeyword, 0, len(keywords))
	for _, kw := range keywords {
		lower := strings.ToLower(kw)
		// Escape regex metacharacters in the keyword.  Note: regexp.QuoteMeta does
		// NOT escape spaces, so the literal space character is used below.
		escaped := regexp.QuoteMeta(lower)
		// Replace literal spaces with a flexible separator so phrase keywords match
		// common filename representations with -, _, or no separator.
		flexible := strings.ReplaceAll(escaped, " ", `[\s_\-]?`)
		// Filename-aware boundary: non-alphanumeric character or start/end of string.
		// This lets "xxx" match in "xxx_video.mp4" while blocking "ass" in "class".
		pattern := `(?i)(?:^|[^a-z0-9])` + flexible + `(?:[^a-z0-9]|$)`
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Fallback to a simple case-insensitive literal if the pattern is invalid.
			re = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(lower))
		}
		compiled = append(compiled, &compiledKeyword{raw: kw, pattern: re})
	}
	return compiled
}

func init() {
	compiledHighConf = buildKeywordPatterns(highConfidenceKeywords)
	compiledMedConf = buildKeywordPatterns(mediumConfidenceKeywords)
}

// Directory patterns that may indicate mature content.
// NOTE: These are matched with strings.Contains() against directory paths,
// so avoid short/generic words like "av" (matches "avatars"), "hot" (matches
// "hotel"), "private"/"hidden"/"secret" (common generic folder names).
var directoryPatterns = []string{
	"adult", "adults", "18+", "21+", "xxx", "porn", "nsfw", "mature",
	"explicit", "erotic", "erotica", "r-rated", "x-rated", "unrated",
	"hentai", "ecchi", "nude", "nudes", "nudity",
	"fetish", "bdsm", "kinky", "taboo",
	"uncensored", "uncut",
	"onlyfans", "fansly", "manyvids",
	"smut", "lewd", "risque", "raunchy",
	"grownup", "grown-up", "not for kids",
	// Common adult content organization patterns
	"adult videos", "adult movies", "adult content",
	"mature content", "mature videos", "mature movies",
	"explicit content", "explicit videos",
	"porn collection", "porn stash",
	"nsfw collection", "nsfw content",
	// Site/studio specific folders
	"brazzers", "bangbros", "realitykings", "naughtyamerica",
	"pornhub", "xvideos", "xhamster",
	// Category folders
	"amateur", "homemade", "professional",
	"webcam", "camgirl", "camgirls", "cam girls",
}

// MatureScanner detects mature/adult content
type MatureScanner struct {
	config      *config.Manager
	log         *logger.Logger
	dbModule    *database.Module
	results     map[string]*ScanResult
	reviewQueue map[string]*models.MatureReviewItem
	mu          sync.RWMutex
	dataDir     string
	tempDir     string              // for HF frame extraction
	hfClient    *huggingface.Client // nil if HuggingFace not configured
	healthy     bool
	healthMsg   string
	healthMu    sync.RWMutex
	scanRepo    repositories.ScanResultRepository // Repository for persistent scan results
	repoDown    atomic.Bool                       // Set true after consecutive repo failures to suppress spam
	repoErrors  atomic.Int32                      // Consecutive repo error count
}

// ScanResult holds the result of scanning a file
type ScanResult struct {
	Path            string     `json:"path"`
	IsMature        bool       `json:"is_mature"`
	Confidence      float64    `json:"confidence"`
	Reasons         []string   `json:"reasons"`
	AutoFlagged     bool       `json:"auto_flagged"`
	NeedsReview     bool       `json:"needs_review"`
	ScannedAt       time.Time  `json:"scanned_at"`
	ReviewedBy      string     `json:"reviewed_by,omitempty"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	ReviewDecision  string     `json:"review_decision,omitempty"`
	HighConfMatches []string   `json:"high_conf_matches,omitempty"`
	MedConfMatches  []string   `json:"med_conf_matches,omitempty"`
}

// NewMatureScanner creates a new mature content scanner
func NewMatureScanner(cfg *config.Manager) *MatureScanner {
	dirs := cfg.Get().Directories
	tempDir := dirs.Temp
	if tempDir == "" {
		tempDir = filepath.Join(dirs.Data, "temp")
	}
	return &MatureScanner{
		config:      cfg,
		log:         logger.New("scanner"),
		results:     make(map[string]*ScanResult),
		reviewQueue: make(map[string]*models.MatureReviewItem),
		dataDir:     dirs.Data,
		tempDir:     tempDir,
	}
}

// Name returns the module name
func (s *MatureScanner) Name() string {
	return "scanner"
}

// Start initializes the scanner
func (s *MatureScanner) Start(_ context.Context) error {
	s.log.Info("Starting mature content scanner...")

	// Initialize MySQL repository (database is required)
	if s.dbModule == nil || !s.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}
	s.scanRepo = mysql.NewScanResultRepository(s.dbModule.GORM())
	s.log.Info("Using MySQL repository for scan results")

	// Log configuration
	cfg := s.config.Get()
	s.log.Info("Scanner configuration:")
	s.log.Info("  High confidence threshold: %.2f (flags as mature)", cfg.MatureScanner.HighConfidenceThreshold)
	s.log.Info("  Medium confidence threshold: %.2f (requires review)", cfg.MatureScanner.MediumConfidenceThreshold)
	s.log.Info("  Auto-flag enabled: %v", cfg.MatureScanner.AutoFlag)
	s.log.Info("  Require review: %v", cfg.MatureScanner.RequireReview)
	s.log.Info("  High-confidence keywords: %d built-in + %d custom", len(highConfidenceKeywords), len(cfg.MatureScanner.HighConfidenceKeywords))
	s.log.Info("  Medium-confidence keywords: %d built-in + %d custom", len(mediumConfidenceKeywords), len(cfg.MatureScanner.MediumConfidenceKeywords))
	s.log.Info("  Directory patterns: %d", len(directoryPatterns))
	s.log.Info("  Regex patterns: %d", len(maturePatterns))

	// Load existing scan results
	if err := s.loadResults(); err != nil {
		s.log.Warn("Failed to load scan results: %v", err)
	} else {
		s.mu.RLock()
		s.log.Info("Loaded %d previous scan results", len(s.results))
		s.mu.RUnlock()
	}

	// Load review queue
	if err := s.loadReviewQueue(); err != nil {
		s.log.Warn("Failed to load review queue: %v", err)
	} else {
		s.mu.RLock()
		s.log.Info("Loaded %d items in review queue", len(s.reviewQueue))
		s.mu.RUnlock()
	}

	s.healthMu.Lock()
	s.healthy = true
	s.healthMsg = "Running"
	s.healthMu.Unlock()
	s.log.Info("Mature content scanner started with STRICT detection enabled")
	return nil
}

// Stop gracefully stops the scanner
func (s *MatureScanner) Stop(_ context.Context) error {
	s.log.Info("Stopping mature content scanner...")

	s.healthMu.Lock()
	s.healthy = false
	s.healthMsg = "Stopped"
	s.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (s *MatureScanner) Health() models.HealthStatus {
	s.healthMu.RLock()
	healthy := s.healthy
	msg := s.healthMsg
	s.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      s.Name(),
		Status:    helpers.StatusString(healthy),
		Message:   msg,
		CheckedAt: time.Now(),
	}
}

// ResetRepoState clears the repo-down flag so the next scan cycle retries DB saves.
func (s *MatureScanner) ResetRepoState() {
	s.repoDown.Store(false)
	s.repoErrors.Store(0)
}

// ScanFile scans a file for mature content and persists the result.
func (s *MatureScanner) ScanFile(path string) *ScanResult {
	result := s.scanFileInternal(path)
	if result == nil {
		return nil
	}

	// Save to repository for persistent cache (skip if repo is down to avoid log spam)
	if s.scanRepo != nil && !s.repoDown.Load() {
		repoResult := s.convertScannerToRepo(result)
		if err := s.scanRepo.Save(context.Background(), repoResult); err != nil {
			errCount := s.repoErrors.Add(1)
			if errCount <= 1 {
				s.log.Error("Failed to save scan result to repository: %v", err)
			}
			if errCount >= 3 {
				s.log.Error("Repository unavailable after %d consecutive errors, skipping repo saves for this scan cycle", errCount)
				s.repoDown.Store(true)
			}
		} else {
			s.repoErrors.Store(0)
		}
	}

	return result
}

// scanFileInternal performs the actual scan without persisting.
func (s *MatureScanner) scanFileInternal(path string) *ScanResult {
	s.log.Debug("→ Scanning: %s", filepath.Base(path))

	// Check repository first for persistent cache (only if repo is ready)
	if s.scanRepo != nil {
		if repoResult, err := s.scanRepo.Get(context.Background(), path); err == nil {
			// Parse scanned_at timestamp
			scannedAt, err := time.Parse(time.RFC3339, repoResult.ScannedAt)
			if err == nil {
				// Check if file has been modified since last scan
				if info, err := os.Stat(path); err == nil && !info.ModTime().After(scannedAt) {
					// Skip scanning if already reviewed/flagged and file hasn't been modified
					// This prevents re-scanning content that has already been processed by an admin
					if repoResult.ReviewedBy != "" || repoResult.ReviewDecision != "" || repoResult.IsMature {
						s.log.Debug("  Skipping scan - already processed in repository (reviewed: %v, decision: %v, flagged: %v)",
							repoResult.ReviewedBy != "", repoResult.ReviewDecision != "", repoResult.IsMature)
						return s.convertRepoToScanner(repoResult)
					}
					// For unreviewed content, still use cache to avoid redundant scans
					s.log.Debug("  Using repository cached scan result (scanned: %v)", scannedAt.Format("2006-01-02 15:04"))
					return s.convertRepoToScanner(repoResult)
				}
			}
		}
	}

	// Check in-memory cache as secondary fallback
	s.mu.RLock()
	if existing, ok := s.results[path]; ok {
		// Skip scanning if already reviewed/flagged and file hasn't been modified
		if info, err := os.Stat(path); err == nil && !info.ModTime().After(existing.ScannedAt) {
			// Always use cached result if file hasn't been modified, regardless of age
			// This prevents re-scanning reviewed or flagged content
			if existing.ReviewedBy != "" || existing.ReviewDecision != "" || existing.IsMature {
				s.mu.RUnlock()
				s.log.Debug("  Skipping scan - already processed (reviewed: %v, decision: %v, flagged: %v)",
					existing.ReviewedBy != "", existing.ReviewDecision != "", existing.IsMature)
				return existing
			}
			// For unreviewed content, use cache if less than 24 hours old
			if time.Since(existing.ScannedAt) < 24*time.Hour {
				s.mu.RUnlock()
				s.log.Debug("  Using in-memory cached scan result (age: %v)", time.Since(existing.ScannedAt).Round(time.Minute))
				return existing
			}
		}
	}
	s.mu.RUnlock()

	result := &ScanResult{
		Path:      path,
		ScannedAt: time.Now(),
		Reasons:   make([]string, 0),
	}

	filename := strings.ToLower(filepath.Base(path))
	dirPath := strings.ToLower(filepath.Dir(path))

	s.log.Debug("  Analyzing: %s", filename)
	confidence := s.computeConfidence(filename, dirPath, result)
	result.Confidence = confidence

	s.applyThresholds(result)

	// Store result
	s.mu.Lock()
	s.results[path] = result
	s.mu.Unlock()

	// Add to review queue if needed, but only if not already reviewed
	// This prevents re-flagging content that has already been reviewed by an admin
	if result.NeedsReview && result.ReviewedBy == "" {
		s.addToReviewQueue(result)
		s.log.Info("  ⚠ NEEDS REVIEW: %s (confidence: %.2f)", filepath.Base(path), confidence)
	} else if result.NeedsReview && result.ReviewedBy != "" {
		s.log.Debug("  ○ Already reviewed by %s, skipping review queue", result.ReviewedBy)
	}

	if result.IsMature {
		s.log.Info("  ✓ FLAGGED AS MATURE: %s (confidence: %.2f, auto-flagged: %v)",
			filepath.Base(path), confidence, result.AutoFlagged)
		if len(result.Reasons) > 0 {
			s.log.Debug("    Reasons: %v", result.Reasons)
		}
	} else {
		s.log.Debug("  ○ Clean: %s (confidence: %.2f)", filepath.Base(path), confidence)
	}

	return result
}

// computeConfidence calculates the total mature-content confidence score for a file.
func (s *MatureScanner) computeConfidence(filename, dirPath string, result *ScanResult) float64 {
	confidence := scanHighConfidenceKeywords(filename, result)
	confidence += scanMediumConfidenceKeywords(filename, result)
	confidence += scanDirectoryPatterns(dirPath, result)
	confidence += scanMatureRegexPatterns(filename, result)
	confidence += scanFilenameStructure(filename, dirPath, result)

	// Also check custom keywords from config (scanConfigKeywords deduplicates against hardcoded matches)
	// Updated to use 0.90 and 0.40 boost values (from 0.85 and 0.35) for stricter detection
	cfg := s.config.Get()
	confidence += scanConfigKeywords(filename, cfg.MatureScanner.HighConfidenceKeywords, 0.90, "Config HIGH-CONF", result)
	confidence += scanConfigKeywords(filename, cfg.MatureScanner.MediumConfidenceKeywords, 0.40, "Config MED-CONF", result)

	// Log the total confidence score for debugging
	s.log.Debug("Confidence score for %s: %.2f (high matches: %d, med matches: %d, reasons: %d)",
		filename, confidence, len(result.HighConfMatches), len(result.MedConfMatches), len(result.Reasons))

	if confidence > 1.0 {
		confidence = 1.0
	}
	return confidence
}

// TODO: scanConfigKeywords uses strings.Contains() for custom keywords, which does NOT
// apply word-boundary matching like the built-in keyword lists (compiledHighConf/compiledMedConf).
// This inconsistency means custom keywords will produce false positives (e.g., custom keyword "ass"
// will match "class"). Should use buildKeywordPatterns() or equivalent boundary-aware matching
// for user-configured keywords to match the behavior of built-in keywords.

// scanConfigKeywords checks the filename against user-configured keywords.
func scanConfigKeywords(filename string, keywords []string, boost float64, label string, result *ScanResult) float64 {
	var confidence float64
	for _, keyword := range keywords {
		kw := strings.ToLower(keyword)
		if strings.Contains(filename, kw) {
			// Skip if already matched by hardcoded keywords (avoid double counting)
			alreadyMatched := false
			for _, m := range result.HighConfMatches {
				if m == kw {
					alreadyMatched = true
					break
				}
			}
			if !alreadyMatched {
				for _, m := range result.MedConfMatches {
					if m == kw {
						alreadyMatched = true
						break
					}
				}
			}
			if !alreadyMatched {
				confidence += boost
				result.Reasons = append(result.Reasons, label+": "+kw)
				// Log custom keyword matches for debugging
				// Note: Uncomment for debugging if needed
				// s.log.Debug("Custom keyword match: %s (boost: %.2f)", kw, boost)
			}
		}
	}
	return confidence
}

// scanHighConfidenceKeywords checks the filename against high-confidence keywords
// using pre-compiled word-boundary regex patterns to avoid false positives such
// as "ass" matching "class" or "breast" matching "abreast".
// A single high-confidence match is enough to flag content as mature.
func scanHighConfidenceKeywords(filename string, result *ScanResult) float64 {
	var confidence float64
	lower := strings.ToLower(filename)
	for _, ck := range compiledHighConf {
		if ck.pattern.MatchString(lower) {
			confidence += 0.90
			result.HighConfMatches = append(result.HighConfMatches, ck.raw)
			result.Reasons = append(result.Reasons, "HIGH-CONF keyword: "+ck.raw)
		}
	}
	return confidence
}

// scanMediumConfidenceKeywords checks the filename against medium-confidence keywords
// using pre-compiled word-boundary regex patterns.
func scanMediumConfidenceKeywords(filename string, result *ScanResult) float64 {
	var confidence float64
	lower := strings.ToLower(filename)
	for _, ck := range compiledMedConf {
		if ck.pattern.MatchString(lower) {
			confidence += 0.40
			result.MedConfMatches = append(result.MedConfMatches, ck.raw)
			result.Reasons = append(result.Reasons, "MED-CONF keyword: "+ck.raw)
		}
	}
	return confidence
}

// scanDirectoryPatterns checks the directory path against known mature directory patterns.
// Uses word-boundary matching on path segments to avoid false positives.
// Increased boost to 0.45 (from 0.4) for stricter detection.
func scanDirectoryPatterns(dirPath string, result *ScanResult) float64 {
	var confidence float64
	lowerPath := strings.ToLower(dirPath)

	// Split into path segments to match whole directory names
	pathParts := strings.Split(lowerPath, string(filepath.Separator))

	for _, pattern := range directoryPatterns {
		for _, part := range pathParts {
			// Match exact segment or check with word boundaries (spaces around)
			if part == pattern || strings.Contains(" "+part+" ", " "+pattern+" ") {
				confidence += 0.45
				result.Reasons = append(result.Reasons, "DIR pattern: "+pattern)
				break // Only count once per pattern
			}
		}
	}
	return confidence
}

// Pre-compiled regex patterns for mature content scanning
// Increased boost values for stricter detection
var maturePatterns = []struct {
	pattern *regexp.Regexp
	boost   float64
	reason  string
}{
	// Age/rating markers (increased from 0.85 to 0.90)
	{regexp.MustCompile(`(?i)\b(18\+|21\+|xxx|nc-?17|x-?rated|r-?rated)\b`), 0.90, "Age-restricted marker"},
	{regexp.MustCompile(`(?i)\b(adults?[\-_ ]?only|age[\-_ ]?restricted|18[\-_ ]?only)\b`), 0.90, "Adults-only marker"},

	// Uncensored/uncut markers (increased from 0.35 to 0.40)
	{regexp.MustCompile(`(?i)\b(uncensored|uncut|unrated|unexpurgated)\b`), 0.40, "Uncensored/unrated marker"},

	// Scene/episode numbering common in adult content (increased from 0.15 to 0.20)
	{regexp.MustCompile(`(?i)\b(vol\.?\d+|scene\.?\d+|ep\.?\d+|part\.?\d+)\b`), 0.20, "Series-style numbering"},
	{regexp.MustCompile(`(?i)\bscene[\-_ ]?\d+\b`), 0.20, "Scene numbering"},

	// Common adult content file naming patterns (increased from 0.20 to 0.25)
	{regexp.MustCompile(`(?i)\b\d{3,6}[\-_ ]\d{2,4}p\b`), 0.25, "Numeric ID + resolution pattern"},
	{regexp.MustCompile(`(?i)\b(720p|1080p|2160p|4k)[\-_ ]?(hardcore|xxx|porn|anal|oral)\b`), 0.90, "Resolution + explicit content"},
	{regexp.MustCompile(`(?i)\b(jav|av)[\-_ ]?\d+\b`), 0.75, "JAV-style ID pattern"},

	// Studio code patterns (increased from 0.10 to 0.15)
	{regexp.MustCompile(`(?i)\b[a-z]{2,5}-?\d{3,5}\b`), 0.15, "Studio content code pattern"},

	// Performer name patterns with explicit context (increased from 0.85 to 0.90)
	{regexp.MustCompile(`(?i)\b(creampie|gangbang|anal|oral|facial|dp|pov)[\-_ ](compilation|comp|mix|best)\b`), 0.90, "Explicit compilation"},

	// Common adult content descriptors with context (increased from 0.70 to 0.75, 0.55 to 0.60)
	{regexp.MustCompile(`(?i)\b(big|huge|massive|giant|monster)[\-_ ]?(cock|dick|tits|boobs|ass|butt)\b`), 0.75, "Explicit body descriptor"},
	{regexp.MustCompile(`(?i)\b(hot|sexy|horny|slutty|dirty)[\-_ ]?(milf|teen|wife|mom|girl|woman|babe)\b`), 0.60, "Suggestive person descriptor"},
	{regexp.MustCompile(`(?i)\b(step[\-_ ]?(mom|dad|sis|bro|sister|brother|mother|father|son|daughter))\b`), 0.70, "Step-family content"},

	// Common scene descriptions (increased from 0.85 to 0.90)
	{regexp.MustCompile(`(?i)\b(casting[\-_ ]?couch|fake[\-_ ]?(taxi|agent|hospital)|backroom)\b`), 0.90, "Known adult scenario"},
	{regexp.MustCompile(`(?i)\b(gloryhole|glorywall|peephole)\b`), 0.90, "Explicit scenario"},
	{regexp.MustCompile(`(?i)\b(cam[\-_ ]?(show|girl|model|session)|web[\-_ ]?cam[\-_ ]?(show|session))\b`), 0.55, "Cam show content"},

	// Adult content release group tags (increased from 0.85 to 0.90)
	{regexp.MustCompile(`(?i)\[(sexart|metart|femjoy|hegre|x-?art|nubile|twistys)]`), 0.90, "Adult studio tag"},
	{regexp.MustCompile(`(?i)\[(brazzers|bangbros|realitykings|naughtyamerica|blacked|tushy)]`), 0.90, "Adult studio tag"},

	// Short abbreviations with word boundaries (increased from 0.55 to 0.60, 0.70 to 0.75)
	{regexp.MustCompile(`(?i)(^|[\-_ .])dp([\-_ .]|$)`), 0.60, "DP abbreviation"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])pov([\-_ .]|$)`), 0.60, "POV abbreviation"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])joi([\-_ .]|$)`), 0.60, "JOI abbreviation"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])mmf([\-_ .]|$)`), 0.60, "MMF abbreviation"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])ffm([\-_ .]|$)`), 0.60, "FFM abbreviation"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])mfm([\-_ .]|$)`), 0.60, "MFM abbreviation"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])r18([\-_ .]|$)`), 0.75, "R18 rating marker"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])jav([\-_ .]|$)`), 0.60, "JAV content marker"},
	{regexp.MustCompile(`(?i)(^|[\-_ .])anal([\-_ .]|$)`), 0.75, "Anal content marker"},

	// Additional patterns
	{regexp.MustCompile(`(?i)\b(bbc|bwc|pawg|milf|gilf)[\-_ ]`), 0.65, "Adult acronym"},
	{regexp.MustCompile(`(?i)\b(threesome|foursome|orgy|group[\-_ ]?sex)\b`), 0.80, "Group content marker"},
	{regexp.MustCompile(`(?i)\b(lesbian|gay|bi[\-_ ]?sexual|trans)\b.*\b(sex|porn|xxx)\b`), 0.75, "Explicit LGBT content"},
}

// scanMatureRegexPatterns checks the filename against regex-based mature content patterns.
func scanMatureRegexPatterns(filename string, result *ScanResult) float64 {
	var confidence float64
	for _, p := range maturePatterns {
		if p.pattern.MatchString(filename) {
			confidence += p.boost
			result.Reasons = append(result.Reasons, p.reason)
		}
	}
	return confidence
}

// Pre-compiled patterns for filename structural analysis
var (
	// JAV-style codes: letter prefix + digits (e.g., SSIS-432, ABP-123, CAWD-001)
	javCodePattern = regexp.MustCompile(`(?i)^[a-z]{2,6}[\-_ ]?\d{3,5}`)
	// Multiple performer-style names separated by common delimiters
	multiNamePattern = regexp.MustCompile(`(?i)([a-z]+[\-_ ]+[a-z]+)[\-_ ]+(and|with|feat|ft|vs)[\-_ ]+([a-z]+[\-_ ]+[a-z]+)`)
	// Numeric IDs common in adult content databases
	numericIDPattern = regexp.MustCompile(`(?i)^\d{4,8}[\-_ ]`)
	// Common adult filename separators: dots between every word (e.g., Site.Name.Performer.Scene.Description.720p.mp4)
	dotSeparatorPattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9]+){5,}`)
)

// scanFilenameStructure analyzes filename structure for patterns common in adult content.
func scanFilenameStructure(filename, dirPath string, result *ScanResult) float64 {
	var confidence float64

	// Strip extension for structural analysis
	nameOnly := strings.TrimSuffix(filename, filepath.Ext(filename))

	// JAV-style code at start of filename (very common in Japanese adult content)
	if javCodePattern.MatchString(nameOnly) {
		// Only boost if there are other signals - JAV codes alone could be anything
		if len(result.HighConfMatches) > 0 || len(result.MedConfMatches) > 0 {
			confidence += 0.25
			result.Reasons = append(result.Reasons, "JAV-style content code in filename")
		}
	}

	// Multi-performer naming (e.g., "Jane Doe and John Smith")
	if multiNamePattern.MatchString(nameOnly) {
		confidence += 0.10
		result.Reasons = append(result.Reasons, "Multi-performer naming pattern")
	}

	// Leading numeric ID (common in scraped content)
	if numericIDPattern.MatchString(nameOnly) {
		confidence += 0.10
		result.Reasons = append(result.Reasons, "Numeric ID prefix (common in scraped content)")
	}

	// Dot-separated words (common in scene releases: Site.Performer.Description.Quality.Format)
	if dotSeparatorPattern.MatchString(nameOnly) {
		confidence += 0.15
		result.Reasons = append(result.Reasons, "Dot-separated naming (common in scene releases)")
	}

	// Check for multiple suspicious signals combining
	suspiciousCount := 0
	suspiciousTerms := []string{
		"compilation", "comp", "mix", "best of", "collection",
		"scene", "episode", "part", "vol", "chapter",
		"amateur", "homemade", "home made", "selfmade", "self made",
		"leaked", "private", "personal", "stolen",
		"full video", "full movie", "full scene", "complete",
	}
	for _, term := range suspiciousTerms {
		if strings.Contains(filename, term) {
			suspiciousCount++
		}
	}
	// Also check directory path for these
	for _, term := range suspiciousTerms {
		if strings.Contains(dirPath, term) {
			suspiciousCount++
		}
	}
	if suspiciousCount >= 2 {
		confidence += 0.25
		result.Reasons = append(result.Reasons, "Multiple suspicious structural indicators")
	} else if suspiciousCount == 1 {
		confidence += 0.10
		result.Reasons = append(result.Reasons, "Suspicious structural indicator")
	}

	return confidence
}

// applyThresholds sets the IsMature, AutoFlagged, and NeedsReview fields based on configured thresholds.
func (s *MatureScanner) applyThresholds(result *ScanResult) {
	cfg := s.config.Get()
	highThreshold := cfg.MatureScanner.HighConfidenceThreshold
	medThreshold := cfg.MatureScanner.MediumConfidenceThreshold

	if result.Confidence >= highThreshold {
		result.IsMature = true
		result.AutoFlagged = cfg.MatureScanner.AutoFlag
		result.NeedsReview = cfg.MatureScanner.RequireReview
		s.log.Debug("  Threshold: HIGH (%.2f >= %.2f)", result.Confidence, highThreshold)
	} else if result.Confidence >= medThreshold {
		result.NeedsReview = true
		s.log.Debug("  Threshold: MEDIUM (%.2f >= %.2f)", result.Confidence, medThreshold)
	} else {
		s.log.Debug("  Threshold: None (%.2f < %.2f)", result.Confidence, medThreshold)
	}
}

// ScanDirectory scans all files in a directory
func (s *MatureScanner) ScanDirectory(dir string) ([]*ScanResult, error) {
	var results []*ScanResult

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			s.log.Warn("Failed to access %s during scan: %v", path, err)
			return nil // Continue on error
		}
		if info.IsDir() {
			return nil
		}

		// Only scan media files matching configured allowed extensions
		ext := strings.ToLower(filepath.Ext(path))
		if !s.isAllowedExtension(ext) {
			return nil
		}

		result := s.ScanFile(path)
		if result != nil {
			results = append(results, result)
		}

		return nil
	})

	return results, err
}

// isAllowedExtension checks if a file extension is in the configured AllowedExtensions list.
// This ensures consistency with config.UploadsConfig.AllowedExtensions rather than
// relying on a hardcoded list.
func (s *MatureScanner) isAllowedExtension(ext string) bool {
	cfg := s.config.Get()
	for _, allowed := range cfg.Uploads.AllowedExtensions {
		if strings.EqualFold(ext, allowed) {
			return true
		}
	}
	return false
}

// addToReviewQueue adds an item to the in-memory review queue.
// The underlying scan result (with needs_review=true) is already persisted to MySQL
// via scanRepo.Save() when ScanFile is called.
func (s *MatureScanner) addToReviewQueue(result *ScanResult) {
	item := &models.MatureReviewItem{
		ID:         uuid.New().String(),
		Name:       filepath.Base(result.Path),
		MediaPath:  result.Path,
		DetectedAt: result.ScannedAt,
		Confidence: result.Confidence,
		Reasons:    result.Reasons,
	}

	s.mu.Lock()
	s.reviewQueue[result.Path] = item
	s.mu.Unlock()
}

// GetReviewQueue returns items pending review
func (s *MatureScanner) GetReviewQueue() []*models.MatureReviewItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]*models.MatureReviewItem, 0, len(s.reviewQueue))
	for _, item := range s.reviewQueue {
		if item.ReviewedAt == nil {
			items = append(items, item)
		}
	}
	return items
}

// ReviewItem processes a review decision
func (s *MatureScanner) ReviewItem(ctx context.Context, path, reviewerID, decision string) error {
	s.mu.Lock()

	item, ok := s.reviewQueue[path]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("item not found in review queue: %s", path)
	}

	now := time.Now()
	item.ReviewedBy = reviewerID
	item.ReviewedAt = &now
	item.Decision = decision

	// Ensure the scan result is in memory so callers (e.g. BatchReviewAction) can
	// retrieve it via GetScanResult after the review.  On a fresh start loadResults
	// is a no-op, so results may be absent even when the review queue was reloaded
	// from the database — populate from DB in that case.
	if _, inMem := s.results[path]; !inMem {
		if s.scanRepo != nil {
			if repoResult, err := s.scanRepo.Get(ctx, path); err == nil && repoResult != nil {
				s.results[path] = s.convertRepoToScanner(repoResult)
			} else {
				s.results[path] = &ScanResult{
					Path:      path,
					ScannedAt: time.Now(),
					Reasons:   []string{},
				}
			}
		} else {
			s.results[path] = &ScanResult{
				Path:      path,
				ScannedAt: time.Now(),
				Reasons:   []string{},
			}
		}
	}

	result := s.results[path]
	result.ReviewedBy = reviewerID
	result.ReviewedAt = &now
	result.ReviewDecision = decision

	switch decision {
	case "approve":
		result.IsMature = true
	case "reject":
		result.IsMature = false
		result.AutoFlagged = false
	}

	s.log.Info("Review decision for %s: %s by %s", path, decision, reviewerID)
	s.mu.Unlock()

	// Persist review decision to MySQL
	if err := s.scanRepo.MarkReviewed(ctx, path, reviewerID, decision); err != nil {
		s.log.Error("Failed to persist review decision: %v", err)
		return fmt.Errorf("failed to persist review decision: %w", err)
	}

	return nil
}

// GetScanResult returns the scan result for a path
func (s *MatureScanner) GetScanResult(path string) (*ScanResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result, ok := s.results[path]
	return result, ok
}

// IsMature checks if a file is flagged as mature
func (s *MatureScanner) IsMature(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if result, ok := s.results[path]; ok {
		return result.IsMature
	}
	return false
}

// SetMatureFlag manually sets the mature flag for a file
func (s *MatureScanner) SetMatureFlag(ctx context.Context, path string, isMature bool, reason string) error {
	s.mu.Lock()

	result, ok := s.results[path]
	if !ok {
		result = &ScanResult{
			Path:      path,
			ScannedAt: time.Now(),
			Reasons:   make([]string, 0),
		}
		s.results[path] = result
	}

	result.IsMature = isMature
	if reason != "" {
		result.Reasons = append(result.Reasons, "Manual: "+reason)
	}
	result.ReviewDecision = "manual"
	reviewedAt := time.Now()
	result.ReviewedAt = &reviewedAt

	s.log.Info("Manually set mature flag for %s: %v", path, isMature)
	// TODO: convertScannerToRepo is called below after Unlock, but result is still
	// referenced without the lock. Another goroutine could modify result concurrently
	// between Unlock and convertScannerToRepo. Should copy the result while holding
	// the lock or call convertScannerToRepo before unlocking.
	s.mu.Unlock()

	// Persist change to MySQL
	repoResult := s.convertScannerToRepo(result)
	if err := s.scanRepo.Save(ctx, repoResult); err != nil {
		s.log.Error("Failed to persist mature flag to database: %v", err)
		return fmt.Errorf("failed to persist mature flag: %w", err)
	}

	return nil
}

// GetStats returns scanning statistics
func (s *MatureScanner) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := Stats{
		TotalScanned:  len(s.results),
		PendingReview: 0,
	}

	for _, result := range s.results {
		if result.IsMature {
			stats.MatureCount++
		}
		if result.AutoFlagged {
			stats.AutoFlagged++
		}
	}

	for _, item := range s.reviewQueue {
		if item.ReviewedAt == nil {
			stats.PendingReview++
		}
	}

	return stats
}

// Stats holds scanning statistics
type Stats struct {
	TotalScanned  int `json:"total_scanned"`
	MatureCount   int `json:"mature_count"`
	AutoFlagged   int `json:"auto_flagged"`
	PendingReview int `json:"pending_review"`
}

// TODO: loadResults is a no-op, so the in-memory results map starts empty on every restart.
// This means GetScanResult and IsMature will return false for all files until they are
// re-scanned. The review queue is loaded from DB (loadReviewQueue), but scan results are
// not. This creates an inconsistency: the review queue references items that have no
// corresponding in-memory scan result, leading to the ReviewItem fallback path being
// triggered every time. Consider loading scan results from DB on startup, at least for
// reviewed/flagged items, to maintain consistency.

// loadResults is a no-op: scan results are persisted per-file in MySQL via scanRepo.Save().
// The in-memory results map is rebuilt as files are scanned at runtime.
func (s *MatureScanner) loadResults() error {
	return nil
}

// loadReviewQueue populates the in-memory review queue from MySQL on startup.
func (s *MatureScanner) loadReviewQueue() error {
	ctx := context.Background()
	pending, err := s.scanRepo.GetPendingReview(ctx)
	if err != nil {
		return fmt.Errorf("failed to load pending reviews from database: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range pending {
		scannedAt, _ := time.Parse(time.RFC3339, r.ScannedAt)
		s.reviewQueue[r.Path] = &models.MatureReviewItem{
			ID:         uuid.New().String(),
			Name:       filepath.Base(r.Path),
			MediaPath:  r.Path,
			DetectedAt: scannedAt,
			Confidence: r.Confidence,
			Reasons:    r.Reasons,
		}
	}

	return nil
}

// TODO: saveReviewQueue and saveResults are no-ops but still exist as methods.
// They are never called from outside this package. Consider removing these dead
// methods to reduce confusion and code surface.

// saveReviewQueue is a no-op: review queue state is persisted in MySQL
// via scanRepo.Save() (needs_review flag) and scanRepo.MarkReviewed().
func (s *MatureScanner) saveReviewQueue() error {
	return nil
}

// saveResults is a no-op: results are persisted per-scan via scanRepo.Save().
func (s *MatureScanner) saveResults() error {
	return nil
}

// Module is an alias for MatureScanner for consistency with other modules
type Module = MatureScanner

// NewModule creates a new scanner module with repository support.
// The database module is stored but repositories are created during Start().
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for scanner")
	}

	scanner := NewMatureScanner(cfg)
	scanner.dbModule = dbModule
	return scanner, nil
}

// convertScannerToRepo converts scanner ScanResult to repository ScanResult
func (s *MatureScanner) convertScannerToRepo(sr *ScanResult) *repositories.ScanResult {
	result := &repositories.ScanResult{
		Path:           sr.Path,
		IsMature:       sr.IsMature,
		Confidence:     sr.Confidence,
		AutoFlagged:    sr.AutoFlagged,
		NeedsReview:    sr.NeedsReview,
		ScannedAt:      sr.ScannedAt.Format(time.RFC3339),
		ReviewedBy:     sr.ReviewedBy,
		ReviewDecision: sr.ReviewDecision,
		Reasons:        sr.Reasons,
	}

	if sr.ReviewedAt != nil {
		result.ReviewedAt = sr.ReviewedAt.Format(time.RFC3339)
	}

	return result
}

// convertRepoToScanner converts repository ScanResult to scanner ScanResult
func (s *MatureScanner) convertRepoToScanner(rr *repositories.ScanResult) *ScanResult {
	scannedAt, _ := time.Parse(time.RFC3339, rr.ScannedAt)

	result := &ScanResult{
		Path:           rr.Path,
		IsMature:       rr.IsMature,
		Confidence:     rr.Confidence,
		AutoFlagged:    rr.AutoFlagged,
		NeedsReview:    rr.NeedsReview,
		ScannedAt:      scannedAt,
		ReviewedBy:     rr.ReviewedBy,
		ReviewDecision: rr.ReviewDecision,
		Reasons:        rr.Reasons,
	}

	if rr.ReviewedAt != "" {
		if reviewedAt, err := time.Parse(time.RFC3339, rr.ReviewedAt); err == nil {
			result.ReviewedAt = &reviewedAt
		}
	}

	return result
}

// ApproveContent approves content from the review queue
func (s *MatureScanner) ApproveContent(ctx context.Context, path string) error {
	return s.ReviewItem(ctx, path, "system", "approve")
}

// RejectContent rejects content from the review queue
func (s *MatureScanner) RejectContent(ctx context.Context, path string) error {
	return s.ReviewItem(ctx, path, "system", "reject")
}

// ClearReviewQueue removes all items from the in-memory review queue.
// Individual MySQL records remain in scan_results with needs_review=true until reviewed.
func (s *MatureScanner) ClearReviewQueue() {
	s.mu.Lock()
	s.reviewQueue = make(map[string]*models.MatureReviewItem)
	s.mu.Unlock()
}

// TODO: SetHFClient is not thread-safe. hfClient is read by ClassifyMatureContent and
// HasHuggingFace without synchronization, while SetHFClient can be called from main.go
// at any time. Should use atomic.Pointer or protect with a mutex.

// SetHFClient sets the Hugging Face client for visual classification. Call with nil to disable.
func (s *MatureScanner) SetHFClient(c *huggingface.Client) {
	s.hfClient = c
}

// HasHuggingFace returns true if visual classification via Hugging Face is configured.
func (s *MatureScanner) HasHuggingFace() bool {
	return s.hfClient != nil
}

// ClassifyMatureContent performs visual classification on a file already detected as mature.
// Extracts frames (for video) or uses the file (for images), sends them to the HF API,
// and returns aggregated, deduplicated tags. Returns nil if HF is not configured or on error.
func (s *MatureScanner) ClassifyMatureContent(ctx context.Context, path string) ([]string, error) {
	if s.hfClient == nil {
		return nil, nil
	}
	cfg := s.config.Get()
	maxFrames := cfg.HuggingFace.MaxFrames
	if maxFrames <= 0 {
		maxFrames = 3
	}
	framePaths, err := huggingface.ExtractFrames(ctx, huggingface.ExtractFramesOptions{VideoPath: path, Count: maxFrames, TempDir: s.tempDir})
	if err != nil {
		s.log.Warn("HF frame extraction failed for %s: %v", path, err)
		return nil, err
	}
	defer func() {
		for _, p := range framePaths {
			if p != path {
				_ = os.Remove(p)
			}
		}
	}()

	seen := make(map[string]bool)
	var allTags []string
	for _, framePath := range framePaths {
		if ctx.Err() != nil {
			break
		}
		data, err := os.ReadFile(framePath)
		if err != nil {
			s.log.Warn("Failed to read frame %s: %v", framePath, err)
			continue
		}
		result, err := s.hfClient.ClassifyImage(ctx, data)
		if err != nil {
			s.log.Warn("HF ClassifyImage failed for %s: %v", framePath, err)
			// Propagate auth/rate errors so admin sees them; skip only transient per-frame failures
			if strings.Contains(err.Error(), "API key") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "429") {
				return nil, err
			}
			continue
		}
		for _, t := range result.Tags {
			key := strings.ToLower(t)
			if !seen[key] {
				seen[key] = true
				allTags = append(allTags, t)
			}
		}
	}
	return allTags, nil
}

// ClassifyMatureDirectory runs visual classification on all mature-flagged files in a directory.
// Returns a map of file path to generated tags. Skips files that are not mature.
func (s *MatureScanner) ClassifyMatureDirectory(ctx context.Context, dir string) (map[string][]string, error) {
	if s.hfClient == nil {
		return nil, nil
	}
	results, err := s.ScanDirectory(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, result := range results {
		if !result.IsMature {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		tags, err := s.ClassifyMatureContent(ctx, result.Path)
		if err != nil {
			s.log.Warn("ClassifyMatureContent failed for %s: %v", result.Path, err)
			continue
		}
		if len(tags) > 0 {
			out[result.Path] = tags
		}
	}
	return out, nil
}
