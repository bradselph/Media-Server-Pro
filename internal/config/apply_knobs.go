package config

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"media-server-pro/internal/logger"
)

// Env var classes, as Manager.Load applies them (see env_overrides.go).
const (
	// EnvClassAlways vars are re-applied from the environment on every start
	// (paths, database, storage, admin bootstrap).
	EnvClassAlways = "always"
	// EnvClassSeed vars only seed a brand-new config.json (or ran once during
	// the EnvSeedMigrated/InfraOwnershipMigrated upgrades); afterwards
	// config.json / the admin UI owns the setting and the env var is ignored
	// at startup. ApplyDeployKnobs is how a deploy changes them.
	EnvClassSeed = "seed"
)

// EnvKeyInfo describes one environment variable read by the config
// overrides: its primary name, fallback alias names that set the same field,
// and when Load applies it.
type EnvKeyInfo struct {
	Name    string
	Aliases []string
	Class   string
}

// KnownEnvKeys enumerates every environment variable the config overrides
// read. Each override pass runs against an empty environment with a recorder
// attached, so nothing is applied but every envGet* lookup reports the names
// it checks. Order is stable: the always class first, then seed, each in
// code order.
func KnownEnvKeys() []EnvKeyInfo {
	m := &Manager{config: DefaultConfig(), log: logger.New("config")}
	var out []EnvKeyInfo
	seen := make(map[string]bool)
	collect := func(class string, pass func()) {
		withEnvSource(&envSource{
			lookup: func(string) string { return "" },
			record: func(keys []string) {
				if len(keys) == 0 || seen[keys[0]] {
					return
				}
				seen[keys[0]] = true
				out = append(out, EnvKeyInfo{Name: keys[0], Aliases: append([]string(nil), keys[1:]...), Class: class})
			},
		}, pass)
	}
	collect(EnvClassAlways, m.applyInfraEnvOverrides)
	collect(EnvClassSeed, func() {
		m.applySeededInfraEnvOverrides()
		m.applyTunableEnvOverrides()
	})
	return out
}

// processEnvKeys are deploy knobs read straight from the process environment
// when the service starts — by the Go runtime (GOMEMLIMIT, GOGC; systemd
// passes $DEPLOY_DIR/.env via EnvironmentFile and internal/runtimeenv steps
// aside when they are set) or by a module (BACKUP_DIR, internal/backup) —
// not through the config overrides, so they never touch config.json and take
// effect on the deploy's restart.
var processEnvKeys = map[string]bool{"GOMEMLIMIT": true, "GOGC": true, "BACKUP_DIR": true}

// IsProcessEnvKey reports whether key is read straight from the process
// environment at start rather than through the config overrides.
func IsProcessEnvKey(key string) bool { return processEnvKeys[key] }

// envKeyClasses maps every known env name (primary and alias) to its class.
func envKeyClasses() map[string]string {
	classes := make(map[string]string)
	for _, k := range KnownEnvKeys() {
		classes[k.Name] = k.Class
		for _, a := range k.Aliases {
			if _, ok := classes[a]; !ok {
				classes[a] = k.Class
			}
		}
	}
	return classes
}

// ApplyKnobsOptions configures Manager.ApplyDeployKnobs.
type ApplyKnobsOptions struct {
	// Keys are the env var names deploy.sh forwarded into .env this deploy.
	Keys []string
	// StatePath is the record of what was last applied; "" = KnobStatePath.
	StatePath string
	// Force applies every listed seed-only key, even when its value matches
	// the record (re-asserts .deploy.env over admin-UI edits).
	Force bool
	// DryRun reports what would change without writing config.json or the
	// record.
	DryRun bool
}

// KnobChange is one config.json field the apply step changed. Values are
// JSON-encoded; secrets are redacted.
type KnobChange struct {
	Path string
	Old  string
	New  string
}

// ApplyKnobsResult reports what ApplyDeployKnobs did with each key.
type ApplyKnobsResult struct {
	Applied   []string     // seed-only keys applied (new or changed since the last apply, or forced)
	Baselined []string     // first tracked run: seed-only keys recorded as the baseline WITHOUT being applied
	Unchanged []string     // seed-only keys whose value matches the last apply; left alone so admin-UI edits survive
	Always    []string     // keys the server re-reads from .env on every start; nothing to persist
	Process   []string     // keys read straight from the environment at start (GOMEMLIMIT, GOGC, BACKUP_DIR)
	Unknown   []string     // keys no config override reads; they have no effect on the server
	Empty     []string     // keys with no value in the environment; skipped
	Changes   []KnobChange // config.json fields that changed (with Baselined: fields that WOULD change)
	Saved     bool         // config.json was rewritten
}

// KnobStatePath is where ApplyDeployKnobs records what it applied: a file
// next to config.json.
func KnobStatePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ".knobs-applied")
}

var envKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ApplyDeployKnobs makes deploy knobs take effect on an already-seeded
// install. Seed-only env vars are ignored at startup once config.json exists
// (see Manager.Load), so without this step changing such a knob in
// .deploy.env and redeploying silently did nothing.
//
// It loads config.json + .env exactly like a normal start, then re-applies
// only the listed seed-only keys whose value differs from the value recorded
// at the last successful apply (all of them with Force). Every other env var
// is hidden from the overrides while they run, so stale values sitting in
// .env — e.g. ones an installer wrote long ago — cannot leak in. The result
// is validated with the normal validator and saved atomically; the record is
// updated only after a successful save, so a failed apply is retried on the
// next deploy. A knob whose value has not changed is never re-applied, so an
// admin-UI edit to that setting survives deploys until the operator changes
// the knob itself.
//
// The first run on an install (no record yet, not forced) is a baseline: it
// records the current values without applying them and reports where they
// differ from config.json. Upgrading to this mechanism therefore never
// silently reverts admin-UI edits made since the server was seeded; the
// operator applies the reported differences deliberately with Force
// (deploy.sh --reapply-knobs).
//
// The record stores a SHA-256 of each value, never the value.
func (m *Manager) ApplyDeployKnobs(opts ApplyKnobsOptions) (*ApplyKnobsResult, error) {
	if err := m.Load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	statePath := opts.StatePath
	if statePath == "" {
		statePath = KnobStatePath(m.configPath)
	}
	state, tracked, err := readKnobState(statePath)
	if err != nil {
		return nil, err
	}
	baseline := !tracked && !opts.Force

	classes := envKeyClasses()
	res := &ApplyKnobsResult{}
	allow := make(map[string]bool)
	hashes := make(map[string]string)
	for _, key := range normalizeKnobKeys(opts.Keys) {
		val := os.Getenv(key)
		switch {
		case val == "":
			res.Empty = append(res.Empty, key)
		case classes[key] == EnvClassAlways:
			res.Always = append(res.Always, key)
		case processEnvKeys[key]:
			res.Process = append(res.Process, key)
		case classes[key] == "":
			res.Unknown = append(res.Unknown, key)
		default:
			h := hashKnobValue(key, val)
			if !opts.Force && state[key] == h {
				res.Unchanged = append(res.Unchanged, key)
				continue
			}
			allow[key] = true
			hashes[key] = h
			if baseline {
				res.Baselined = append(res.Baselined, key)
			} else {
				res.Applied = append(res.Applied, key)
			}
		}
	}
	if len(allow) == 0 {
		return res, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	originalJSON, err := json.Marshal(m.config)
	if err != nil {
		return nil, fmt.Errorf("snapshot config: %w", err)
	}
	before, err := flattenConfig(originalJSON)
	if err != nil {
		return nil, err
	}
	restore := func() {
		var orig Config
		if json.Unmarshal(originalJSON, &orig) == nil {
			m.config = &orig
		}
	}

	withEnvSource(&envSource{lookup: func(key string) string {
		if !allow[key] {
			return ""
		}
		return os.Getenv(key)
	}}, func() {
		m.applySeededInfraEnvOverrides()
		m.applyTunableEnvOverrides()
	})
	// Same post-processing Load does after the env overrides.
	m.resolveAbsolutePaths()
	m.syncFeatureToggles()
	m.normalizeHLSScalars()
	if baseline {
		// Nothing is applied on the baseline run, so there is nothing to
		// validate: report the would-be differences, record, and stop.
		if err := m.diffAgainst(before, res); err != nil {
			restore()
			return res, err
		}
		restore()
		if opts.DryRun {
			return res, nil
		}
		maps.Copy(state, hashes)
		if err := writeKnobState(statePath, state); err != nil {
			return res, fmt.Errorf("recording the knob baseline in %s failed: %w", statePath, err)
		}
		return res, nil
	}
	// Load already printed the validators' warnings for this config; mute
	// them on this second pass so the deploy log shows each once.
	loud := m.log
	m.log = logger.New("config")
	m.log.SetLevel(logger.ERROR)
	errs := m.validateLocked()
	m.log = loud
	if len(errs) > 0 {
		restore()
		return res, fmt.Errorf("knob values failed validation, config.json left unchanged: %w", errors.Join(errs...))
	}

	if err := m.diffAgainst(before, res); err != nil {
		restore()
		return res, err
	}

	if opts.DryRun {
		restore()
		return res, nil
	}
	if len(res.Changes) > 0 {
		if err := m.save(); err != nil {
			restore()
			return res, fmt.Errorf("save config.json: %w", err)
		}
		res.Saved = true
	}
	// Record what .deploy.env asked for even when it was already in effect, so
	// the next deploy only acts on a real change.
	maps.Copy(state, hashes)
	if err := writeKnobState(statePath, state); err != nil {
		return res, fmt.Errorf("config.json updated but recording applied knobs in %s failed (the next deploy re-applies them): %w", statePath, err)
	}
	return res, nil
}

// diffAgainst records in res the config.json fields that differ between the
// before snapshot and the current in-memory config. Caller holds m.mu.
func (m *Manager) diffAgainst(before map[string]string, res *ApplyKnobsResult) error {
	afterJSON, err := json.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("snapshot config: %w", err)
	}
	after, err := flattenConfig(afterJSON)
	if err != nil {
		return err
	}
	res.Changes = diffFlattened(before, after)
	return nil
}

// normalizeKnobKeys trims, drops empty or malformed names, and dedupes while
// keeping order.
func normalizeKnobKeys(keys []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] || !envKeyPattern.MatchString(k) {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func hashKnobValue(key, val string) string {
	sum := sha256.Sum256([]byte(key + "\x00" + val))
	return hex.EncodeToString(sum[:])
}

// readKnobState loads the record; exists reports whether one was present.
func readKnobState(path string) (state map[string]string, exists bool, err error) {
	state = make(map[string]string)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read knob record %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 {
			state[fields[0]] = fields[1]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, false, fmt.Errorf("read knob record %s: %w", path, err)
	}
	return state, true, nil
}

func writeKnobState(path string, state map[string]string) error {
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Written by `server -apply-knobs` during deploy: one line per deploy knob,\n")
	b.WriteString("# KEY SHA256(value) of the value last applied to config.json. A knob is only\n")
	b.WriteString("# re-applied when its .deploy.env value changes, so admin-UI edits survive\n")
	b.WriteString("# deploys. Delete this file (or deploy with --reapply-knobs) to re-apply all.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %s\n", k, state[k])
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// flattenConfig turns a JSON-encoded config into dot-path -> JSON leaf value.
// Arrays are leaves (compared whole).
func flattenConfig(data []byte) (map[string]string, error) {
	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("flatten config: %w", err)
	}
	out := make(map[string]string)
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if obj, ok := v.(map[string]any); ok {
			for k, child := range obj {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, child)
			}
			return
		}
		enc, _ := json.Marshal(v)
		out[prefix] = string(enc)
	}
	walk("", tree)
	return out, nil
}

// sensitivePathParts mark config.json paths whose values must never be
// printed in a deploy log.
var sensitivePathParts = []string{"password", "secret", "token", "api_key", "access_key", "hash"}

func isSensitivePath(path string) bool {
	lower := strings.ToLower(path)
	for _, p := range sensitivePathParts {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func diffFlattened(before, after map[string]string) []KnobChange {
	paths := make(map[string]bool)
	for p := range before {
		paths[p] = true
	}
	for p := range after {
		paths[p] = true
	}
	var changes []KnobChange
	for p := range paths {
		oldV, newV := before[p], after[p]
		if oldV == newV {
			continue
		}
		if isSensitivePath(p) {
			oldV, newV = redactedLabel(oldV), redactedLabel(newV)
		}
		changes = append(changes, KnobChange{Path: p, Old: oldV, New: newV})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func redactedLabel(v string) string {
	if v == "" || v == `""` || v == "null" || v == "[]" {
		return "(empty)"
	}
	return "(redacted)"
}
