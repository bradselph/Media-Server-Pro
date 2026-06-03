package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewUserPermissionsRepository_ReturnsInterface(t *testing.T) {
	// The explicit interface type makes "constructor returns the interface" a
	// compile-time guarantee (a runtime type assertion would be tautological).
	var repo repositories.UserPermissionsRepository = NewUserPermissionsRepository(nil)
	if repo == nil {
		t.Fatal("NewUserPermissionsRepository(nil) returned nil")
	}
}

func TestNewUserPermissionsRepository_InternalDBField(t *testing.T) {
	repo := NewUserPermissionsRepository(nil)
	pr, ok := repo.(*UserPermissionsRepository)
	if !ok {
		t.Fatal("could not cast to *UserPermissionsRepository")
	}
	if pr.db != nil {
		t.Error("expected db to be nil when constructed with nil")
	}
}
