package mysql

import "testing"

func TestNewFavoritesRepository_NilDBPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewFavoritesRepository(nil) should panic")
		}
	}()
	NewFavoritesRepository(nil)
}

func TestFavoriteRow_TableName(t *testing.T) {
	row := favoriteRow{}
	if row.TableName() != "user_favorites" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "user_favorites")
	}
}
