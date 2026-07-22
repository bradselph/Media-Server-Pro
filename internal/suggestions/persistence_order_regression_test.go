package suggestions

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"media-server-pro/internal/repositories"
)

type orderedSuggestionRepo struct {
	mu            sync.Mutex
	blockSaveOnce sync.Once
	saveEntered   chan struct{}
	releaseSave   chan struct{}
	deletePathErr error
	renameErr     error
	profiles      map[string]*repositories.SuggestionProfileRecord
	history       map[string][]*repositories.ViewHistoryRecord
}

func newOrderedSuggestionRepo() *orderedSuggestionRepo {
	return &orderedSuggestionRepo{
		profiles: make(map[string]*repositories.SuggestionProfileRecord),
		history:  make(map[string][]*repositories.ViewHistoryRecord),
	}
}

func (r *orderedSuggestionRepo) SaveProfile(_ context.Context, p *repositories.SuggestionProfileRecord) error {
	if r.saveEntered != nil {
		r.blockSaveOnce.Do(func() {
			close(r.saveEntered)
			<-r.releaseSave
		})
	}
	r.mu.Lock()
	cp := *p
	r.profiles[p.UserID] = &cp
	r.mu.Unlock()
	return nil
}

func (r *orderedSuggestionRepo) GetProfile(_ context.Context, userID string) (*repositories.SuggestionProfileRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.profiles[userID]
	if p == nil {
		return nil, repositories.ErrSuggestionProfileNotFound
	}
	return new(*p), nil
}

func (r *orderedSuggestionRepo) DeleteProfile(_ context.Context, userID string) error {
	r.mu.Lock()
	delete(r.profiles, userID)
	r.mu.Unlock()
	return nil
}

func (r *orderedSuggestionRepo) ResetProfile(_ context.Context, userID string) error {
	r.mu.Lock()
	delete(r.profiles, userID)
	delete(r.history, userID)
	r.mu.Unlock()
	return nil
}

func (r *orderedSuggestionRepo) ListProfiles(context.Context) ([]*repositories.SuggestionProfileRecord, error) {
	return nil, nil
}

func (r *orderedSuggestionRepo) SaveViewHistory(_ context.Context, userID string, entry *repositories.ViewHistoryRecord) error {
	return r.BatchSaveViewHistory(context.Background(), userID, []*repositories.ViewHistoryRecord{entry})
}

func (r *orderedSuggestionRepo) BatchSaveViewHistory(_ context.Context, userID string, entries []*repositories.ViewHistoryRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]*repositories.ViewHistoryRecord, len(entries))
	for i, entry := range entries {
		cp := *entry
		copied[i] = &cp
	}
	r.history[userID] = copied
	return nil
}

func (r *orderedSuggestionRepo) GetViewHistory(_ context.Context, userID string) ([]*repositories.ViewHistoryRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*repositories.ViewHistoryRecord(nil), r.history[userID]...), nil
}

func (r *orderedSuggestionRepo) DeleteViewHistory(_ context.Context, userID string) error {
	r.mu.Lock()
	delete(r.history, userID)
	r.mu.Unlock()
	return nil
}

func (r *orderedSuggestionRepo) DeleteViewHistoryByMediaPath(context.Context, string) error {
	return r.deletePathErr
}

func (r *orderedSuggestionRepo) RenameViewHistoryMediaPath(context.Context, string, string) error {
	return r.renameErr
}

func TestRatingSaveCannotResurrectResetProfile(t *testing.T) {
	repo := newOrderedSuggestionRepo()
	repo.saveEntered = make(chan struct{})
	repo.releaseSave = make(chan struct{})
	m := NewModule(nil, nil)
	m.repo = repo

	ratingDone := make(chan struct{})
	go func() {
		m.RecordRating("user-1", "/media/a.mp4", 5)
		close(ratingDone)
	}()
	<-repo.saveEntered

	resetDone := make(chan error, 1)
	go func() { resetDone <- m.ResetUserProfile("user-1") }()
	close(repo.releaseSave)
	<-ratingDone
	if err := <-resetDone; err != nil {
		t.Fatalf("ResetUserProfile: %v", err)
	}

	repo.mu.Lock()
	_, hasProfile := repo.profiles["user-1"]
	_, hasHistory := repo.history["user-1"]
	repo.mu.Unlock()
	if hasProfile || hasHistory || m.GetUserProfile("user-1") != nil {
		t.Fatal("a completed pre-reset rating save resurrected the reset profile")
	}
}

func TestConcurrentCompletionKeepsProfileDirtyAfterOlderSave(t *testing.T) {
	repo := newOrderedSuggestionRepo()
	repo.saveEntered = make(chan struct{})
	repo.releaseSave = make(chan struct{})
	m := NewModule(nil, nil)
	m.repo = repo
	m.nextRevision = 1
	m.profiles["user-1"] = &UserProfile{
		UserID:          "user-1",
		CategoryScores:  map[string]float64{},
		TypePreferences: map[string]float64{},
		ViewHistory:     []ViewHistory{{MediaPath: "/media/a.mp4", LastViewed: time.Now()}},
		LastUpdated:     time.Now(),
		dirty:           true,
		revision:        1,
	}

	saveDone := make(chan error, 1)
	go func() { saveDone <- m.saveProfiles(context.Background()) }()
	<-repo.saveEntered
	m.RecordCompletion("user-1", "/media/a.mp4")
	close(repo.releaseSave)
	if err := <-saveDone; err != nil {
		t.Fatalf("saveProfiles: %v", err)
	}

	m.mu.RLock()
	p := m.profiles["user-1"]
	dirty, revision := p.dirty, p.revision
	m.mu.RUnlock()
	if !dirty || revision <= 1 {
		t.Fatalf("concurrent completion was lost: dirty=%v revision=%d", dirty, revision)
	}
}

func TestPurgeMediaPathTreatsMissingRowsAsIdempotent(t *testing.T) {
	repo := newOrderedSuggestionRepo()
	repo.deletePathErr = repositories.ErrViewHistoryNotFound
	m := NewModule(nil, nil)
	m.repo = repo
	m.RecordView("user-1", "/media/a.mp4", nil, "video", 1)

	if err := m.PurgeMediaPath("/media/a.mp4"); err != nil {
		t.Fatalf("PurgeMediaPath: %v", err)
	}
	if got := m.GetUserProfile("user-1"); got == nil || len(got.ViewHistory) != 0 {
		t.Fatalf("cache history was not purged: %#v", got)
	}
}

func TestRenameMediaPathFailureLeavesCacheAtOldPath(t *testing.T) {
	repo := newOrderedSuggestionRepo()
	repo.renameErr = errors.New("database unavailable")
	m := NewModule(nil, nil)
	m.repo = repo
	m.RecordView("user-1", "/media/old.mp4", nil, "video", 1)

	if err := m.RenameMediaPath("/media/old.mp4", "/media/new.mp4"); err == nil {
		t.Fatal("RenameMediaPath unexpectedly succeeded")
	}
	got := m.GetUserProfile("user-1")
	if got == nil || len(got.ViewHistory) != 1 || got.ViewHistory[0].MediaPath != "/media/old.mp4" {
		t.Fatalf("failed DB rename changed cache: %#v", got)
	}
}
