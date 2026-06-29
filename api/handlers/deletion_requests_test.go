package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// stubDeletionRepo is an in-memory DataDeletionRequestRepository for handler
// unit tests. Each test fixes the Get result/error and the UpdateStatus error,
// and the stub records the UpdateStatus call so tests can assert on it.
type stubDeletionRepo struct {
	getRecord *repositories.DataDeletionRequestRecord
	getErr    error
	updateErr error

	updateCalls  int
	lastStatus   string
	lastReviewer string
	lastNotes    string
}

func (s *stubDeletionRepo) Create(context.Context, *repositories.DataDeletionRequestRecord) error {
	return nil
}

func (s *stubDeletionRepo) Get(context.Context, string) (*repositories.DataDeletionRequestRecord, error) {
	return s.getRecord, s.getErr
}

func (s *stubDeletionRepo) ListByStatus(context.Context, string) ([]*repositories.DataDeletionRequestRecord, error) {
	return nil, nil
}

func (s *stubDeletionRepo) CountPendingByUser(context.Context, string) (int64, error) {
	return 0, nil
}

func (s *stubDeletionRepo) UpdateStatus(_ context.Context, _, status, reviewedBy, adminNotes string) error {
	s.updateCalls++
	s.lastStatus = status
	s.lastReviewer = reviewedBy
	s.lastNotes = adminNotes
	return s.updateErr
}

// newProcessHandler builds a minimal Handler wired only with the deletion repo
// and a logger. analytics/admin are nil — trackServerEvent nil-guards both, so
// the success path is safe. The approve branch is intentionally out of scope
// here: it calls the concrete *auth.Module, which can't be stubbed, so it's
// covered by the DB-backed integration test instead.
func newProcessHandler(repo repositories.DataDeletionRequestRepository) *Handler {
	return &Handler{
		deletionRequests: repo,
		log:              logger.New("deletion-test"),
	}
}

// newProcessContext builds an admin-authenticated POST context for
// AdminProcessDeletionRequest with the given :id param and JSON body.
func newProcessContext(id, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/admin/data-deletion-requests/"+id+"/process", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set("session", &models.Session{
		ID: "admin-sess", UserID: "admin-id", Username: "admin", Role: models.RoleAdmin,
	})
	return c, rec
}

func pendingRecord() *repositories.DataDeletionRequestRecord {
	return &repositories.DataDeletionRequestRecord{
		ID: "req-1", UserID: "u-1", Username: "victim", Email: "v@example.com",
		Status: string(models.DeletionRequestPending),
	}
}

func TestAdminProcessDeletionRequest_InvalidAction(t *testing.T) {
	repo := &stubDeletionRepo{getRecord: pendingRecord()}
	h := newProcessHandler(repo)
	c, rec := newProcessContext("req-1", `{"action":"explode"}`)

	h.AdminProcessDeletionRequest(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid action: expected 400, got %d", rec.Code)
	}
	if repo.updateCalls != 0 {
		t.Errorf("invalid action must not touch the repo: UpdateStatus called %d times", repo.updateCalls)
	}
}

func TestAdminProcessDeletionRequest_NotesTooLong(t *testing.T) {
	repo := &stubDeletionRepo{getRecord: pendingRecord()}
	h := newProcessHandler(repo)
	body := `{"action":"deny","admin_notes":"` + strings.Repeat("x", 2001) + `"}`
	c, rec := newProcessContext("req-1", body)

	h.AdminProcessDeletionRequest(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("over-long admin notes: expected 400, got %d", rec.Code)
	}
	if repo.updateCalls != 0 {
		t.Errorf("validation failure must not touch the repo: UpdateStatus called %d times", repo.updateCalls)
	}
}

func TestAdminProcessDeletionRequest_NotFound(t *testing.T) {
	repo := &stubDeletionRepo{getRecord: nil} // Get returns (nil, nil) → not found
	h := newProcessHandler(repo)
	c, rec := newProcessContext("missing", `{"action":"deny"}`)

	h.AdminProcessDeletionRequest(c)

	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown request: expected 404, got %d", rec.Code)
	}
}

func TestAdminProcessDeletionRequest_AlreadyProcessed(t *testing.T) {
	rec0 := pendingRecord()
	rec0.Status = string(models.DeletionRequestApproved)
	repo := &stubDeletionRepo{getRecord: rec0}
	h := newProcessHandler(repo)
	c, rec := newProcessContext("req-1", `{"action":"deny"}`)

	h.AdminProcessDeletionRequest(c)

	if rec.Code != http.StatusConflict {
		t.Errorf("already-processed request: expected 409, got %d", rec.Code)
	}
	if repo.updateCalls != 0 {
		t.Errorf("already-processed request must not be re-updated: UpdateStatus called %d times", repo.updateCalls)
	}
}

func TestAdminProcessDeletionRequest_DenySuccess(t *testing.T) {
	repo := &stubDeletionRepo{getRecord: pendingRecord()}
	h := newProcessHandler(repo)
	c, rec := newProcessContext("req-1", `{"action":"deny","admin_notes":"not warranted"}`)

	h.AdminProcessDeletionRequest(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("deny: expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if repo.updateCalls != 1 {
		t.Fatalf("deny: expected exactly one UpdateStatus call, got %d", repo.updateCalls)
	}
	if want := string(models.DeletionRequestDenied); repo.lastStatus != want {
		t.Errorf("deny: persisted status = %q, want %q", repo.lastStatus, want)
	}
	if repo.lastReviewer != "admin" {
		t.Errorf("deny: reviewer = %q, want %q", repo.lastReviewer, "admin")
	}
	if repo.lastNotes != "not warranted" {
		t.Errorf("deny: admin notes = %q, want %q", repo.lastNotes, "not warranted")
	}

	var env struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("deny: response not valid JSON: %v", err)
	}
	if env.Data["status"] != string(models.DeletionRequestDenied) {
		t.Errorf("deny: response status = %q, want %q", env.Data["status"], string(models.DeletionRequestDenied))
	}
}

func TestAdminProcessDeletionRequest_DenyUpdateFails(t *testing.T) {
	repo := &stubDeletionRepo{getRecord: pendingRecord(), updateErr: context.DeadlineExceeded}
	h := newProcessHandler(repo)
	c, rec := newProcessContext("req-1", `{"action":"deny"}`)

	h.AdminProcessDeletionRequest(c)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("deny with failing UpdateStatus: expected 500, got %d", rec.Code)
	}
}
