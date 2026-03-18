package categorizer

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Category constants
// ---------------------------------------------------------------------------

func TestCategoryConstants(t *testing.T) {
	cats := []Category{CategoryMovies, CategoryTVShows, CategoryDocumentaries, CategoryAnime, CategoryMusic, CategoryPodcasts, CategoryAudiobooks, CategoryUncategorized}
	seen := make(map[Category]bool)
	for _, c := range cats {
		if c == "" {
			t.Error("category constant should not be empty")
		}
		if seen[c] {
			t.Errorf("duplicate category: %s", c)
		}
		seen[c] = true
	}
}

// ---------------------------------------------------------------------------
// parseNumber
// ---------------------------------------------------------------------------

func TestParseNumber_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"2020abc", 2020},
		{"123", 123},
	}
	for _, tc := range tests {
		got := parseNumber(tc.input)
		if got != tc.want {
			t.Errorf("parseNumber(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestParseNumber_Empty(t *testing.T) {
	if parseNumber("") != 0 {
		t.Error("parseNumber('') should return 0")
	}
}

func TestParseNumber_NoDigits(t *testing.T) {
	if parseNumber("abc") != 0 {
		t.Error("parseNumber('abc') should return 0")
	}
}

func TestParseNumber_EmbeddedDigits(t *testing.T) {
	got := parseNumber("S01")
	if got != 1 {
		t.Errorf("parseNumber('S01') = %d, want 1 (first digit group)", got)
	}
}

// ---------------------------------------------------------------------------
// copyItem
// ---------------------------------------------------------------------------

func TestCopyItem_Nil(t *testing.T) {
	got := copyItem(nil)
	if got != nil {
		t.Error("copyItem(nil) should return nil")
	}
}

func TestCopyItem_DeepCopy(t *testing.T) {
	src := &CategorizedItem{
		ID:       "cat-1",
		Category: CategoryMovies,
		Path:     "/videos/movie.mp4",
	}
	dst := copyItem(src)
	if dst.ID != src.ID {
		t.Errorf("ID = %q, want %q", dst.ID, src.ID)
	}
	if dst.Category != src.Category {
		t.Errorf("Category = %q, want %q", dst.Category, src.Category)
	}
	// Mutation should not affect original
	dst.Category = CategoryMusic
	if src.Category == CategoryMusic {
		t.Error("mutating copy should not affect original")
	}
}

// ---------------------------------------------------------------------------
// NewPathContext
// ---------------------------------------------------------------------------

func TestNewPathContext_Simple(t *testing.T) {
	ctx := NewPathContext("/videos/movies/action/film.mp4")
	if ctx.Filename == "" {
		t.Error("Filename should not be empty")
	}
	if ctx.DirPath == "" {
		t.Error("DirPath should not be empty")
	}
	if ctx.FullPath == "" {
		t.Error("FullPath should not be empty")
	}
}

func TestNewPathContext_Lowercase(t *testing.T) {
	ctx := NewPathContext("/Videos/Movie.MP4")
	if ctx.Filename != "movie.mp4" {
		t.Errorf("Filename should be lowercased: %q", ctx.Filename)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "categorizer" {
		t.Errorf("Name() = %q, want %q", m.Name(), "categorizer")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "categorizer" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}
