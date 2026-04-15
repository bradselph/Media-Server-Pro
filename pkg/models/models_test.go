package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	testUnmarshalFmt = "Unmarshal failed: %v"
	testMarshalFmt   = "Marshal failed: %v"
)

// ---------------------------------------------------------------------------
// Session expiry
// ---------------------------------------------------------------------------

func TestSession_IsExpired_Future(t *testing.T) {
	s := &Session{ExpiresAt: time.Now().Add(1 * time.Hour)}
	if s.IsExpired() {
		t.Error("session with future ExpiresAt should not be expired")
	}
}

func TestSession_IsExpired_Past(t *testing.T) {
	s := &Session{ExpiresAt: time.Now().Add(-1 * time.Second)}
	if !s.IsExpired() {
		t.Error("session with past ExpiresAt should be expired")
	}
}

// ---------------------------------------------------------------------------
// AdminSession
// ---------------------------------------------------------------------------

func TestAdminSession_IsAdmin(t *testing.T) {
	admin := &AdminSession{Session: Session{Role: RoleAdmin}}
	if !admin.IsAdmin() {
		t.Error("AdminSession with RoleAdmin should return IsAdmin true")
	}
	viewer := &AdminSession{Session: Session{Role: RoleViewer}}
	if viewer.IsAdmin() {
		t.Error("AdminSession with RoleViewer should return IsAdmin false")
	}
}

func TestAdminSession_MarshalJSON_IncludesIsAdmin(t *testing.T) {
	a := &AdminSession{Session: Session{
		ID:   "sess-1",
		Role: RoleAdmin,
	}}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf(testUnmarshalFmt, err)
	}
	isAdmin, ok := m["is_admin"]
	if !ok {
		t.Fatal("MarshalJSON output missing is_admin field")
	}
	if isAdmin != true {
		t.Errorf("expected is_admin=true, got %v", isAdmin)
	}
}

// ---------------------------------------------------------------------------
// User JSON - PasswordHash and Salt must be excluded
// ---------------------------------------------------------------------------

func TestUser_JSON_ExcludesSecrets(t *testing.T) {
	u := User{
		ID:           "u-1",
		Username:     "alice",
		PasswordHash: "secret-hash",
		Salt:         "secret-salt",
		Role:         RoleViewer,
	}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf(testMarshalFmt, err)
	}
	raw := string(data)
	if strings.Contains(raw, "secret-hash") {
		t.Error("PasswordHash leaked into JSON output")
	}
	if strings.Contains(raw, "secret-salt") {
		t.Error("Salt leaked into JSON output")
	}
}

// ---------------------------------------------------------------------------
// MediaItem JSON - Path must be excluded
// ---------------------------------------------------------------------------

func TestMediaItem_JSON_ExcludesPath(t *testing.T) {
	item := MediaItem{
		ID:   "m-1",
		Name: "test.mp4",
		Path: "/secret/path/test.mp4",
		Type: MediaTypeVideo,
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf(testMarshalFmt, err)
	}
	if strings.Contains(string(data), "/secret/path") {
		t.Error("MediaItem.Path leaked into JSON output")
	}
}

// ---------------------------------------------------------------------------
// UserPreferences validation
// ---------------------------------------------------------------------------

func TestUserPreferences_Validate_Defaults(t *testing.T) {
	p := UserPreferences{}
	p.Validate()

	if p.PlaybackSpeed != 1.0 {
		t.Errorf("expected default PlaybackSpeed=1.0, got %v", p.PlaybackSpeed)
	}
	if p.Volume != 0 {
		t.Errorf("expected Volume=0 (clamped from 0), got %v", p.Volume)
	}
	if p.ItemsPerPage != 20 {
		t.Errorf("expected default ItemsPerPage=20, got %v", p.ItemsPerPage)
	}
}

func TestUserPreferences_Validate_ClampSpeed(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{0.1, 0.25},  // below minimum -> 0.25
		{0.25, 0.25}, // at minimum
		{1.0, 1.0},   // normal
		{3.0, 3.0},   // at maximum
		{5.0, 1.0},   // above maximum -> default 1.0
		{-1.0, 1.0},  // negative -> default 1.0
		{0.0, 1.0},   // zero -> default 1.0
	}
	for _, tc := range tests {
		p := UserPreferences{PlaybackSpeed: tc.input, Volume: 0.5, ItemsPerPage: 10}
		p.Validate()
		if p.PlaybackSpeed != tc.want {
			t.Errorf("PlaybackSpeed(%v) -> %v, want %v", tc.input, p.PlaybackSpeed, tc.want)
		}
	}
}

func TestUserPreferences_Validate_ClampVolume(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{-0.5, 0},
		{0.0, 0.0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tc := range tests {
		p := UserPreferences{Volume: tc.input, PlaybackSpeed: 1.0, ItemsPerPage: 10}
		p.Validate()
		if p.Volume != tc.want {
			t.Errorf("Volume(%v) -> %v, want %v", tc.input, p.Volume, tc.want)
		}
	}
}

func TestUserPreferences_Validate_ClampItemsPerPage(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 20},    // non-positive -> default 20
		{-5, 20},   // negative -> default 20
		{1, 1},     // minimum
		{100, 100}, // normal
		{200, 200}, // at maximum
		{201, 200}, // above maximum -> 200
	}
	for _, tc := range tests {
		p := UserPreferences{ItemsPerPage: tc.input, PlaybackSpeed: 1.0, Volume: 0.5}
		p.Validate()
		if p.ItemsPerPage != tc.want {
			t.Errorf("ItemsPerPage(%v) -> %v, want %v", tc.input, p.ItemsPerPage, tc.want)
		}
	}
}

func TestUserPreferences_Validate_Theme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"light", "light"},
		{"dark", "dark"},
		{"auto", "auto"},
		{"", ""},
		{"invalid", "auto"},
	}
	for _, tc := range tests {
		p := UserPreferences{Theme: tc.input, PlaybackSpeed: 1.0, Volume: 0.5, ItemsPerPage: 10}
		p.Validate()
		if p.Theme != tc.want {
			t.Errorf("Theme(%q) -> %q, want %q", tc.input, p.Theme, tc.want)
		}
	}
}

func TestUserPreferences_Validate_ViewMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"grid", "grid"},
		{"list", "list"},
		{"compact", "compact"},
		{"invalid", "grid"},
	}
	for _, tc := range tests {
		p := UserPreferences{ViewMode: tc.input, PlaybackSpeed: 1.0, Volume: 0.5, ItemsPerPage: 10}
		p.Validate()
		if p.ViewMode != tc.want {
			t.Errorf("ViewMode(%q) -> %q, want %q", tc.input, p.ViewMode, tc.want)
		}
	}
}

func TestUserPreferences_Validate_SortOrder(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"asc", "asc"},
		{"desc", "desc"},
		{"", ""},
		{"random", "asc"},
	} {
		p := UserPreferences{SortOrder: tc.input, PlaybackSpeed: 1.0, Volume: 0.5, ItemsPerPage: 10}
		p.Validate()
		if p.SortOrder != tc.want {
			t.Errorf("SortOrder(%q) -> %q, want %q", tc.input, p.SortOrder, tc.want)
		}
	}
}

func TestUserPreferences_Validate_TruncatesLongStrings(t *testing.T) {
	long := strings.Repeat("x", 200)
	p := UserPreferences{
		DefaultQuality:  long,
		Language:        long,
		EqualizerPreset: long,
		SortBy:          long,
		FilterCategory:  long,
		FilterMediaType: long,
		PlaybackSpeed:   1.0,
		Volume:          0.5,
		ItemsPerPage:    10,
	}
	p.Validate()

	if len(p.DefaultQuality) > 50 {
		t.Errorf("DefaultQuality not truncated to 50, got %d", len(p.DefaultQuality))
	}
	if len(p.Language) > 10 {
		t.Errorf("Language not truncated to 10, got %d", len(p.Language))
	}
	if len(p.EqualizerPreset) > 100 {
		t.Errorf("EqualizerPreset not truncated to 100, got %d", len(p.EqualizerPreset))
	}
	if len(p.SortBy) > 50 {
		t.Errorf("SortBy not truncated to 50, got %d", len(p.SortBy))
	}
	if len(p.FilterCategory) > 100 {
		t.Errorf("FilterCategory not truncated to 100, got %d", len(p.FilterCategory))
	}
	if len(p.FilterMediaType) > 50 {
		t.Errorf("FilterMediaType not truncated to 50, got %d", len(p.FilterMediaType))
	}
}

// ---------------------------------------------------------------------------
// UserPreferences UnmarshalJSON - canonical and alias keys
// ---------------------------------------------------------------------------

func TestUserPreferences_UnmarshalJSON_Canonical(t *testing.T) {
	data := `{"auto_play": true, "equalizer_preset": "rock"}`
	var p UserPreferences
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf(testUnmarshalFmt, err)
	}
	if !p.AutoPlay {
		t.Error("expected AutoPlay=true")
	}
	if p.EqualizerPreset != "rock" {
		t.Errorf("expected EqualizerPreset=rock, got %q", p.EqualizerPreset)
	}
}

func TestUserPreferences_UnmarshalJSON_Aliases(t *testing.T) {
	data := `{"autoplay": true, "equalizer_bands": "jazz"}`
	var p UserPreferences
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf(testUnmarshalFmt, err)
	}
	if !p.AutoPlay {
		t.Error("expected AutoPlay=true via autoplay alias")
	}
	if p.EqualizerPreset != "jazz" {
		t.Errorf("expected EqualizerPreset=jazz via equalizer_bands alias, got %q", p.EqualizerPreset)
	}
}

func TestUserPreferences_UnmarshalJSON_AmbiguousError(t *testing.T) {
	data := `{"auto_play": true, "autoplay": false}`
	var p UserPreferences
	err := json.Unmarshal([]byte(data), &p)
	if err == nil {
		t.Fatal("expected error for ambiguous auto_play + autoplay")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got: %v", err)
	}
}

func TestUserPreferences_UnmarshalJSON_AmbiguousEqualizer(t *testing.T) {
	data := `{"equalizer_preset": "rock", "equalizer_bands": "jazz"}`
	var p UserPreferences
	err := json.Unmarshal([]byte(data), &p)
	if err == nil {
		t.Fatal("expected error for ambiguous equalizer_preset + equalizer_bands")
	}
}

func TestUserPreferences_UnmarshalJSON_CanonicalOverridesAlias(t *testing.T) {
	// When only canonical is present, alias should not interfere
	data := `{"auto_play": false, "theme": "dark"}`
	var p UserPreferences
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf(testUnmarshalFmt, err)
	}
	if p.AutoPlay != false {
		t.Error("expected AutoPlay=false")
	}
	if p.Theme != "dark" {
		t.Errorf("expected Theme=dark, got %q", p.Theme)
	}
}

func TestUserPreferences_MarshalJSON_NoAliases(t *testing.T) {
	p := UserPreferences{AutoPlay: true, EqualizerPreset: "rock"}
	data, err := json.Marshal(&p)
	if err != nil {
		t.Fatalf(testMarshalFmt, err)
	}
	raw := string(data)
	if strings.Contains(raw, "autoplay") {
		t.Error("MarshalJSON should not emit alias 'autoplay'")
	}
	if strings.Contains(raw, "equalizer_bands") {
		t.Error("MarshalJSON should not emit alias 'equalizer_bands'")
	}
	if !strings.Contains(raw, "auto_play") {
		t.Error("MarshalJSON should emit canonical 'auto_play'")
	}
}

// ---------------------------------------------------------------------------
// WatchHistoryItem - Path excluded from JSON
// ---------------------------------------------------------------------------

func TestWatchHistoryItem_JSON_ExcludesPath(t *testing.T) {
	w := WatchHistoryItem{
		MediaID:   "m-1",
		MediaPath: "/secret/path.mp4",
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf(testMarshalFmt, err)
	}
	if strings.Contains(string(data), "/secret/path") {
		t.Error("MediaPath leaked into JSON output")
	}
}

// ---------------------------------------------------------------------------
// PlaylistItem - MediaPath excluded from JSON
// ---------------------------------------------------------------------------

func TestPlaylistItem_JSON_ExcludesMediaPath(t *testing.T) {
	pi := PlaylistItem{
		ID:        "pi-1",
		MediaPath: "/secret/playlist/video.mp4",
	}
	data, err := json.Marshal(pi)
	if err != nil {
		t.Fatalf(testMarshalFmt, err)
	}
	if strings.Contains(string(data), "/secret/playlist") {
		t.Error("PlaylistItem.MediaPath leaked into JSON output")
	}
}

// ---------------------------------------------------------------------------
// HLSJob - Path excluded from JSON
// ---------------------------------------------------------------------------

func TestHLSJob_JSON_ExcludesPaths(t *testing.T) {
	j := HLSJob{
		ID:        "hls-1",
		MediaPath: "/secret/media.mp4",
		OutputDir: "/secret/output/",
		Status:    HLSStatusPending,
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatalf(testMarshalFmt, err)
	}
	raw := string(data)
	if strings.Contains(raw, "/secret/media") {
		t.Error("HLSJob.MediaPath leaked into JSON output")
	}
	if strings.Contains(raw, "/secret/output") {
		t.Error("HLSJob.OutputDir leaked into JSON output")
	}
}

// ---------------------------------------------------------------------------
// Table names
// ---------------------------------------------------------------------------

func TestTableNames(t *testing.T) {
	tests := []struct {
		name  string
		table string
	}{
		{"User", (&User{}).TableName()},
		{"UserPermissions", UserPermissions{}.TableName()},
		{"UserPreferences", (&UserPreferences{}).TableName()},
		{"Session", (&Session{}).TableName()},
		{"PlaybackPosition", PlaybackPosition{}.TableName()},
		{"MediaTag", MediaTag{}.TableName()},
		{"Playlist", Playlist{}.TableName()},
		{"PlaylistItem", PlaylistItem{}.TableName()},
		{"AnalyticsEvent", AnalyticsEvent{}.TableName()},
		{"AuditLogEntry", AuditLogEntry{}.TableName()},
	}
	for _, tc := range tests {
		if tc.table == "" {
			t.Errorf("%s.TableName() is empty", tc.name)
		}
	}
}

// ---------------------------------------------------------------------------
// MediaType constants
// ---------------------------------------------------------------------------

func TestMediaTypeConstants(t *testing.T) {
	if MediaTypeVideo != "video" {
		t.Errorf("MediaTypeVideo = %q, want 'video'", MediaTypeVideo)
	}
	if MediaTypeAudio != "audio" {
		t.Errorf("MediaTypeAudio = %q, want 'audio'", MediaTypeAudio)
	}
	if MediaTypeUnknown != "unknown" {
		t.Errorf("MediaTypeUnknown = %q, want 'unknown'", MediaTypeUnknown)
	}
}

// ---------------------------------------------------------------------------
// HLSStatus constants
// ---------------------------------------------------------------------------

func TestHLSStatusConstants(t *testing.T) {
	statuses := map[HLSStatus]string{
		HLSStatusPending:   "pending",
		HLSStatusRunning:   "running",
		HLSStatusCompleted: "completed",
		HLSStatusFailed:    "failed",
		HLSStatusCanceled:  "canceled",
	}
	for s, want := range statuses {
		if string(s) != want {
			t.Errorf("HLSStatus %v != %q", s, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Health status constants
// ---------------------------------------------------------------------------

func TestHealthStatusConstants(t *testing.T) {
	if StatusHealthy != "healthy" {
		t.Errorf("StatusHealthy = %q", StatusHealthy)
	}
	if StatusUnhealthy != "unhealthy" {
		t.Errorf("StatusUnhealthy = %q", StatusUnhealthy)
	}
}

// ---------------------------------------------------------------------------
// UserRole constants
// ---------------------------------------------------------------------------

func TestUserRoleConstants(t *testing.T) {
	if RoleAdmin != "admin" {
		t.Errorf("RoleAdmin = %q", RoleAdmin)
	}
	if RoleViewer != "viewer" {
		t.Errorf("RoleViewer = %q", RoleViewer)
	}
}

// ---------------------------------------------------------------------------
// SuggestionType constants
// ---------------------------------------------------------------------------

func TestSuggestionTypeConstants(t *testing.T) {
	for _, tc := range []struct {
		got  SuggestionType
		want string
	}{
		{SuggestionTypeMovie, "movie"},
		{SuggestionTypeTVEpisode, "tv_episode"},
		{SuggestionTypeMusic, "music"},
		{SuggestionTypeUnknown, "unknown"},
	} {
		if string(tc.got) != tc.want {
			t.Errorf("SuggestionType %v != %q", tc.got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Internal helper: truncateString
// ---------------------------------------------------------------------------

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 0, "hello"}, // maxLen <= 0 returns original
		{"", 5, ""},
		{"日本語テスト", 3, "日本語"}, // multi-byte: truncates by rune count
	}
	for _, tc := range tests {
		got := truncateString(tc.input, tc.maxLen)
		if got != tc.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Internal helper: stringInSetOrDefault
// ---------------------------------------------------------------------------

func TestStringInSetOrDefault(t *testing.T) {
	allowed := map[string]bool{"a": true, "b": true}
	tests := []struct {
		input  string
		defVal string
		want   string
	}{
		{"a", "x", "a"},
		{"b", "x", "b"},
		{"c", "x", "x"}, // not in set -> default
		{"", "x", ""},   // empty -> empty (treated as valid)
	}
	for _, tc := range tests {
		got := stringInSetOrDefault(tc.input, allowed, tc.defVal)
		if got != tc.want {
			t.Errorf("stringInSetOrDefault(%q, ..., %q) = %q, want %q", tc.input, tc.defVal, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Internal helpers: clamp functions
// ---------------------------------------------------------------------------

func TestClampPlaybackSpeed(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{0, 1.0},
		{-1, 1.0},
		{0.1, 0.25},
		{0.25, 0.25},
		{1.5, 1.5},
		{3.0, 3.0},
		{3.1, 1.0},
	}
	for _, tc := range tests {
		got := clampPlaybackSpeed(tc.in)
		if got != tc.want {
			t.Errorf("clampPlaybackSpeed(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestClampVolume(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{-1, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{2.0, 1.0},
	}
	for _, tc := range tests {
		got := clampVolume(tc.in)
		if got != tc.want {
			t.Errorf("clampVolume(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestClampItemsPerPage(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{0, 20},
		{-5, 20},
		{1, 1},
		{100, 100},
		{200, 200},
		{300, 200},
	}
	for _, tc := range tests {
		got := clampItemsPerPage(tc.in)
		if got != tc.want {
			t.Errorf("clampItemsPerPage(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
