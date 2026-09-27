package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
)

// sensitiveConfigKeywords are substrings whose presence in a config key marks
// its value for redaction in the audit log. Package-level so the recursive
// redactor does not reallocate it per call.
var sensitiveConfigKeywords = []string{"password", "token", "api_key", "access_key", "secret", "deploy_key"}

// redactSensitiveConfigKeys returns a copy of m with sensitive values replaced by "[REDACTED]".
// Prevents database credentials, API keys, tokens, etc. from being stored in the audit log.
func redactSensitiveConfigKeys(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	redacted := make(map[string]any, len(m))
	for k, v := range m {
		keyLower := strings.ToLower(k)
		if slices.ContainsFunc(sensitiveConfigKeywords, func(kw string) bool {
			return strings.Contains(keyLower, kw)
		}) {
			redacted[k] = "[REDACTED]"
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			redacted[k] = redactSensitiveConfigKeys(nested)
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// AdminGetConfig returns the current configuration
func (h *Handler) AdminGetConfig(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	cfg := h.admin.GetConfigMap()
	writeSuccess(c, cfg)
}

// configDenyList contains top-level config sections that must not be mutated at
// runtime via the admin API. Credentials, session secrets, and path controls
// are restricted to env vars or direct config file edits only.
var configDenyList = map[string]bool{
	"database":    true, // DB host, user, password
	"auth":        true, // session secrets, lockout policy
	"receiver":    true, // slave API keys
	"directories": true, // media scan paths — runtime redirect is a security risk
}

// configFieldDenyList lists dot-notation paths of individual fields that must
// not be mutated at runtime, even though their parent section is not fully blocked.
// Format: "section.field" (lowercase, matching JSON tag names).
var configFieldDenyList = map[string]bool{
	"admin.username":      true, // use admin credential endpoints instead
	"admin.password_hash": true, // change password via /api/admin/change-password
	"hub.csv_path":        true, // server-local filesystem path — runtime redirect is a security risk (mirrors directories)
	"hub.work_dir":        true, // server-local scratch path — config/env only
}

// filterDeniedConfigKeys removes denied sections/fields from the update map
// and returns the list of rejected keys.
//
// A key is denied when:
//   - its top-level section appears in configDenyList (entire section blocked), OR
//   - the key itself (lowercased) appears in configFieldDenyList, OR
//   - for object-valued sections, specific nested fields are stripped from the value.
func filterDeniedConfigKeys(updates map[string]any) []string {
	var rejected []string
	for k := range updates {
		topLevel, _, _ := strings.Cut(strings.ToLower(k), ".")
		if configDenyList[topLevel] {
			rejected = append(rejected, k)
			delete(updates, k)
			continue
		}
		if configFieldDenyList[strings.ToLower(k)] {
			rejected = append(rejected, k)
			delete(updates, k)
			continue
		}
		// Strip individual sensitive fields from object-valued section updates.
		if obj, ok := updates[k].(map[string]any); ok {
			for _, deny := range []string{"password_hash", "username"} {
				if topLevel == "admin" {
					if _, exists := obj[deny]; exists {
						delete(obj, deny)
						rejected = append(rejected, k+"."+deny)
					}
				}
			}
		}
	}
	return rejected
}

// hotReloadKeys lists the top-level config sections whose admin edits are
// applied to running components immediately, so no restart is required. It is
// kept in sync with the actual cfg.OnChange consumers / per-request reads:
//   - security       → internal/security/security.go (rate limits, CORS, CSP,
//     trusted proxies) + the whitelist/blacklist enable flags
//     applied in AdminUpdateConfig below
//   - features        → feature gates are re-read per request / per task tick,
//     EXCEPT the entries in hotReloadFieldOverrides below (modules that are
//     only ever constructed once, at boot, in cmd/server/main.go)
//   - hls, analytics  → cmd/server/main.go registerScheduleWatcher re-applies the
//     schedule intervals live, EXCEPT the entries in hotReloadFieldOverrides
//   - age_gate,
//     cookie_consent  → cmd/server/main.go reloads the middleware live via
//     UpdateConfig on every config change
//
// "server" is deliberately NOT listed here: internal/server/server.go builds
// s.httpServer exactly once in Start() and never rebuilds it, so host/port/
// TLS/timeout changes all require a restart. The one exception
// (memory_limit_percent) is carried in hotReloadFieldOverrides instead.
//
// Sections NOT listed here (storage, directories, database, auth, uploads, …)
// are persisted but only take effect on restart. NOTE: this is section-level —
// hotReloadFieldOverrides below corrects the few individual fields whose
// live-reload behavior disagrees with their section's default classification,
// but restart_required otherwise remains a best-effort hint, not a guarantee.
var hotReloadKeys = map[string]bool{
	"security":       true,
	"features":       true,
	"hls":            true,
	"analytics":      true,
	"age_gate":       true,
	"cookie_consent": true,
	"hub":            true, // hub.page_size / csv_path are read live per request / per import
}

// hotReloadFieldOverrides lists dot-notation "section.field" paths (lowercase,
// matching JSON tag names, mirroring the configFieldDenyList style) whose
// individual restart requirement disagrees with their parent section's
// default classification in hotReloadKeys:
//   - true  → the field IS re-read live even though its section is not listed
//     (or not fully covered) in hotReloadKeys.
//   - false → the field is NOT actually live even though its section is
//     listed in hotReloadKeys, so it still forces restart_required=true.
//
// Keep this in sync with the actual runtime consumers:
//   - server.memory_limit_percent  → true: cmd/server/main.go OnChange calls
//     runtimeenv.TuneMemoryLimit live. Every other server.* field (host, port,
//     enable_https, cert/key files, timeouts, max_header_bytes) only takes
//     effect when internal/server/server.go rebuilds s.httpServer in Start().
//   - features.enable_hub            → false: cmd/server/main.go only
//     constructs m.hub when the flag is already true at boot; flipping it on
//     later leaves the module nil until a restart.
//   - features.enable_auto_discovery → false: same reasoning, m.autodiscovery.
//   - analytics.max_reconstruct_events → false: internal/analytics/module.go
//     caches this value at construction and only reapplies it via
//     reconstructStats(), which runs once from Start().
var hotReloadFieldOverrides = map[string]bool{
	"server.memory_limit_percent":      true,
	"features.enable_hub":              false,
	"features.enable_auto_discovery":   false,
	"analytics.max_reconstruct_events": false,
}

// fieldRestartRequired resolves whether a single lowercase "section.field"
// path with new value newVal requires a restart, applying
// hotReloadFieldOverrides on top of the section-level default from
// hotReloadKeys.
//
// For override==false fields (live in a hot section but not actually
// hot-reloadable), a restart is only reported when newVal differs from the
// value already persisted, per prevConfig — a snapshot shaped like
// admin.Module.GetConfigMap(), captured before the update was applied. This
// matters because every real admin-panel save resends the whole containing
// section unchanged fields included (see SystemSettingsPanel.vue saveConfig,
// which PUTs config.value[section] in full), so an override key such as
// features.enable_hub is present on virtually every "features" save whether
// or not it actually changed. When prevConfig has no recorded value for the
// field (e.g. nil, as unit tests exercising this function directly with
// synthetic single-field payloads do), the field conservatively requires a
// restart, matching the previous behavior.
func fieldRestartRequired(topLevel, field string, newVal any, prevConfig map[string]any) bool {
	override, overridden := hotReloadFieldOverrides[topLevel+"."+field]
	if !overridden {
		return !hotReloadKeys[topLevel]
	}
	if override {
		return false
	}
	if oldVal, ok := lookupConfigValue(prevConfig, topLevel, field); ok && valuesEqual(newVal, oldVal) {
		return false
	}
	return true
}

// lookupConfigValue looks up "section.field" inside a config snapshot shaped
// like admin.Module.GetConfigMap() (a nested map[string]any keyed by
// lowercase JSON field names). Safe to call with a nil snapshot.
func lookupConfigValue(snapshot map[string]any, topLevel, field string) (any, bool) {
	section, ok := snapshot[topLevel].(map[string]any)
	if !ok {
		return nil, false
	}
	val, ok := section[field]
	return val, ok
}

// valuesEqual compares a JSON-decoded update value (bools/float64/strings/…)
// against a persisted config value (bools/int/string/…) for equality,
// normalizing both through JSON so, e.g., an update payload's float64(500)
// and a persisted int 500 compare equal.
func valuesEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	aj, aerr := json.Marshal(a)
	bj, berr := json.Marshal(b)
	return aerr == nil && berr == nil && bytes.Equal(aj, bj)
}

// computeRestartRequired reports whether any key in updates falls outside the
// hot-reload set, at field granularity where hotReloadFieldOverrides applies.
// Update values are either a full dot-notation path (e.g. "server.
// memory_limit_percent") mapping to a scalar, or a top-level section name
// mapping to an object of changed fields (e.g. {"analytics": {"max_reconstruct_events": 500}}) —
// both shapes are accepted by config.Manager.SetValuesBatch.
//
// prevConfig is an optional (variadic so existing single-argument call sites,
// including admin_config_restart_test.go's synthetic single-field fixtures,
// keep compiling unchanged) pre-update config snapshot from
// admin.Module.GetConfigMap(), used to resolve hotReloadFieldOverrides
// false-positives per fieldRestartRequired. Real callers (AdminUpdateConfig)
// should always pass it.
func computeRestartRequired(updates map[string]any, prevConfig ...map[string]any) bool {
	var prev map[string]any
	if len(prevConfig) > 0 {
		prev = prevConfig[0]
	}
	for k, v := range updates {
		topLevel, field, hasField := strings.Cut(strings.ToLower(k), ".")
		if hasField {
			if fieldRestartRequired(topLevel, field, v, prev) {
				return true
			}
			continue
		}
		if obj, ok := v.(map[string]any); ok {
			for field, fieldVal := range obj {
				if fieldRestartRequired(topLevel, strings.ToLower(field), fieldVal, prev) {
					return true
				}
			}
			continue
		}
		if !hotReloadKeys[topLevel] {
			return true
		}
	}
	return false
}

// AdminUpdateConfig updates the configuration (raw updates passed to admin; some changes require restart).
func (h *Handler) AdminUpdateConfig(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	var updates map[string]any
	if json.NewDecoder(c.Request.Body).Decode(&updates) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	// Reject mutations to sensitive config sections (database creds, etc.)
	rejected := filterDeniedConfigKeys(updates)
	if len(rejected) > 0 {
		h.log.Warn("Admin config update rejected keys: %v", rejected)
	}

	if len(updates) == 0 {
		writeError(c, http.StatusBadRequest, "No allowed configuration keys to update")
		return
	}

	// Snapshot the config as it stood before this update so
	// computeRestartRequired can tell an unchanged hotReloadFieldOverrides
	// field (always present when the admin panel resends a whole section)
	// apart from one that actually changed.
	previousConfig := h.admin.GetConfigMap()

	if err := h.admin.UpdateConfig(updates); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	// Apply runtime config changes to in-memory modules. Read from admin (the
	// module that just persisted the update) instead of media (which keeps its
	// own snapshot that may not have refreshed yet).
	if h.security != nil {
		updatedCfg := h.config.Get()
		h.security.SetWhitelistEnabled(updatedCfg.Security.EnableIPWhitelist)
		h.security.SetBlacklistEnabled(updatedCfg.Security.EnableIPBlacklist)
	}

	// Determine whether any updated key falls outside the hot-reload set.
	restartRequired := computeRestartRequired(updates, previousConfig)

	// Audit-log the change with redacted secrets so the existing review UI
	// surfaces it. The trackServerEvent below also writes an analytics row,
	// but logAdminAction is kept here because it carries the redacted-details
	// payload (the analytics path stores keys-only to avoid leaking secrets).
	h.logAdminAction(c, &adminLogActionParams{Action: "update_config", Target: "configuration", Details: redactSensitiveConfigKeys(updates)})
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	h.trackServerEvent(c, analytics.EventConfigUpdate, map[string]any{
		"keys":             keys,
		"restart_required": restartRequired,
	})
	result := map[string]any{
		"config":           h.admin.GetConfigMap(),
		"restart_required": restartRequired,
	}
	if len(rejected) > 0 {
		result["rejected_keys"] = rejected
	}
	writeSuccess(c, result)
}
