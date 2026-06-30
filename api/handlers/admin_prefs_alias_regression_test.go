package handlers

import (
	"encoding/json"
	"testing"

	"media-server-pro/pkg/models"
)

// TestApplyPreferencesPatch_AutoplayAliasDoesNotClobber guards the legacy
// "autoplay" alias so it can no longer overwrite the canonical "auto_play" value
// when a PATCH body carries both keys.
func TestApplyPreferencesPatch_AutoplayAliasDoesNotClobber(t *testing.T) {
	// Canonical true + alias false in one body: canonical must win.
	prefs := &models.UserPreferences{}
	applyPreferencesPatch(prefs, map[string]any{"auto_play": true, "autoplay": false})
	if !prefs.AutoPlay {
		t.Error("auto_play=true must not be clobbered by the autoplay=false alias")
	}

	// Canonical false + alias true in one body: canonical must win.
	prefs = &models.UserPreferences{AutoPlay: true}
	applyPreferencesPatch(prefs, map[string]any{"auto_play": false, "autoplay": true})
	if prefs.AutoPlay {
		t.Error("auto_play=false must not be clobbered by the autoplay=true alias")
	}

	// Alias-only still works when the canonical key is absent (backward compatible).
	prefs = &models.UserPreferences{}
	applyPreferencesPatch(prefs, map[string]any{"autoplay": true})
	if !prefs.AutoPlay {
		t.Error("autoplay alias should still set AutoPlay when auto_play is absent")
	}
}

// TestParseAdminUpdateBody_SystemKeysNotInCustomMeta verifies that system-derived
// fields routed through the admin metadata map are reserved (kept out of the
// updates map, hence out of CustomMeta) while genuine custom keys pass through.
func TestParseAdminUpdateBody_SystemKeysNotInCustomMeta(t *testing.T) {
	raw := map[string]json.RawMessage{
		"metadata": json.RawMessage(`{"duration":"120","blur_hash":"L1abc","studio":"acme"}`),
	}
	parsed, errMsg := parseAdminUpdateBody(raw)
	if errMsg != "" {
		t.Fatalf("unexpected parse error: %s", errMsg)
	}
	if _, ok := parsed.updates["duration"]; ok {
		t.Error("duration must be reserved, not routed into custom metadata")
	}
	if _, ok := parsed.updates["blur_hash"]; ok {
		t.Error("blur_hash must be reserved, not routed into custom metadata")
	}
	if v, ok := parsed.updates["studio"]; !ok || v != "acme" {
		t.Errorf("a non-reserved custom key should pass through; got %v ok=%v", v, ok)
	}
}
