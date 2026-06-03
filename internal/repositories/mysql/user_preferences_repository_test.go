package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewUserPreferencesRepository_ReturnsInterface(t *testing.T) {
	// The explicit interface type makes "constructor returns the interface" a
	// compile-time guarantee (a runtime type assertion would be tautological).
	var repo repositories.UserPreferencesRepository = NewUserPreferencesRepository(nil)
	if repo == nil {
		t.Fatal("NewUserPreferencesRepository(nil) returned nil")
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
