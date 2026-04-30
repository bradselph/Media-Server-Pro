package mysql

import "testing"

func TestNewIPListRepository_NilDBPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewIPListRepository(nil) should panic")
		}
	}()
	NewIPListRepository(nil)
}

func TestIPListConfigRow_TableName(t *testing.T) {
	row := ipListConfigRow{}
	if row.TableName() != "ip_list_config" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "ip_list_config")
	}
}

func TestIPListEntryRow_TableName(t *testing.T) {
	row := ipListEntryRow{}
	if row.TableName() != "ip_list_entries" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "ip_list_entries")
	}
}
