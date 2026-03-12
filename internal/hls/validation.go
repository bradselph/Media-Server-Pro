package hls

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidationResult holds HLS validation results
type ValidationResult struct {
	JobID        string   `json:"job_id"`
	Valid        bool     `json:"valid"`
	VariantCount int      `json:"variant_count"`
	SegmentCount int      `json:"segment_count"`
	Errors       []string `json:"errors,omitempty"`
}

// ValidateMasterPlaylist validates a master playlist and its variants
func (m *Module) ValidateMasterPlaylist(jobID string) (*ValidationResult, error) {
	result := &ValidationResult{
		JobID:  jobID,
		Valid:  true,
		Errors: make([]string, 0),
	}
	outputDir := filepath.Join(m.cacheDir, jobID)

	masterData, err := m.readMasterPlaylist(outputDir)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	variants := m.parseVariantStreams(string(masterData))
	result.VariantCount = len(variants)
	if len(variants) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Master playlist has no variant streams")
		return result, nil
	}

	m.validateVariants(result, outputDir, variants)
	return result, nil
}

// readMasterPlaylist reads the master playlist file and returns its content or an error
func (m *Module) readMasterPlaylist(outputDir string) ([]byte, error) {
	masterPath := filepath.Join(outputDir, masterPlaylistName)
	data, err := os.ReadFile(masterPath)
	if err != nil {
		return nil, fmt.Errorf("master playlist not found: %w", err)
	}
	return data, nil
}

// validateVariant validates one variant playlist and returns segment count and any errors
func (m *Module) validateVariant(outputDir, variant string) (segmentCount int, errs []string) {
	variantPath := filepath.Join(outputDir, variant)
	variantData, err := os.ReadFile(variantPath)
	if err != nil {
		return 0, []string{fmt.Sprintf("Variant %s not found: %v", variant, err)}
	}
	if len(variantData) == 0 {
		return 0, []string{fmt.Sprintf("Variant %s is empty", variant)}
	}
	segments := m.parseSegments(string(variantData))
	for _, segment := range segments {
		segmentPath := filepath.Join(outputDir, filepath.Dir(variant), segment)
		if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("Segment %s missing", segment))
		}
	}
	return len(segments), errs
}

// validateVariants checks each variant playlist and its segments, updating result in place
func (m *Module) validateVariants(result *ValidationResult, outputDir string, variants []string) {
	for _, variant := range variants {
		segmentCount, errs := m.validateVariant(outputDir, variant)
		if len(errs) > 0 {
			result.Valid = false
			result.Errors = append(result.Errors, errs...)
		}
		result.SegmentCount += segmentCount
	}
}

// parseVariantStreams extracts variant stream paths from master playlist
func (m *Module) parseVariantStreams(content string) []string {
	var variants []string
	// Handle both Unix (\n) and Windows (\r\n) line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		// Trim any remaining whitespace (including \r)
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		if i+1 >= len(lines) {
			continue
		}
		variant := strings.TrimSpace(lines[i+1])
		if variant == "" || strings.HasPrefix(variant, "#") {
			continue
		}
		variants = append(variants, variant)
	}

	return variants
}

// isSegmentLine returns true if the line is a non-empty, non-comment segment filename (.ts)
// TODO: Bug - only .ts segments are recognized. HLS also supports .aac audio
// segments and fMP4 segments (.m4s, .mp4). If the server ever supports
// hls_segment_type=fmp4, this check will miss all segments, causing validation
// to report "no segments found" for valid jobs. Should also check for .m4s and .aac.
func isSegmentLine(line string) bool {
	if line == "" || strings.HasPrefix(line, "#") {
		return false
	}
	return strings.HasSuffix(line, ".ts")
}

// parseSegments extracts segment filenames from a variant playlist
func (m *Module) parseSegments(content string) []string {
	var segments []string
	// Handle both Unix (\n) and Windows (\r\n) line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if isSegmentLine(line) {
			segments = append(segments, line)
		}
	}

	return segments
}
