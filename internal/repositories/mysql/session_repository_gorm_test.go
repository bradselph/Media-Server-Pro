package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewSessionRepository_ReturnsInterface(t *testing.T) {
	repo := NewSessionRepository(nil)
	if repo == nil {
		t.Fatal("NewSessionRepository(nil) returned nil")
	}
	if _, ok := repo.(repositories.SessionRepository); !ok {
		t.Fatal("NewSessionRepository does not implement repositories.SessionRepository")
	}
}

func TestNewSessionRepository_InternalDBField(t *testing.T) {
	repo := NewSessionRepository(nil)
	sr, ok := repo.(*SessionRepository)
	if !ok {
		t.Fatal("could not cast to *SessionRepository")
	}
	if sr.db != nil {
		t.Error("expected db to be nil when constructed with nil")
	}
}
