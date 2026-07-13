package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScratchOrderBug(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server_2026-07-13.log"), []byte("A1\nA2\nA3\n"), 0644)
	os.WriteFile(filepath.Join(dir, "server_2026-07-12.log"), []byte("B1\nB2\nB3\n"), 0644)

	entries, _ := os.ReadDir(dir)
	// mimic sort
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].Name() > entries[i].Name() {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	limit := 200
	var logLines []string
	for _, entry := range entries {
		filePath := filepath.Join(dir, entry.Name())
		lines, err := readLastNLines(filePath, limit-len(logLines))
		if err != nil {
			continue
		}
		logLines = append(logLines, lines...)
	}
	t.Logf("before reversal: %v", logLines)

	for i, j := 0, len(logLines)-1; i < j; i, j = i+1, j-1 {
		logLines[i], logLines[j] = logLines[j], logLines[i]
	}
	t.Logf("after reversal: %v", logLines)
}
