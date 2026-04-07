package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) findEnvFile() string {
	configDir := filepath.Dir(m.configPath)
	envPath := filepath.Join(configDir, ".env")
	if _, err := os.Stat(envPath); err == nil {
		return envPath
	}
	if _, err := os.Stat(".env"); err == nil {
		return ".env"
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		envPath := filepath.Join(exeDir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}
	return ""
}

func stripEnvQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	if value[0] == '"' && value[len(value)-1] == '"' {
		unquoted := value[1 : len(value)-1]
		return strings.ReplaceAll(unquoted, `\"`, `"`)
	}
	if value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}

func parseEnvLine(line string) (key, value string) {
	idx := strings.Index(line, "=")
	if idx == -1 {
		return "", ""
	}
	key = strings.TrimSpace(line[:idx])
	raw := strings.TrimSpace(line[idx+1:])

	// If the value is quoted, pass it through as-is (comments inside quotes are
	// part of the value). Otherwise strip inline comments (# preceded by whitespace).
	if len(raw) >= 2 && ((raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'')) {
		value = stripEnvQuotes(raw)
	} else {
		// Strip inline comment: find the first unquoted " #"
		if i := strings.Index(raw, " #"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		} else if strings.HasPrefix(raw, "#") {
			raw = ""
		}
		value = raw
	}
	return key, value
}

// loadEnvFile reads .env and applies values via os.Setenv (process-wide).
func (m *Manager) loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close .env file: %v", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value := parseEnvLine(line)
		if key == "" {
			continue
		}
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
