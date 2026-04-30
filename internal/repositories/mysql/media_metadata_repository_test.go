package mysql

import "testing"

func TestEscapeLike_NoSpecialChars(t *testing.T) {
	if got := escapeLike("hello"); got != "hello" {
		t.Errorf("escapeLike(%q) = %q, want %q", "hello", got, "hello")
	}
}

func TestEscapeLike_Percent(t *testing.T) {
	if got := escapeLike("100%"); got != `100\%` {
		t.Errorf("escapeLike(%q) = %q, want %q", "100%", got, `100\%`)
	}
}

func TestEscapeLike_Underscore(t *testing.T) {
	if got := escapeLike("my_file"); got != `my\_file` {
		t.Errorf("escapeLike(%q) = %q, want %q", "my_file", got, `my\_file`)
	}
}

func TestEscapeLike_Backslash(t *testing.T) {
	if got := escapeLike(`C:\path`); got != `C:\\path` {
		t.Errorf(`escapeLike("C:\path") = %q, want %q`, got, `C:\\path`)
	}
}

func TestEscapeLike_AllSpecialChars(t *testing.T) {
	input := `50%_done\`
	want := `50\%\_done\\`
	if got := escapeLike(input); got != want {
		t.Errorf("escapeLike(%q) = %q, want %q", input, got, want)
	}
}

func TestEscapeLike_EmptyString(t *testing.T) {
	if got := escapeLike(""); got != "" {
		t.Errorf("escapeLike(%q) = %q, want %q", "", got, "")
	}
}

func TestEscapeLike_BackslashBeforePercent(t *testing.T) {
	input := `\%`
	want := `\\\%`
	if got := escapeLike(input); got != want {
		t.Errorf("escapeLike(%q) = %q, want %q", input, got, want)
	}
}

func TestMediaMetadataRow_TableName(t *testing.T) {
	row := mediaMetadataRow{}
	if row.TableName() != "media_metadata" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "media_metadata")
	}
}

func TestMediaTagRow_TableName(t *testing.T) {
	row := mediaTagRow{}
	if row.TableName() != "media_tags" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "media_tags")
	}
}

func TestPlaybackPositionRow_TableName(t *testing.T) {
	row := playbackPositionRow{}
	if row.TableName() != "playback_positions" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "playback_positions")
	}
}
