package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// FND-0034: Regression tests ensuring ClientIPFromContext reads the stored key.

func TestFND0034_ClientIPFromContext_ReadsStoredKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)

	c.Set(ContextClientIPKey, "203.0.113.100")

	got := ClientIPFromContext(c)
	if got != "203.0.113.100" {
		t.Errorf("ClientIPFromContext = %q, want %q (FND-0034 regression)", got, "203.0.113.100")
	}
}

func TestFND0034_ClientIPFromContext_EmptyValueFallsBack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Request.RemoteAddr = "198.51.100.5:1234"

	c.Set(ContextClientIPKey, "")

	got := ClientIPFromContext(c)
	if got == "" {
		t.Error("ClientIPFromContext should fall back when stored IP is empty (FND-0034 regression)")
	}
}

func TestFND0034_ClientIPFromContext_KeyAbsentFallsBack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Request.RemoteAddr = "198.51.100.7:5678"

	got := ClientIPFromContext(c)
	if got == "" {
		t.Error("ClientIPFromContext should return non-empty fallback when key absent (FND-0034 regression)")
	}
}
