package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewUserPreferencesRepository_ReturnsInterface(t *testing.T) {
	repo := NewUserPreferencesRepository(nil)
	if repo == nil {
		t.Fatal("NewUserPreferencesRepository(nil) returned nil")
	}
	if _, ok := repo.(repositories.UserPreferencesRepository); !ok {
		t.Fatal("NewUserPreferencesRepository does not implement repositories.UserPreferencesRepository")
	}
}

func TestNewUserPreferencesRepository_InternalDBField(t *testing.T) {
	repo := NewUserPreferencesRepository(nil)
	pr, ok := repo.(*UserPreferencesRepository)
	if !ok {
		t.Fatal("could not cast to *UserPreferencesRepository")
	}
	if pr.db != nil {
		t.Error("expected db to be nil when constructed with nil")
	}
}
