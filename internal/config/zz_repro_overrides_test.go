package config

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestRepro_TasksOverrides_AliasedAcrossGetCopy demonstrates that getCopy()
// leaves Tasks.Overrides pointing at the SAME map object as the live
// m.config, unlike every other collection field (which is deep-copied).
func TestRepro_TasksOverrides_AliasedAcrossGetCopy(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), testConfigFilename))

	if err := m.Update(func(c *Config) {
		c.Tasks.Overrides = map[string]TaskOverride{
			"seed-task": {},
		}
	}); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	cp := m.Get()

	// Mutate the live config's map via another Update call.
	if err := m.Update(func(c *Config) {
		c.Tasks.Overrides["seed-task"] = TaskOverride{ScheduleSecs: new(999)}
	}); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	// If getCopy() had deep-copied Tasks.Overrides (like it does for every
	// other slice/map field), cp's map would be untouched by the later Update.
	got := cp.Tasks.Overrides["seed-task"]
	if got.ScheduleSecs != nil {
		t.Fatalf("BUG CONFIRMED: cp (obtained via Get() BEFORE the second Update) observed the later mutation: ScheduleSecs=%v -- Tasks.Overrides map is aliased, not copied", *got.ScheduleSecs)
	}
}

// TestRepro_TasksOverrides_ConcurrentMapRaceCrash drives an actual concurrent
// map read/write using only the public Manager API (Get + Update), mirroring
// a caller that holds on to a Config snapshot (as cmd/server/main.go's
// registerWithOverride and any future status endpoint would) while another
// goroutine persists a task override. Run with -race.
func TestRepro_TasksOverrides_ConcurrentMapRaceCrash(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), testConfigFilename))
	if err := m.Update(func(c *Config) {
		c.Tasks.Overrides = map[string]TaskOverride{"seed-task": {}}
	}); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	cp := m.Get() // snapshot; cp.Tasks.Overrides aliases the live map

	var wg sync.WaitGroup
	wg.Add(2)

	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = m.Update(func(c *Config) {
				c.Tasks.Overrides["seed-task"] = TaskOverride{ScheduleSecs: new(i)}
			})
		}
	}()
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < 2000; i++ {
			// Unsynchronized read of the map obtained from an earlier Get(),
			// exactly like main.go's `cfg.Get().Tasks.Overrides[reg.ID]`
			// pattern would do if invoked concurrently with an admin write.
			_ = cp.Tasks.Overrides["seed-task"]
		}
	}()
	wg.Wait()
}
