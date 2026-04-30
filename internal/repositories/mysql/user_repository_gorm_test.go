package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewUserRepository_ReturnsInterface(t *testing.T) {
	repo := NewUserRepository(nil)
	if repo == nil {
		t.Fatal("NewUserRepository(nil) returned nil")
	}
	if _, ok := repo.(repositories.UserRepository); !ok {
		t.Fatal("NewUserRepository does not implement repositories.UserRepository")
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
