package mysql

import "testing"

func TestNewAnalyticsRepository_NilDBPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewAnalyticsRepository(nil) should panic")
		}
	}()
	NewAnalyticsRepository(nil)
}
