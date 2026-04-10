package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

const (
	testSlug = "abc-123"
	mimeJSON = "application/json"
)

// ---------------------------------------------------------------------------
// ParseQueryInt
// ---------------------------------------------------------------------------

func TestParseQueryInt_Default(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	got := ParseQueryInt(c, "limit", QueryIntOpts{Default: 25, Min: 1, Max: 100})
	if got != 25 {
		t.Errorf("ParseQueryInt(empty) = %d, want 25", got)
	}
}

func TestParseQueryInt_ValidValue(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?limit=50", nil)

	got := ParseQueryInt(c, "limit", QueryIntOpts{Default: 25, Min: 1, Max: 100})
	if got != 50 {
		t.Errorf("ParseQueryInt(50) = %d, want 50", got)
	}
}

func TestParseQueryInt_BelowMin(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?limit=-5", nil)

	got := ParseQueryInt(c, "limit", QueryIntOpts{Default: 25, Min: 1, Max: 100})
	if got != 1 {
		t.Errorf("ParseQueryInt(-5) = %d, want 1 (min)", got)
	}
}

func TestParseQueryInt_AboveMax(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?limit=500", nil)

	got := ParseQueryInt(c, "limit", QueryIntOpts{Default: 25, Min: 1, Max: 100})
	if got != 100 {
		t.Errorf("ParseQueryInt(500) = %d, want 100 (max)", got)
	}
}

func TestParseQueryInt_Invalid(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?limit=abc", nil)

	got := ParseQueryInt(c, "limit", QueryIntOpts{Default: 25, Min: 1, Max: 100})
	if got != 25 {
		t.Errorf("ParseQueryInt(abc) = %d, want 25 (default)", got)
	}
}

func TestParseQueryInt_Zero(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?offset=0", nil)

	got := ParseQueryInt(c, "offset", QueryIntOpts{Default: 0, Min: 0, Max: 1000})
	if got != 0 {
		t.Errorf("ParseQueryInt(0) = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// ParseLimitOffset
// ---------------------------------------------------------------------------

func TestParseLimitOffset_Defaults(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	limit, offset := ParseLimitOffset(c, LimitOffsetOpts{
		DefaultLimit:  20,
		MaxLimit:      100,
		DefaultOffset: 0,
		MaxOffset:     10000,
	})
	if limit != 20 {
		t.Errorf("limit = %d, want 20", limit)
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0", offset)
	}
}

func TestParseLimitOffset_CustomValues(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?limit=50&offset=100", nil)

	limit, offset := ParseLimitOffset(c, LimitOffsetOpts{
		DefaultLimit:  20,
		MaxLimit:      100,
		DefaultOffset: 0,
		MaxOffset:     10000,
	})
	if limit != 50 {
		t.Errorf("limit = %d, want 50", limit)
	}
	if offset != 100 {
		t.Errorf("offset = %d, want 100", offset)
	}
}

func TestParseLimitOffset_Clamped(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test?limit=999&offset=99999", nil)

	limit, offset := ParseLimitOffset(c, LimitOffsetOpts{
		DefaultLimit:  20,
		MaxLimit:      100,
		DefaultOffset: 0,
		MaxOffset:     10000,
	})
	if limit != 100 {
		t.Errorf("limit = %d, want 100 (max)", limit)
	}
	if offset != 10000 {
		t.Errorf("offset = %d, want 10000 (max)", offset)
	}
}

// ---------------------------------------------------------------------------
// RequireParamID
// ---------------------------------------------------------------------------

func TestRequireParamID_Present(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: testSlug}}

	id, ok := RequireParamID(c, "id")
	if !ok {
		t.Error("expected ok=true for present param")
	}
	if id != testSlug {
		t.Errorf("id = %q, want %q", id, testSlug)
	}
}

func TestRequireParamID_Missing(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{}

	_, ok := RequireParamID(c, "id")
	if ok {
		t.Error("expected ok=false for missing param")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRequireParamID_Whitespace(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "  "}}

	_, ok := RequireParamID(c, "id")
	if ok {
		t.Error("expected ok=false for whitespace-only param")
	}
}

// ---------------------------------------------------------------------------
// RequireSession
// ---------------------------------------------------------------------------

func TestRequireSession_NoSession(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	s := RequireSession(c)
	if s != nil {
		t.Error("expected nil session")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireSession_WithSession(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	session := &models.Session{UserID: "user-1"}
	c.Set("session", session)

	s := RequireSession(c)
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.UserID != "user-1" {
		t.Errorf("session.UserID = %q, want user-1", s.UserID)
	}
}

// ---------------------------------------------------------------------------
// BindJSON
// ---------------------------------------------------------------------------

func TestBindJSON_Valid(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"test"}`
	c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	c.Request.Header.Set(headerContentType, mimeJSON)

	var dest struct {
		Name string `json:"name"`
	}
	ok := BindJSON(c, &dest, "invalid body")
	if !ok {
		t.Error("expected ok=true for valid JSON")
	}
	if dest.Name != "test" {
		t.Errorf("Name = %q, want test", dest.Name)
	}
}

func TestBindJSON_Invalid(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString("not json"))
	c.Request.Header.Set(headerContentType, mimeJSON)

	var dest struct {
		Name string `json:"name"`
	}
	ok := BindJSON(c, &dest, "bad request body")
	if ok {
		t.Error("expected ok=false for invalid JSON")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	if !containsSubstr(body, "bad request body") {
		t.Errorf("response missing custom error message: %s", body)
	}
}

func TestBindJSON_EmptyErrMsg(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString("not json"))
	c.Request.Header.Set(headerContentType, mimeJSON)

	var dest struct{}
	BindJSON(c, &dest, "")
	// Should use the default error constant
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("should use default error when errMsg is empty")
	}
}
