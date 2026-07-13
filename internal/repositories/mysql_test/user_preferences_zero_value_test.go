package mysql_test

import (
	"context"
	"testing"
	"time"

	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/internal/testutil"
	"media-server-pro/pkg/models"
)

// TestUserPreferences_ZeroValuesPersist is a regression test for the GORM
// default-tag zero-value substitution bug (R5 sweep): Volume=0 (mute),
// AutoplaySimilar=false (opt-out), and AccentHue=0 (a valid hue) were silently
// reverted to their gorm `default:` values on every Upsert, because GORM's
// Create/OnConflict substitutes the default tag for any zero-valued field. The
// fix removed the `default:` tags from those three fields (new-user defaults are
// applied in defaultUserPreferences instead).
func TestUserPreferences_ZeroValuesPersist(t *testing.T) {
	cfg := testutil.TestConfig(t)
	dbModule := testutil.TestDBModule(t, cfg)
	repo := mysql.NewUserPreferencesRepository(dbModule.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	prefs := &models.UserPreferences{
		UserID:          "user-zero",
		Volume:          0,     // muted
		AutoplaySimilar: false, // opted out
		AccentHue:       0,     // valid hue (red)
		Theme:           "dark",
	}
	if err := repo.Upsert(ctx, prefs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.Get(ctx, "user-zero")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Volume != 0 {
		t.Errorf("Volume = %v, want 0 (mute must persist, not revert to the default)", got.Volume)
	}
	if got.AutoplaySimilar {
		t.Errorf("AutoplaySimilar = true, want false (opt-out must persist)")
	}
	if got.AccentHue != 0 {
		t.Errorf("AccentHue = %v, want 0 (valid hue must persist, not revert to 220)", got.AccentHue)
	}

	// Re-saving an existing row that previously had a non-zero volume must still
	// let the user drop it back to 0 (mute), not resurrect the default.
	prefs.Volume = 0.3
	if err := repo.Upsert(ctx, prefs); err != nil {
		t.Fatalf("Upsert (0.3): %v", err)
	}
	prefs.Volume = 0
	if err := repo.Upsert(ctx, prefs); err != nil {
		t.Fatalf("Upsert (mute): %v", err)
	}
	got, err = repo.Get(ctx, "user-zero")
	if err != nil {
		t.Fatalf("Get (after re-save): %v", err)
	}
	if got.Volume != 0 {
		t.Errorf("after re-save, Volume = %v, want 0", got.Volume)
	}
}
