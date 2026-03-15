package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/api/handlers"
	"media-server-pro/api/routes"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestServer wraps a TestEnv with an HTTP test server and Gin engine,
// ready for integration testing against the full route tree.
type TestServer struct {
	Env     *TestEnv
	Handler *handlers.Handler
	Engine  *gin.Engine
	Server  *httptest.Server
}

// NewTestServer creates a TestEnv, wires up the Handler and routes,
// and starts an httptest.Server. The server is closed when the test ends.
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	env := NewTestEnv(t)

	handler := handlers.NewHandler(handlers.HandlerDeps{
		BuildInfo: handlers.BuildInfo{
			Version:   "test",
			BuildDate: "2024-01-01",
		},
		Core: handlers.HandlerCoreDeps{
			Config:    env.Config,
			Media:     env.Media,
			Streaming: env.Streaming,
			HLS:       env.HLS,
			Auth:      env.Auth,
			Database:  env.DB,
		},
		Optional: handlers.HandlerOptionalDeps{
			Playlist: env.Playlist,
			Security: env.Security,
		},
	})

	engine := gin.New()
	routes.Setup(engine, nil, handler, env.Auth, env.Security, env.Config, nil)

	srv := httptest.NewServer(engine)
	t.Cleanup(func() { srv.Close() })

	return &TestServer{
		Env:     env,
		Handler: handler,
		Engine:  engine,
		Server:  srv,
	}
}

// Request sends an HTTP request to the test server and returns the response.
// The caller is responsible for closing the response body.
func (ts *TestServer) Request(method, path string, body io.Reader) *http.Response {
	req, err := http.NewRequest(method, ts.Server.URL+path, body)
	if err != nil {
		// This is a programming error in the test itself, so panic is appropriate.
		panic("testutil.Request: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic("testutil.Request: " + err.Error())
	}
	return resp
}

// AuthRequest sends an authenticated HTTP request by attaching a session_id cookie.
// The caller is responsible for closing the response body.
func (ts *TestServer) AuthRequest(method, path string, body io.Reader, sessionID string) *http.Response {
	req, err := http.NewRequest(method, ts.Server.URL+path, body)
	if err != nil {
		panic("testutil.AuthRequest: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  "session_id",
		Value: sessionID,
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic("testutil.AuthRequest: " + err.Error())
	}
	return resp
}

// ParseJSON reads and decodes the response body as a JSON object.
// It closes the response body after reading. The response envelope
// from the server is: {"success": bool, "data": ..., "error": "..."}.
func (ts *TestServer) ParseJSON(resp *http.Response) map[string]any {
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic("testutil.ParseJSON: read body: " + err.Error())
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		panic("testutil.ParseJSON: unmarshal: " + err.Error() + " (body: " + string(data) + ")")
	}
	return result
}
