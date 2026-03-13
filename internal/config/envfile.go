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
	value = stripEnvQuotes(strings.TrimSpace(line[idx+1:]))
	return key, value
}

// TODO: Bug — loadEnvFile calls os.Setenv which mutates the global process environment.
// These values persist for the entire process lifetime and affect all goroutines, even
// after a config reload. If the .env file is removed or values change, stale env vars
// remain set. This also means env overrides from the .env file cannot be distinguished
// from real environment variables set by the OS/user. Should store parsed values in a
// local map and use that map in applyEnvOverrides instead of polluting os.Environ().
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
