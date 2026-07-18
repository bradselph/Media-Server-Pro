package hub

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
)

// fakeHubRepo captures BatchInsert calls for importer tests and the
// query/filter passed to List/Search for GetEmbeds routing tests.
type fakeHubRepo struct {
	records []*repositories.HubEmbedRecord

	listCalled   bool
	lastListSort string

	searchCalled     bool
	lastSearchFilter repositories.HubEmbedFilter
}

func (f *fakeHubRepo) BatchInsert(_ context.Context, embeds []*repositories.HubEmbedRecord) (int64, error) {
	f.records = append(f.records, embeds...)
	return int64(len(embeds)), nil
}
func (f *fakeHubRepo) List(_ context.Context, _, _ int, sort string) ([]*repositories.HubEmbedRecord, int64, error) {
	f.listCalled = true
	f.lastListSort = sort
	return nil, 0, nil
}
func (f *fakeHubRepo) Search(_ context.Context, _ string, filter repositories.HubEmbedFilter, _, _ int) ([]*repositories.HubEmbedRecord, int64, error) {
	f.searchCalled = true
	f.lastSearchFilter = filter
	return nil, 0, nil
}
func (f *fakeHubRepo) GetByEmbedID(context.Context, string) (*repositories.HubEmbedRecord, error) {
	return nil, nil
}
func (f *fakeHubRepo) GetByEmbedIDs(context.Context, []string) ([]*repositories.HubEmbedRecord, error) {
	return nil, nil
}
func (f *fakeHubRepo) CountAll(context.Context) (int64, error)                { return int64(len(f.records)), nil }
func (f *fakeHubRepo) CategorySamples(context.Context, int) ([]string, error) { return nil, nil }
func (f *fakeHubRepo) DeleteAll(context.Context) error                        { return nil }

func TestImportCSV_StreamsAndParses(t *testing.T) {
	// Two valid rows plus one malformed (too few fields) that must be skipped.
	content := sampleRow + "\n" +
		`<iframe src="https://www.pornhub.com/embed/deadbeef"></iframe>|t|p1;p2|Second Title|a;b|C1|Star|60|100|5|1` + "\n" +
		`garbage|only|three` + "\n"
	path := filepath.Join(t.TempDir(), "sample.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write sample csv: %v", err)
	}
	repo := &fakeHubRepo{}
	read, inserted, err := ImportCSV(context.Background(), path, repo, 500, nil, nil)
	if err != nil {
		t.Fatalf("ImportCSV error: %v", err)
	}
	if read != 2 || inserted != 2 {
		t.Errorf("read=%d inserted=%d, want 2/2 (malformed row skipped)", read, inserted)
	}
	if len(repo.records) != 2 {
		t.Fatalf("captured %d records, want 2", len(repo.records))
	}
	if repo.records[1].EmbedID != "deadbeef" {
		t.Errorf("second record embed id = %q, want deadbeef", repo.records[1].EmbedID)
	}
}

const sampleRow = `<iframe src="https://www.pornhub.com/embed/c3dbc9a5d726288d8a4b" frameborder="0" height="481" width="608" scrolling="no"></iframe>|https://cdn/thumb5.jpg|https://cdn/p1.jpg;https://cdn/p2.jpg;https://cdn/p3.jpg|Gen Padova - Cum Bot|cumbots.com;machine;toys|Brunette;Toys;Solo Female|Gen Padova|185|2392561|3154|432|https://cdn/alt5.jpg|https://cdn/alt1.jpg`

// hubTestModule builds a Module wired to the fake repo with the Hub feature
// enabled, so GetEmbeds passes the ready() gate without a database.
func hubTestModule(t *testing.T, repo repositories.HubEmbedRepository) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Update(func(c *config.Config) { c.Features.EnableHub = true }); err != nil {
		t.Fatalf("enable hub feature: %v", err)
	}
	return &Module{config: cfg, repo: repo}
}

// TestGetEmbeds_SortIsHonoredOnFilteredPath guards the fix for the filtered
// listing dropping the caller's sort. Previously any active filter routed to
// Search, which pinned results to views DESC and ignored SortBy; sorting only
// worked on the unfiltered List path. GetEmbeds must forward SortBy to Search.
func TestGetEmbeds_SortIsHonoredOnFilteredPath(t *testing.T) {
	// Filtered path: a category filter must still carry the requested sort.
	fake := &fakeHubRepo{}
	m := hubTestModule(t, fake)
	if _, _, err := m.GetEmbeds(context.Background(), Filter{Category: "Toys", SortBy: "duration"}, 60, 0); err != nil {
		t.Fatalf("GetEmbeds (filtered): %v", err)
	}
	if !fake.searchCalled {
		t.Fatal("expected the Search path when a category filter is set")
	}
	if fake.lastSearchFilter.SortBy != "duration" {
		t.Errorf("Search filter SortBy = %q, want %q (sort dropped on filtered path)", fake.lastSearchFilter.SortBy, "duration")
	}
	if fake.lastSearchFilter.Category != "Toys" {
		t.Errorf("Search filter Category = %q, want %q", fake.lastSearchFilter.Category, "Toys")
	}

	// Unfiltered path: still routes to List, carrying the same sort key.
	fakeList := &fakeHubRepo{}
	mList := hubTestModule(t, fakeList)
	if _, _, err := mList.GetEmbeds(context.Background(), Filter{SortBy: "title"}, 60, 0); err != nil {
		t.Fatalf("GetEmbeds (unfiltered): %v", err)
	}
	if !fakeList.listCalled {
		t.Fatal("expected the List path when no filters are set")
	}
	if fakeList.searchCalled {
		t.Error("Search should not be called without a search/category/tag filter")
	}
	if fakeList.lastListSort != "title" {
		t.Errorf("List sort = %q, want %q", fakeList.lastListSort, "title")
	}
}

func TestOpenZippedCSV_StreamsEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "cat.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(zf)
	// A decoy non-CSV entry the picker must skip in favor of the .csv entry.
	if w, err := zw.Create("readme.txt"); err == nil {
		_, _ = io.WriteString(w, "not the catalog")
	}
	w, err := zw.Create("catalog.csv")
	if err != nil {
		t.Fatalf("zip entry: %v", err)
	}
	row2 := `<iframe src="https://www.pornhub.com/embed/aaa111"></iframe>|t|p|Row2|x|C|Star|30|50|2|0`
	if _, err := io.WriteString(w, sampleRow+"\n"+row2+"\n"); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	_ = zf.Close()

	rc, name, err := OpenZippedCSV(zipPath, nil)
	if err != nil {
		t.Fatalf("OpenZippedCSV: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if name != "catalog.csv" {
		t.Errorf("chose entry %q, want catalog.csv", name)
	}
	repo := &fakeHubRepo{}
	read, inserted, err := ImportReader(context.Background(), rc, repo, nil, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportReader: %v", err)
	}
	if read != 2 || inserted != 2 {
		t.Errorf("read=%d inserted=%d, want 2/2", read, inserted)
	}
}

func TestImportCSVWithOptions_LimitAndDryRun(t *testing.T) {
	row2 := `<iframe src="https://www.pornhub.com/embed/aaa111"></iframe>|t|p|Row2|x|C|Star|30|50|2|0`
	row3 := `<iframe src="https://www.pornhub.com/embed/bbb222"></iframe>|t|p|Row3|x|C|Star|30|50|2|0`
	content := sampleRow + "\n" + row2 + "\n" + row3 + "\n"
	path := filepath.Join(t.TempDir(), "c.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	// Limit=2 → only the first two valid rows are imported.
	repo := &fakeHubRepo{}
	read, inserted, err := ImportCSVWithOptions(context.Background(), path, repo, nil, ImportOptions{Limit: 2})
	if err != nil {
		t.Fatalf("limit import: %v", err)
	}
	if read != 2 || inserted != 2 || len(repo.records) != 2 {
		t.Errorf("limit: read=%d inserted=%d records=%d, want 2/2/2", read, inserted, len(repo.records))
	}

	// DryRun → parses every row but writes nothing.
	repo2 := &fakeHubRepo{}
	read, inserted, err = ImportCSVWithOptions(context.Background(), path, repo2, nil, ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if read != 3 || inserted != 0 || len(repo2.records) != 0 {
		t.Errorf("dry-run: read=%d inserted=%d records=%d, want 3/0/0", read, inserted, len(repo2.records))
	}
}

func TestParseLine_ValidRow(t *testing.T) {
	rec := parseLine(sampleRow)
	if rec == nil {
		t.Fatal("parseLine returned nil for a valid row")
	}
	if rec.EmbedID != "c3dbc9a5d726288d8a4b" {
		t.Errorf("EmbedID = %q, want c3dbc9a5d726288d8a4b", rec.EmbedID)
	}
	if rec.Title != "Gen Padova - Cum Bot" {
		t.Errorf("Title = %q", rec.Title)
	}
	if rec.Pornstar != "Gen Padova" {
		t.Errorf("Pornstar = %q", rec.Pornstar)
	}
	if rec.DurationSecs != 185 {
		t.Errorf("DurationSecs = %d, want 185", rec.DurationSecs)
	}
	if rec.Views != 2392561 {
		t.Errorf("Views = %d, want 2392561", rec.Views)
	}
	if rec.RatingUp != 3154 || rec.RatingDown != 432 {
		t.Errorf("ratings = %d/%d, want 3154/432", rec.RatingUp, rec.RatingDown)
	}
	if rec.ThumbURL != "https://cdn/thumb5.jpg" {
		t.Errorf("ThumbURL = %q", rec.ThumbURL)
	}
	if rec.Tags != "cumbots.com;machine;toys" {
		t.Errorf("Tags = %q", rec.Tags)
	}
	if rec.Categories != "Brunette;Toys;Solo Female" {
		t.Errorf("Categories = %q", rec.Categories)
	}
}

func TestParseLine_Invalid(t *testing.T) {
	cases := map[string]string{
		"too few fields": `a|b|c`,
		"no embed id":    `<iframe src="https://example.com/video/123"></iframe>|t|p|Title|tags|cats|star|10|20|1|0`,
		"empty":          ``,
	}
	for name, line := range cases {
		if rec := parseLine(line); rec != nil {
			t.Errorf("%s: expected nil, got %+v", name, rec)
		}
	}
}

func TestParseLine_PreviewCap(t *testing.T) {
	var previews []string
	for i := 0; i < maxPreviewURLs+15; i++ {
		previews = append(previews, "https://cdn/frame.jpg")
	}
	line := `<iframe src="https://www.pornhub.com/embed/xyz"></iframe>|thumb|` +
		strings.Join(previews, ";") + `|Title|tags|cats|star|10|20|1|0`
	rec := parseLine(line)
	if rec == nil {
		t.Fatal("parseLine returned nil")
	}
	got := len(strings.Split(rec.PreviewURLs, ";"))
	if got != maxPreviewURLs {
		t.Errorf("preview URLs stored = %d, want capped at %d", got, maxPreviewURLs)
	}
}

func TestSplitList(t *testing.T) {
	got := splitList("a; b ;;c;")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitList len = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if len(splitList("")) != 0 {
		t.Error("splitList(\"\") should be empty")
	}
}

// TestModule_DisabledIsInert verifies the non-interference requirement: when the
// feature is disabled the module reads/returns nothing and never touches a repo
// (here the db is nil, so any DB access would panic).
func TestModule_DisabledIsInert(t *testing.T) {
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if m.Get().Hub.Enabled {
		t.Fatal("precondition: Hub should be disabled by default")
	}
	mod := NewModule(m, nil) // nil db: any real query would panic

	items, total, err := mod.GetEmbeds(context.Background(), Filter{}, 60, 0)
	if err != nil || total != 0 || len(items) != 0 {
		t.Errorf("disabled GetEmbeds = (%d items, total %d, err %v), want empty", len(items), total, err)
	}
	if n, err := mod.CountAll(context.Background()); err != nil || n != 0 {
		t.Errorf("disabled CountAll = (%d, %v), want (0, nil)", n, err)
	}
	if cats, err := mod.ListCategories(context.Background()); err != nil || len(cats) != 0 {
		t.Errorf("disabled ListCategories = (%v, %v), want empty", cats, err)
	}
	if item, err := mod.GetEmbedByID(context.Background(), "abc"); err != nil || item != nil {
		t.Errorf("disabled GetEmbedByID = (%v, %v), want (nil, nil)", item, err)
	}
	// Start must no-op (no DB access) when disabled.
	if err := mod.Start(context.Background()); err != nil {
		t.Errorf("disabled Start returned error: %v", err)
	}
}

// TestModule_EnabledButNotStarted verifies queries are still safe (empty, no
// panic) when the feature is on but Start hasn't wired a repository yet.
func TestModule_EnabledButNotStarted(t *testing.T) {
	m := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := m.Load(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := m.SetValuesBatch(map[string]any{"features": map[string]any{"enable_hub": true}}); err != nil {
		t.Fatalf("enable hub: %v", err)
	}
	if !m.Get().Hub.Enabled {
		t.Fatal("precondition: Hub should be enabled")
	}
	mod := NewModule(m, nil) // repo stays nil (Start not called with a real db)

	items, total, err := mod.GetEmbeds(context.Background(), Filter{}, 60, 0)
	if err != nil || total != 0 || len(items) != 0 {
		t.Errorf("enabled-not-started GetEmbeds = (%d, %d, %v), want empty", len(items), total, err)
	}
}
