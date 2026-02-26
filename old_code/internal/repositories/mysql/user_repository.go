// Package mysql provides MySQL implementations of repository interfaces using GORM.
package mysql

import (
	"database/sql"

	"media-server-pro/internal/repositories"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// NewUserRepository creates a new MySQL-backed user repository from *sql.DB
// This is a compatibility wrapper that converts *sql.DB to GORM
func NewUserRepository(sqlDB *sql.DB) (repositories.UserRepository, error) {
	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn: sqlDB,
	}), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return NewUserRepositoryGORM(gormDB), nil
}
