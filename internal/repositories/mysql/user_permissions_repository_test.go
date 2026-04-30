package mysql

import (
	"testing"

	"media-server-pro/internal/repositories"
)

func TestNewUserPermissionsRepository_ReturnsInterface(t *testing.T) {
	repo := NewUserPermissionsRepository(nil)
	if repo == nil {
		t.Fatal("NewUserPermissionsRepository(nil) returned nil")
	}
	if _, ok := repo.(repositories.UserPermissionsRepository); !ok {
		t.Fatal("NewUserPermissionsRepository does not implement repositories.UserPermissionsRepository")
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
