package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminGetConfig returns the current configuration
func (h *Handler) AdminGetConfig(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg := h.admin.GetConfigMap()
	writeSuccess(c, cfg)
}

// AdminUpdateConfig updates the configuration
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

	h.logAdminAction(c, &adminLogActionParams{UserID: "admin", Username: "admin", Action: "update_config", Target: "configuration", Details: updates})
	writeSuccess(c, h.admin.GetConfigMap())
}
