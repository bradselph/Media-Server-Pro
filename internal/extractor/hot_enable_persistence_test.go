package extractor

import (
	"context"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
)

// ---------------------------------------------------------------------------
// Regression test for C06: Start() must initialize m.repo regardless of the
// boot-time value of Extractor.Enabled, mirroring internal/receiver and
// internal/remote. Extractor.Enabled is a hot-reloadable flag (the admin
// panel can flip it without a restart), and AddItem/RemoveItem silently skip
// DB persistence whenever m.repo is nil -- so if repo construction stayed
// behind the "disabled" early return, every item added while the module was
// hot-enabled (but never restarted) would only live in the in-memory map and
// vanish on the next restart/crash.
// ---------------------------------------------------------------------------

// TestAddItem_PersistsAfterHotEnable simulates a module that booted with
// Extractor.Enabled=false (the default) and was later hot-enabled via the
// admin panel without a restart. AddItem must still persist to the
// repository, proving that repository availability does not depend on
// Extractor.Enabled having been true at boot.
//
// Start() itself is intentionally not exercised here: it obtains the
// repository via m.dbModule.GORM(), which requires a live MySQL connection
// (mysqlrepo.NewExtractorItemRepository panics on a nil *gorm.DB, and this
// module has no test seam -- nor does the project depend on a sqlite driver
// -- for injecting a fake one). The sibling federation modules (receiver,
// remote) have the same limitation and likewise never call Start() from
// their test suites; this test instead pins the observable contract that the
// Start() fix guarantees: once m.repo is set, persistence does not depend on
// the live value of Extractor.Enabled.
func TestAddItem_PersistsAfterHotEnable(t *testing.T) {
	const streamURL = "https://1.1.1.1/hot-enable-stream.m3u8"

	cfg := newExtractorTestConfig(t, 10)
	if err := cfg.Update(func(c *config.Config) {
		c.Extractor.Enabled = false
	}); err != nil {
		t.Fatalf("configure extractor as disabled: %v", err)
	}

	m := NewModule(cfg, nil)
	// Simulate what the fixed Start() now guarantees: m.repo is initialized
	// unconditionally, even though the module booted disabled.
	repo := newFakeExtractorItemRepository()
	m.repo = repo
	m.httpClient = successfulExtractorHTTPClient()

	// Hot-enable the feature without a restart, as SystemSettingsPanel.vue's
	// admin toggle does (features is a hotReloadKeys entry).
	if err := cfg.Update(func(c *config.Config) {
		c.Extractor.Enabled = true
	}); err != nil {
		t.Fatalf("hot-enable extractor: %v", err)
	}

	item, err := m.AddItem(streamURL, "Hot-enabled stream", "admin")
	if err != nil {
		t.Fatalf("AddItem after hot-enable: %v", err)
	}

	persisted, err := repo.Get(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("repo.Get: %v", err)
	}
	if persisted == nil {
		t.Fatal("item added after hot-enable was not persisted to the repository; " +
			"repo must be initialized in Start() regardless of boot-time Enabled state")
	}
}

// TestRemoveItem_PersistsAfterHotEnable mirrors TestAddItem_PersistsAfterHotEnable
// for the delete path.
func TestRemoveItem_PersistsAfterHotEnable(t *testing.T) {
	const id = "ext_hotenable_remove"

	cfg := newExtractorTestConfig(t, 10)
	if err := cfg.Update(func(c *config.Config) {
		c.Extractor.Enabled = false
	}); err != nil {
		t.Fatalf("configure extractor as disabled: %v", err)
	}

	repo := newFakeExtractorItemRepository(&repositories.ExtractorItemRecord{ID: id})
	m := NewModule(cfg, nil)
	m.repo = repo
	m.items[id] = &ExtractedItem{ID: id}

	if err := cfg.Update(func(c *config.Config) {
		c.Extractor.Enabled = true
	}); err != nil {
		t.Fatalf("hot-enable extractor: %v", err)
	}

	if err := m.RemoveItem(id); err != nil {
		t.Fatalf("RemoveItem after hot-enable: %v", err)
	}

	persisted, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("repo.Get: %v", err)
	}
	if persisted != nil {
		t.Fatal("item removed after hot-enable was not deleted from the repository; " +
			"repo must be initialized in Start() regardless of boot-time Enabled state")
	}
}
