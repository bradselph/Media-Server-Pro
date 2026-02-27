// Package mysql - session_repository.go implements session persistence in MySQL using GORM.
package mysql

import (
	"database/sql"

	"media-server-pro/internal/repositories"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// NewSessionRepository creates a new MySQL-backed session repository from *sql.DB
// This is a compatibility wrapper that converts *sql.DB to GORM
func NewSessionRepository(sqlDB *sql.DB) (repositories.SessionRepository, error) {
	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn: sqlDB,
	}), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return NewSessionRepositoryGORM(gormDB), nil
}
