package handlers

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// parseLogLimit returns a clamped limit from query string, or defaultVal if invalid.
func parseLogLimit(q string, defaultVal, max int) int {
	l, err := strconv.Atoi(q)
	if err != nil || l <= 0 || l > max {
		return defaultVal
	}
	return l
}

// GetServerLogs reads recent entries from the server log files.
func (h *Handler) GetServerLogs(c *gin.Context) {
	limit := parseLogLimit(c.Query("limit"), 200, 2000)

	cfg := h.media.GetConfig()
	logsDir := cfg.Directories.Logs
	if logsDir == "" {
		logsDir = "logs"
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		writeSuccess(c, []interface{}{})
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	var logLines []map[string]interface{}

	const maxLogFiles = 50
	filesProcessed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		filePath := filepath.Join(logsDir, entry.Name())
		lines, readErr := readLastNLines(filePath, limit-len(logLines))
		if readErr != nil {
			h.log.Debug("Failed to read log file %s: %v", filePath, readErr)
			continue
		}

		for _, line := range lines {
			logEntry := parseLogLine(line)
			logLines = append(logLines, logEntry)
		}

		filesProcessed++
		if len(logLines) >= limit || filesProcessed >= maxLogFiles {
			break
		}
	}

	for i, j := 0, len(logLines)-1; i < j; i, j = i+1, j-1 {
		logLines[i], logLines[j] = logLines[j], logLines[i]
	}

	logLines = filterLogEntries(logLines, strings.ToLower(c.Query("level")), strings.ToLower(c.Query("module")))
	writeSuccess(c, logLines)
}

// filterLogEntries returns entries matching level and/or module filters; pass "" to skip that filter.
func filterLogEntries(entries []map[string]interface{}, levelFilter, moduleFilter string) []map[string]interface{} {
	if levelFilter == "" && moduleFilter == "" {
		return entries
	}
	filtered := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		if levelFilter != "" {
			entryLevel, _ := entry["level"].(string)
			if strings.ToLower(entryLevel) != levelFilter {
				continue
			}
		}
		if moduleFilter != "" {
			entryModule, _ := entry["module"].(string)
			if !strings.Contains(strings.ToLower(entryModule), moduleFilter) {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// readLastNLines reads the last N lines from a file
// TODO: This reads the entire file into memory to get the last N lines. For large log files
// (hundreds of MB), this will cause excessive memory usage. Consider reading the file in
// reverse from the end (e.g., seeking to EOF and reading backwards) or using a ring buffer
// approach to only keep the last N lines in memory.
func readLastNLines(filePath string, n int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var lines []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, sc.Err()
}

// parseLogLine parses a server log line into a structured entry
func parseLogLine(line string) map[string]interface{} {
	entry := map[string]interface{}{
		"raw":       line,
		"timestamp": "",
		"level":     "info",
		"module":    "",
		"message":   line,
	}

	if len(line) > 25 && line[0] == '[' {
		if idx := strings.Index(line[1:], "]"); idx > 0 {
			entry["timestamp"] = line[1 : idx+1]
			rest := strings.TrimSpace(line[idx+2:])

			if len(rest) > 0 && rest[0] == '[' {
				if idx2 := strings.Index(rest[1:], "]"); idx2 > 0 {
					level := strings.TrimSpace(rest[1 : idx2+1])
					entry["level"] = strings.ToLower(level)
					rest = strings.TrimSpace(rest[idx2+2:])
				}
			}

			if len(rest) > 0 && rest[0] == '[' {
				if idx3 := strings.Index(rest[1:], "]"); idx3 > 0 {
					entry["module"] = rest[1 : idx3+1]
					rest = strings.TrimSpace(rest[idx3+2:])
				}
			}

			if len(rest) > 0 && rest[0] == '[' {
				if idx4 := strings.Index(rest[1:], "]"); idx4 > 0 {
					rest = strings.TrimSpace(rest[idx4+2:])
				}
			}

			entry["message"] = rest
		}
	}

	return entry
}
