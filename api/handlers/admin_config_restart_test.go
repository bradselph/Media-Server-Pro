package handlers

import "testing"

// TestComputeRestartRequired covers the field-level restart_required
// classification used by AdminUpdateConfig: most sections are all-or-nothing
// via hotReloadKeys, but hotReloadFieldOverrides corrects a handful of
// individual fields whose live-reload behavior disagrees with their parent
// section's default (C09: server.*, C10: features.enable_hub/
// enable_auto_discovery, C19: analytics.max_reconstruct_events).
func TestComputeRestartRequired(t *testing.T) {
	tests := []struct {
		name    string
		updates map[string]any
		want    bool
	}{
		// --- server: section is NOT hot-reload; memory_limit_percent is the
		// lone exception (cmd/server/main.go OnChange retunes it live). ---
		{
			name:    "server.port dot-path requires restart",
			updates: map[string]any{"server.port": 8080},
			want:    true,
		},
		{
			name:    "server.memory_limit_percent dot-path is hot",
			updates: map[string]any{"server.memory_limit_percent": 50},
			want:    false,
		},
		{
			name:    "server nested object with only memory_limit_percent is hot",
			updates: map[string]any{"server": map[string]any{"memory_limit_percent": 60}},
			want:    false,
		},
		{
			name: "server nested object mixing hot and cold fields requires restart",
			updates: map[string]any{"server": map[string]any{
				"memory_limit_percent": 50,
				"port":                 8080,
			}},
			want: true,
		},
		{
			name:    "server.enable_https dot-path requires restart",
			updates: map[string]any{"server.enable_https": true},
			want:    true,
		},

		// --- features: section is hot-reload, EXCEPT enable_hub/
		// enable_auto_discovery which only take effect via one-time module
		// construction in cmd/server/main.go. ---
		{
			name:    "features.enable_hub dot-path requires restart",
			updates: map[string]any{"features.enable_hub": true},
			want:    true,
		},
		{
			name:    "features.enable_auto_discovery dot-path requires restart",
			updates: map[string]any{"features.enable_auto_discovery": true},
			want:    true,
		},
		{
			name:    "features nested object with enable_hub requires restart",
			updates: map[string]any{"features": map[string]any{"enable_hub": true}},
			want:    true,
		},
		{
			name:    "features nested object with an unrelated flag is hot",
			updates: map[string]any{"features": map[string]any{"enable_hls": true}},
			want:    false,
		},

		// --- analytics: section is hot-reload (registerScheduleWatcher
		// re-applies cleanup_interval live), EXCEPT max_reconstruct_events
		// which internal/analytics/module.go only reads at construction. ---
		{
			name:    "analytics.max_reconstruct_events dot-path requires restart",
			updates: map[string]any{"analytics.max_reconstruct_events": 500},
			want:    true,
		},
		{
			name:    "analytics nested object with max_reconstruct_events requires restart",
			updates: map[string]any{"analytics": map[string]any{"max_reconstruct_events": 500}},
			want:    true,
		},
		{
			name:    "analytics.cleanup_interval dot-path is hot",
			updates: map[string]any{"analytics.cleanup_interval": 3600},
			want:    false,
		},
		{
			name:    "analytics nested object with only a hot field is hot",
			updates: map[string]any{"analytics": map[string]any{"cleanup_interval": 3600}},
			want:    false,
		},

		// --- sections outside hotReloadKeys always require a restart. ---
		{
			name:    "database section requires restart",
			updates: map[string]any{"database": map[string]any{"host": "db.example.com"}},
			want:    true,
		},

		// --- multiple keys: any single restart-requiring key wins. ---
		{
			name: "mixed hot and cold sections require restart",
			updates: map[string]any{
				"security": map[string]any{"cors_enabled": true},
				"database": map[string]any{"host": "db.example.com"},
			},
			want: true,
		},
		{
			name: "multiple hot sections stay hot",
			updates: map[string]any{
				"security": map[string]any{"cors_enabled": true},
				"features": map[string]any{"enable_hls": true},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeRestartRequired(tt.updates); got != tt.want {
				t.Errorf("computeRestartRequired(%v) = %v, want %v", tt.updates, got, tt.want)
			}
		})
	}
}
