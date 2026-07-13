package mysql

import (
	"reflect"
	"strings"
	"testing"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// TestUserPreferences_ZeroValueFieldsHaveNoGormDefaultTag guards the R5 fix and
// runs in CI without a database (unlike the MySQL-only upsert test, which SKIPs).
// Volume (0=mute), AutoplaySimilar (false=opt-out), and AccentHue (0=valid hue)
// must NOT carry a gorm `default:` tag: GORM's Create/OnConflict upsert (both the
// UserRepository.Update path and UserPreferencesRepository.Upsert) substitutes a
// field's default tag for any zero value at INSERT, reverting the user's choice on
// every save. New-user defaults are set in auth.defaultUserPreferences() instead.
func TestUserPreferences_ZeroValueFieldsHaveNoGormDefaultTag(t *testing.T) {
	mustNotHaveDefault := map[string]bool{"Volume": true, "AutoplaySimilar": true, "AccentHue": true}
	seen := map[string]bool{}
	for f := range reflect.TypeFor[models.UserPreferences]().Fields() {
		if !mustNotHaveDefault[f.Name] {
			continue
		}
		seen[f.Name] = true
		if gormTag := f.Tag.Get("gorm"); strings.Contains(gormTag, "default:") {
			t.Errorf("UserPreferences.%s must NOT have a gorm `default:` tag (got %q); it reverts "+
				"the user's zero-value choice (mute/opt-out/hue-0) on every save", f.Name, gormTag)
		}
	}
	for name := range mustNotHaveDefault {
		if !seen[name] {
			t.Errorf("field %q not found on UserPreferences — test out of date?", name)
		}
	}
}

// userPreferencesUpsertColumns must cover every persisted UserPreferences column
// except the user_id conflict key. A column left out is silently not updated on
// ON DUPLICATE KEY UPDATE — this is the bug that dropped accent_hue. Reflect over
// the struct's db tags so adding a field without updating the list fails here.
func TestUserPreferencesUpsertColumns_CoversAllDBColumns(t *testing.T) {
	have := map[string]bool{"user_id": true} // conflict key, intentionally excluded
	for _, c := range userPreferencesUpsertColumns {
		have[c] = true
	}
	for f := range reflect.TypeFor[models.UserPreferences]().Fields() {
		col := f.Tag.Get("db")
		if col == "" || col == "-" {
			continue
		}
		if !have[col] {
			t.Errorf("UserPreferences column %q missing from userPreferencesUpsertColumns; "+
				"ON DUPLICATE KEY UPDATE will silently never persist it", col)
		}
	}
}

func TestNewUserRepository_ReturnsInterface(t *testing.T) {
	// The explicit interface type makes "constructor returns the interface" a
	// compile-time guarantee (a runtime type assertion would be tautological).
	var repo repositories.UserRepository = NewUserRepository(nil)
	if repo == nil {
		t.Fatal("NewUserRepository(nil) returned nil")
	}
}

func TestNewUserRepository_WiresSubRepos(t *testing.T) {
	repo := NewUserRepository(nil)
	ur, ok := repo.(*UserRepository)
	if !ok {
		t.Fatal("could not cast to *UserRepository")
	}
	if ur.prefsRepo == nil {
		t.Error("expected prefsRepo to be non-nil (auto-instantiated)")
	}
	if ur.permsRepo == nil {
		t.Error("expected permsRepo to be non-nil (auto-instantiated)")
	}
}

func TestMarshalJSONParam_NilReturnsNilNil(t *testing.T) {
	result, err := marshalJSONParam(nil)
	if err != nil {
		t.Fatalf("marshalJSONParam(nil) error = %v", err)
	}
	if result != nil {
		t.Errorf("marshalJSONParam(nil) = %v, want nil", result)
	}
}

func TestMarshalJSONParam_MapReturnsJSONString(t *testing.T) {
	input := map[string]any{"key": "value"}
	result, err := marshalJSONParam(input)
	if err != nil {
		t.Fatalf("marshalJSONParam(map) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("marshalJSONParam(map) returned %T, want string", result)
	}
	if str != `{"key":"value"}` {
		t.Errorf("marshalJSONParam(map) = %q, want %q", str, `{"key":"value"}`)
	}
}

func TestMarshalJSONParam_EmptyMapReturnsJSONString(t *testing.T) {
	result, err := marshalJSONParam(map[string]any{})
	if err != nil {
		t.Fatalf("marshalJSONParam(empty map) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != `{}` {
		t.Errorf("marshalJSONParam({}) = %q, want %q", str, `{}`)
	}
}

func TestMarshalJSONParam_NullableValueReturnsNil(t *testing.T) {
	var nilMap map[string]any
	result, err := marshalJSONParam(nilMap)
	if err != nil {
		t.Fatalf("marshalJSONParam(nil map) error = %v", err)
	}
	if result != nil {
		t.Errorf("marshalJSONParam(nil map) = %v, want nil (json.Marshal produces 'null')", result)
	}
}

func TestMarshalJSONParam_SliceReturnsJSONArray(t *testing.T) {
	input := []string{"a", "b", "c"}
	result, err := marshalJSONParam(input)
	if err != nil {
		t.Fatalf("marshalJSONParam(slice) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != `["a","b","c"]` {
		t.Errorf("marshalJSONParam(slice) = %q, want %q", str, `["a","b","c"]`)
	}
}

func TestMarshalJSONParam_NilSliceReturnsNil(t *testing.T) {
	var nilSlice []string
	result, err := marshalJSONParam(nilSlice)
	if err != nil {
		t.Fatalf("marshalJSONParam(nil slice) error = %v", err)
	}
	if result != nil {
		t.Errorf("marshalJSONParam(nil slice) = %v, want nil", result)
	}
}

func TestMarshalJSONParam_StringReturnsQuotedString(t *testing.T) {
	result, err := marshalJSONParam("hello")
	if err != nil {
		t.Fatalf("marshalJSONParam(string) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != `"hello"` {
		t.Errorf("marshalJSONParam(string) = %q, want %q", str, `"hello"`)
	}
}

func TestMarshalJSONParam_IntReturnsNumberString(t *testing.T) {
	result, err := marshalJSONParam(42)
	if err != nil {
		t.Fatalf("marshalJSONParam(int) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != `42` {
		t.Errorf("marshalJSONParam(int) = %q, want %q", str, `42`)
	}
}

func TestMarshalJSONParam_BoolReturnsString(t *testing.T) {
	result, err := marshalJSONParam(true)
	if err != nil {
		t.Fatalf("marshalJSONParam(bool) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != `true` {
		t.Errorf("marshalJSONParam(bool) = %q, want %q", str, `true`)
	}
}

func TestMarshalJSONParam_UnmarshalableReturnsError(t *testing.T) {
	ch := make(chan int)
	_, err := marshalJSONParam(ch)
	if err == nil {
		t.Fatal("marshalJSONParam(chan) should return an error")
	}
}

func TestMarshalJSONParam_NestedMapReturnsJSON(t *testing.T) {
	input := map[string]any{
		"outer": map[string]any{
			"inner": 123,
		},
	}
	result, err := marshalJSONParam(input)
	if err != nil {
		t.Fatalf("marshalJSONParam(nested map) error = %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != `{"outer":{"inner":123}}` {
		t.Errorf("marshalJSONParam(nested) = %q, want %q", str, `{"outer":{"inner":123}}`)
	}
}
