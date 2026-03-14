package handlers

import (
	"bufio"
	"net/http"
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
		if os.IsNotExist(err) {
			writeSuccess(c, []interface{}{})
			return
		}
		h.log.Warn("Failed to read logs directory %s: %v", logsDir, err)
		writeError(c, http.StatusInternalServerError, "Failed to read logs directory")
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

// readLastNLines reads the last N lines from a file using a ring buffer so
// only O(n) memory is held regardless of file size.
func readLastNLines(filePath string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	ring := make([]string, n)
	idx := 0
	total := 0

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		ring[idx%n] = sc.Text()
		idx++
		total++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	count := total
	if count > n {
		count = n
	}
	result := make([]string, 0, count)
	start := idx - count
	for i := start; i < idx; i++ {
		result = append(result, ring[i%n])
	}
	return result, nil
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
