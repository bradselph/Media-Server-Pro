package duplicates

import (
	"testing"

	"media-server-pro/internal/repositories"
)

const testPathA = "/path/a.mp4"

// ---------------------------------------------------------------------------
// sourceFor
// ---------------------------------------------------------------------------

func TestSourceFor(t *testing.T) {
	tests := []struct {
		slaveID string
		want    string
	}{
		{"", "local"},
		{"slave-1", "receiver"},
		{"any-value", "receiver"},
	}
	for _, tc := range tests {
		got := sourceFor(tc.slaveID)
		if got != tc.want {
			t.Errorf("sourceFor(%q) = %q, want %q", tc.slaveID, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// buildLocalFingerprintGroups
// ---------------------------------------------------------------------------

func TestBuildLocalFingerprintGroups_Empty(t *testing.T) {
	groups := buildLocalFingerprintGroups(map[string]*repositories.MediaMetadata{})
	if len(groups) != 0 {
		t.Errorf("expected empty groups, got %d", len(groups))
	}
}

func TestBuildLocalFingerprintGroups_SkipsEmptyFingerprint(t *testing.T) {
	all := map[string]*repositories.MediaMetadata{
		testPathA: {ContentFingerprint: "", StableID: "id-a"},
	}
	groups := buildLocalFingerprintGroups(all)
	if len(groups) != 0 {
		t.Error("should skip entries without fingerprint")
	}
}

func TestBuildLocalFingerprintGroups_SkipsEmptyStableID(t *testing.T) {
	all := map[string]*repositories.MediaMetadata{
		testPathA: {ContentFingerprint: "fp1", StableID: ""},
	}
	groups := buildLocalFingerprintGroups(all)
	if len(groups) != 0 {
		t.Error("should skip entries without stable ID")
	}
}

func TestBuildLocalFingerprintGroups_GroupsByFingerprint(t *testing.T) {
	all := map[string]*repositories.MediaMetadata{
		testPathA:     {ContentFingerprint: "fp1", StableID: "id-a"},
		"/path/b.mp4": {ContentFingerprint: "fp1", StableID: "id-b"},
		"/path/c.mp4": {ContentFingerprint: "fp2", StableID: "id-c"},
	}
	groups := buildLocalFingerprintGroups(all)
	if len(groups) != 2 {
		t.Errorf("expected 2 fingerprint groups, got %d", len(groups))
	}
	if len(groups["fp1"]) != 2 {
		t.Errorf("fp1 group should have 2 entries, got %d", len(groups["fp1"]))
	}
	if len(groups["fp2"]) != 1 {
		t.Errorf("fp2 group should have 1 entry, got %d", len(groups["fp2"]))
	}
}

// ---------------------------------------------------------------------------
// buildReceiverFingerprintIndex
// ---------------------------------------------------------------------------

func TestBuildReceiverFingerprintIndex_Empty(t *testing.T) {
	idx := buildReceiverFingerprintIndex(nil, "slave1")
	if len(idx) != 0 {
		t.Errorf("expected empty index, got %d", len(idx))
	}
}

func TestBuildReceiverFingerprintIndex_ExcludesSlave(t *testing.T) {
	recs := []*repositories.ReceiverMediaRecord{
		{ID: "r1", SlaveID: "slave1", ContentFingerprint: "fp1", Name: "a.mp4"},
		{ID: "r2", SlaveID: "slave2", ContentFingerprint: "fp1", Name: "b.mp4"},
	}
	idx := buildReceiverFingerprintIndex(recs, "slave1")
	if len(idx["fp1"]) != 1 {
		t.Errorf("should exclude slave1, got %d entries for fp1", len(idx["fp1"]))
	}
	if idx["fp1"][0].SlaveID != "slave2" {
		t.Error("remaining entry should be from slave2")
	}
}

func TestBuildReceiverFingerprintIndex_SkipsEmptyFingerprint(t *testing.T) {
	recs := []*repositories.ReceiverMediaRecord{
		{ID: "r1", SlaveID: "slave2", ContentFingerprint: "", Name: "a.mp4"},
	}
	idx := buildReceiverFingerprintIndex(recs, "slave1")
	if len(idx) != 0 {
		t.Error("should skip entries without fingerprint")
	}
}

func TestBuildReceiverFingerprintIndex_GroupsByFingerprint(t *testing.T) {
	recs := []*repositories.ReceiverMediaRecord{
		{ID: "r1", SlaveID: "slave2", ContentFingerprint: "fp1", Name: "a.mp4"},
		{ID: "r2", SlaveID: "slave3", ContentFingerprint: "fp1", Name: "b.mp4"},
		{ID: "r3", SlaveID: "slave2", ContentFingerprint: "fp2", Name: "c.mp4"},
	}
	idx := buildReceiverFingerprintIndex(recs, "slave1")
	if len(idx["fp1"]) != 2 {
		t.Errorf("fp1 should have 2 entries, got %d", len(idx["fp1"]))
	}
	if len(idx["fp2"]) != 1 {
		t.Errorf("fp2 should have 1 entry, got %d", len(idx["fp2"]))
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "duplicates" {
		t.Errorf("Name() = %q, want %q", m.Name(), "duplicates")
	}
}

func TestModuleHealth_NotStarted(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "duplicates" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Running")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
	if h.Message != "Running" {
		t.Errorf("message = %q, want Running", h.Message)
	}
}

func TestEnabled_NilRepo(t *testing.T) {
	m := &Module{}
	if m.enabled() {
		t.Error("should not be enabled when dupRepo is nil")
	}
}

// ---------------------------------------------------------------------------
// DuplicateGroup / DuplicateItem types
// ---------------------------------------------------------------------------

func TestDuplicateItemSource(t *testing.T) {
	local := &DuplicateItem{Source: sourceFor("")}
	if local.Source != "local" {
		t.Errorf("local item source = %q", local.Source)
	}
	remote := &DuplicateItem{Source: sourceFor("s1")}
	if remote.Source != "receiver" {
		t.Errorf("remote item source = %q", remote.Source)
	}
}

// ---------------------------------------------------------------------------
// ClearForSlave / ClearPendingForSlave with nil repo (no panic)
// ---------------------------------------------------------------------------

func TestClearForSlave_NilRepo(_ *testing.T) {
	m := &Module{}
	m.ClearForSlave("slave1") // should not panic
}

func TestClearPendingForSlave_NilRepo(_ *testing.T) {
	m := &Module{}
	m.ClearPendingForSlave("slave1") // should not panic
}

// ---------------------------------------------------------------------------
// CountPending disabled
// ---------------------------------------------------------------------------

func TestCountPending_Disabled(t *testing.T) {
	m := &Module{}
	count := m.CountPending()
	if count != 0 {
		t.Errorf("CountPending on disabled module should return 0, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// ListDuplicates nil repo
// ---------------------------------------------------------------------------

func TestListDuplicates_NilRepo(t *testing.T) {
	m := &Module{}
	groups, err := m.ListDuplicates("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if groups != nil {
		t.Error("should return nil for nil repo")
	}
}

// ---------------------------------------------------------------------------
// ResolveDuplicate nil repo
// ---------------------------------------------------------------------------

func TestResolveDuplicate_NilRepo(t *testing.T) {
	m := &Module{}
	err := m.ResolveDuplicate(ResolveDuplicateInput{ID: "test", Action: "keep_both"})
	if err == nil {
		t.Error("should return error when repo is nil")
	}
}

// ---------------------------------------------------------------------------
// RecordDuplicatesFromSlave disabled
// ---------------------------------------------------------------------------

func TestRecordDuplicatesFromSlave_Disabled(_ *testing.T) {
	m := &Module{}
	// Should not panic when disabled
	m.RecordDuplicatesFromSlave("slave1", []ReceiverItemRef{
		{OpaqueID: "id1", Name: "file.mp4", ContentFingerprint: "fp1"},
	})
}
