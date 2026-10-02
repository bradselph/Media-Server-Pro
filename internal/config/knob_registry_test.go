package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests keep deploy-knobs.sh — the deploy pipeline's knob registry —
// in sync with the environment variables the server actually reads
// (KnownEnvKeys), so a new config env var can't ship without a deploy knob
// and a renamed one can't leave a dead knob behind.

// notDeployKnobs are env vars the server reads that deliberately have no
// deploy knob. Every entry needs a reason.
var notDeployKnobs = map[string]string{
	"DATABASE_HEARTBEAT_INTERVAL_SECONDS": "integer-seconds alternate of DATABASE_HEARTBEAT_INTERVAL (a knob)",
	"DATABASE_RECOVERY_COOLDOWN_MINUTES":  "integer-minutes alternate of DATABASE_RECOVERY_COOLDOWN (a knob)",
	"DATABASE_CONN_MAX_LIFETIME_HOURS":    "integer-hours alternate of DATABASE_CONN_MAX_LIFETIME (a knob)",
	"DATABASE_TIMEOUT_SECONDS":            "integer-seconds alternate of DATABASE_TIMEOUT (a knob)",
	"DATABASE_RETRY_INTERVAL_SECONDS":     "integer-seconds alternate of DATABASE_RETRY_INTERVAL (a knob)",
	"REMOTE_MEDIA_CACHE_SIZE_MB":          "MB alternate of REMOTE_MEDIA_CACHE_SIZE (a knob)",
	"HLS_ENABLED":                         "overridden on every load by FEATURE_HLS (syncFeatureToggles)",
	"THUMBNAILS_ENABLED":                  "overridden on every load by FEATURE_THUMBNAILS (syncFeatureToggles)",
	"ANALYTICS_ENABLED":                   "overridden on every load by FEATURE_ANALYTICS (syncFeatureToggles)",
	"MATURE_SCANNER_ENABLED":              "overridden on every load by FEATURE_MATURE_SCANNER (syncFeatureToggles)",
	"REMOTE_MEDIA_ENABLED":                "overridden on every load by FEATURE_REMOTE_MEDIA (syncFeatureToggles)",
	"EXTRACTOR_ENABLED":                   "overridden on every load by FEATURE_EXTRACTOR (syncFeatureToggles)",
	"HLS_PLAYLIST_LENGTH":                 "not used by the server (VOD playlists list every segment)",
}

// plumbingEnv are env vars read directly (os.Getenv) that are set by systemd,
// the terminal, the server itself, or tests — never by an operator.
var plumbingEnv = map[string]string{
	"INVOCATION_ID":              "set by systemd for every unit invocation",
	"NOTIFY_SOCKET":              "set by systemd (sd_notify)",
	"WATCHDOG_USEC":              "set by systemd (WatchdogSec)",
	"NO_COLOR":                   "terminal convention",
	"TERM":                       "terminal convention",
	"MEDIA_SERVER_RESTART_DELAY": "set by the server for its own restarted child process",
}

var (
	directEnvRead   = regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\(\s*(?:"([A-Z][A-Z0-9_]*)"|([A-Za-z_][A-Za-z0-9_]*))\s*\)`)
	knobOrderBlock  = regexp.MustCompile(`(?s)\nKNOB_ORDER=\(\n(.*?)\n\)\n`)
	knobScopeAssign = regexp.MustCompile(`(?m)^KNOB_SCOPE\[([A-Z0-9_]+)\]="([a-z]+)"`)
	knobHelperCall  = regexp.MustCompile(`(?m)^_knob ([A-Z0-9_]+) ([a-z]+) `)
)

func parseKnobRegistry(t *testing.T) (order []string, scope map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy-knobs.sh"))
	if err != nil {
		t.Fatalf("read deploy-knobs.sh: %v", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	m := knobOrderBlock.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("KNOB_ORDER block not found in deploy-knobs.sh")
	}
	for line := range strings.SplitSeq(m[1], "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			order = append(order, line)
		}
	}
	scope = make(map[string]string)
	for _, sm := range knobScopeAssign.FindAllStringSubmatch(text, -1) {
		scope[sm[1]] = sm[2]
	}
	for _, sm := range knobHelperCall.FindAllStringSubmatch(text, -1) {
		scope[sm[1]] = sm[2]
	}
	return order, scope
}

func TestKnobRegistry_CoversEveryServerEnvVar(t *testing.T) {
	order, _ := parseKnobRegistry(t)
	registered := make(map[string]bool, len(order))
	for _, name := range order {
		registered[name] = true
	}
	known := make(map[string]bool)
	for _, k := range KnownEnvKeys() {
		known[k.Name] = true
		if registered[k.Name] {
			continue
		}
		if _, excluded := notDeployKnobs[k.Name]; excluded {
			continue
		}
		t.Errorf("server env var %s (%s class) has no deploy knob — add it to deploy-knobs.sh (KNOB_ORDER + metadata) or to notDeployKnobs with a reason", k.Name, k.Class)
	}
	for name := range notDeployKnobs {
		if registered[name] {
			t.Errorf("%s is in notDeployKnobs but is also a registered knob", name)
		}
		if !known[name] {
			t.Errorf("notDeployKnobs entry %s is stale: the server no longer reads it", name)
		}
	}
}

func TestKnobRegistry_RuntimeKnobsAreReadByServer(t *testing.T) {
	order, scope := parseKnobRegistry(t)
	known := make(map[string]bool)
	aliasOf := make(map[string]string)
	for _, k := range KnownEnvKeys() {
		known[k.Name] = true
		for _, a := range k.Aliases {
			aliasOf[a] = k.Name
		}
	}
	seen := make(map[string]bool)
	for _, name := range order {
		if seen[name] {
			t.Errorf("knob %s listed twice in KNOB_ORDER", name)
		}
		seen[name] = true
		switch scope[name] {
		case "runtime":
			if canonical, ok := aliasOf[name]; ok {
				t.Errorf("knob %s is an alias of %s — register the primary name instead", name, canonical)
			} else if !known[name] && !IsProcessEnvKey(name) {
				t.Errorf("runtime knob %s is not read by the server (dead knob)", name)
			}
		case "build":
			if !strings.HasPrefix(name, "NUXT_PUBLIC_") {
				t.Errorf("build knob %s must be a NUXT_PUBLIC_* variable", name)
			}
		case "vps", "toolchain":
		default:
			t.Errorf("knob %s has a missing or unknown scope %q", name, scope[name])
		}
	}
}

// Env vars read with os.Getenv outside the config package (e.g. BACKUP_DIR in
// internal/backup, GOMEMLIMIT in internal/runtimeenv) bypass KnownEnvKeys, so
// scan the sources for them: each must be a registered knob or plumbing.
func TestKnobRegistry_CoversDirectEnvReads(t *testing.T) {
	order, _ := parseKnobRegistry(t)
	registered := make(map[string]bool, len(order))
	for _, name := range order {
		registered[name] = true
	}
	root := filepath.Join("..", "..")
	found := 0
	for _, dir := range []string{"cmd", "internal", "pkg", "api"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "config", "testutil", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range directEnvRead.FindAllStringSubmatch(string(src), -1) {
				name := m[1]
				if name == "" { // identifier argument: resolve `ident = "VALUE"` in the same file
					cm := regexp.MustCompile(regexp.QuoteMeta(m[2]) + `\s*=\s*"([A-Z][A-Z0-9_]*)"`).FindStringSubmatch(string(src))
					if cm == nil {
						t.Errorf("%s: os.Getenv(%s) — cannot resolve the env var name; use a literal or a const in the same file", path, m[2])
						continue
					}
					name = cm[1]
				}
				found++
				if registered[name] {
					if !IsProcessEnvKey(name) {
						t.Errorf("%s reads %s directly; register it in processEnvKeys (apply_knobs.go) so the apply step classifies it", path, name)
					}
					continue
				}
				if _, ok := plumbingEnv[name]; !ok {
					t.Errorf("%s reads env var %s directly but it is neither a deploy knob nor listed in plumbingEnv", path, name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found == 0 {
		t.Fatal("found no direct env reads — the scan is broken")
	}
}
