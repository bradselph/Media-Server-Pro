package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// redactSensitiveConfigKeys returns a copy of m with sensitive values replaced by "[REDACTED]".
// Prevents database credentials, API keys, tokens, etc. from being stored in the audit log.
func redactSensitiveConfigKeys(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	redacted := make(map[string]interface{}, len(m))
	for k, v := range m {
		keyLower := strings.ToLower(k)
		if strings.Contains(keyLower, "password") || strings.Contains(keyLower, "token") ||
			strings.Contains(keyLower, "api_key") || strings.Contains(keyLower, "secret") ||
			strings.Contains(keyLower, "deploy_key") {
			redacted[k] = "[REDACTED]"
			continue
		}
		if nested, ok := v.(map[string]interface{}); ok {
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
// runtime via the admin API. Credentials, secrets, and password hashes must be
// changed via env vars or direct config file edits only.
var configDenyList = map[string]bool{
	"database":    true, // DB host, user, password
	"auth":        true, // session secrets, lockout policy
	"admin":       true, // admin username, password_hash
	"receiver":    true, // slave API keys
	"storage":     true, // S3 access key, secret key
	"huggingface": true, // classification API key
}

// filterDeniedConfigKeys removes denied keys from the update map and returns
// the list of rejected keys.  A key is denied when its top-level section
// (the part before the first ".") appears in configDenyList, so both flat keys
// ("admin") and dot-notation keys ("admin.password_hash") are caught.
func filterDeniedConfigKeys(updates map[string]interface{}) []string {
	var rejected []string
	for k := range updates {
		topLevel := strings.SplitN(strings.ToLower(k), ".", 2)[0]
		if configDenyList[topLevel] {
			rejected = append(rejected, k)
			delete(updates, k)
		}
	}
	return rejected
}

// AdminUpdateConfig updates the configuration (raw updates passed to admin; some changes require restart).
func (h *Handler) AdminUpdateConfig(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	var updates map[string]interface{}
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

	if err := h.admin.UpdateConfig(updates); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Apply runtime config changes to in-memory modules
	if h.security != nil {
		updatedCfg := h.media.GetConfig()
		h.security.SetWhitelistEnabled(updatedCfg.Security.EnableIPWhitelist)
		h.security.SetBlacklistEnabled(updatedCfg.Security.EnableIPBlacklist)
	}

	h.logAdminAction(c, &adminLogActionParams{Action: "update_config", Target: "configuration", Details: redactSensitiveConfigKeys(updates)})
	result := map[string]interface{}{"config": h.admin.GetConfigMap()}
	if len(rejected) > 0 {
		result["rejected_keys"] = rejected
	}
	writeSuccess(c, result)
}
