package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewSessionRepository_ReturnsInterface(t *testing.T) {
	// The explicit interface type makes "constructor returns the interface" a
	// compile-time guarantee (a runtime type assertion would be tautological).
	var repo repositories.SessionRepository = NewSessionRepository(nil)
	if repo == nil {
		t.Fatal("NewSessionRepository(nil) returned nil")
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
