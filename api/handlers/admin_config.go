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
	if !h.requireAdmin(c) {
		return
	}
	cfg := h.admin.GetConfigMap()
	writeSuccess(c, cfg)
}

// AdminUpdateConfig updates the configuration (raw updates passed to admin; some changes require restart).
func (h *Handler) AdminUpdateConfig(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var updates map[string]interface{}
	if json.NewDecoder(c.Request.Body).Decode(&updates) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
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

	h.logAdminAction(c, &adminLogActionParams{UserID: "admin", Username: "admin", Action: "update_config", Target: "configuration", Details: redactSensitiveConfigKeys(updates)})
	writeSuccess(c, h.admin.GetConfigMap())
}
