package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"media-server-pro/internal/repositories"
)

type apiTokenRow struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	Name       string     `gorm:"column:name"`
	TokenHash  string     `gorm:"column:token_hash"`
	LastUsedAt *time.Time `gorm:"column:last_used_at"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (apiTokenRow) TableName() string { return "user_api_tokens" }

// APITokenRepositoryImpl implements repositories.APITokenRepository using GORM.
type APITokenRepositoryImpl struct {
	db *gorm.DB
}

// NewAPITokenRepository creates a new GORM-based API token repository.
func NewAPITokenRepository(db *gorm.DB) repositories.APITokenRepository {
	return &APITokenRepositoryImpl{db: db}
}

func (r *APITokenRepositoryImpl) Create(ctx context.Context, token *repositories.APITokenRecord) error {
	row := apiTokenRow{
		ID:        token.ID,
		UserID:    token.UserID,
		Name:      token.Name,
		TokenHash: token.TokenHash,
		ExpiresAt: token.ExpiresAt,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("create api token: %w", err)
	}
	return nil
}

func (r *APITokenRepositoryImpl) GetByHash(ctx context.Context, tokenHash string) (*repositories.APITokenRecord, error) {
	var row apiTokenRow
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("get api token by hash: %w", err)
	}
	return rowToAPITokenRecord(&row), nil
}

func (r *APITokenRepositoryImpl) ListByUser(ctx context.Context, userID string) ([]*repositories.APITokenRecord, error) {
	var rows []apiTokenRow
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	out := make([]*repositories.APITokenRecord, len(rows))
	for i, row := range rows {
		out[i] = rowToAPITokenRecord(&row)
	}
	return out, nil
}

func (r *APITokenRepositoryImpl) Delete(ctx context.Context, id, userID string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&apiTokenRow{})
	if result.Error != nil {
		return fmt.Errorf("delete api token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repositories.ErrAPITokenNotFound
	}
	return nil
}

func (r *APITokenRepositoryImpl) UpdateLastUsed(ctx context.Context, tokenHash string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&apiTokenRow{}).
		Where("token_hash = ?", tokenHash).
		Update("last_used_at", now)
	if result.Error != nil {
		return fmt.Errorf("update api token last_used_at: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repositories.ErrAPITokenNotFound
	}
	return nil
}

func rowToAPITokenRecord(row *apiTokenRow) *repositories.APITokenRecord {
	return &repositories.APITokenRecord{
		ID:         row.ID,
		UserID:     row.UserID,
		Name:       row.Name,
		TokenHash:  row.TokenHash,
		LastUsedAt: row.LastUsedAt,
		ExpiresAt:  row.ExpiresAt,
		CreatedAt:  row.CreatedAt,
	}
}
