package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

type hlsJobRow struct {
	ID             string     `gorm:"column:id;primaryKey"`
	MediaPath      string     `gorm:"column:media_path"`
	OutputDir      string     `gorm:"column:output_dir"`
	Status         string     `gorm:"column:status"`
	Progress       float64    `gorm:"column:progress"`
	Qualities      string     `gorm:"column:qualities;type:json"`
	StartedAt      time.Time  `gorm:"column:started_at"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
	LastAccessedAt *time.Time `gorm:"column:last_accessed_at"`
	Error          *string    `gorm:"column:error_message"`
	FailCount      int        `gorm:"column:fail_count"`
	HLSUrl         *string    `gorm:"column:hls_url"`
	Available      bool       `gorm:"column:available"`
}

func (hlsJobRow) TableName() string { return "hls_jobs" }

type HLSJobRepository struct {
	db *gorm.DB
}

func NewHLSJobRepository(db *gorm.DB) repositories.HLSJobRepository {
	if db == nil {
		panic("NewHLSJobRepository: db is nil")
	}
	return &HLSJobRepository{db: db}
}

func (r *HLSJobRepository) Save(ctx context.Context, job *models.HLSJob) error {
	if job == nil {
		return fmt.Errorf("job must not be nil")
	}
	row, err := r.jobToRow(job)
	if err != nil {
		return fmt.Errorf("failed to serialize HLS job: %w", err)
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"media_path", "output_dir", "status", "progress", "qualities",
			"started_at", "completed_at", "last_accessed_at", "error_message",
			"fail_count", "hls_url", "available",
		}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("failed to save HLS job: %w", err)
	}
	return nil
}

func (r *HLSJobRepository) Get(ctx context.Context, id string) (*models.HLSJob, error) {
	var row hlsJobRow
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil //nolint:nilnil // callers check rec == nil explicitly
		}
		return nil, fmt.Errorf("failed to get HLS job: %w", err)
	}
	job, err := r.rowToJob(&row)
	if err != nil {
		return nil, fmt.Errorf("failed to convert HLS job row: %w", err)
	}
	return job, nil
}

func (r *HLSJobRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&hlsJobRow{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete HLS job: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("HLS job not found: %s", id)
	}
	return nil
}

func (r *HLSJobRepository) List(ctx context.Context) ([]*models.HLSJob, error) {
	var rows []hlsJobRow
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list HLS jobs: %w", err)
	}
	jobs := make([]*models.HLSJob, 0, len(rows))
	for i := range rows {
		job, err := r.rowToJob(&rows[i])
		if err != nil {
			log.Printf("[hls_job_repository] skipping corrupt HLS job row %s: %v", rows[i].ID, err)
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *HLSJobRepository) jobToRow(job *models.HLSJob) (hlsJobRow, error) {
	qualJSON, err := json.Marshal(job.Qualities)
	if err != nil {
		return hlsJobRow{}, fmt.Errorf("failed to marshal qualities: %w", err)
	}
	row := hlsJobRow{
		ID:        job.ID,
		MediaPath: job.MediaPath,
		OutputDir: job.OutputDir,
		Status:    string(job.Status),
		Progress:  job.Progress,
		Qualities: string(qualJSON),
		StartedAt: job.StartedAt,
		FailCount: job.FailCount,
		Available: job.Available,
	}
	if job.CompletedAt != nil {
		row.CompletedAt = job.CompletedAt
	}
	if job.LastAccessedAt != nil {
		row.LastAccessedAt = job.LastAccessedAt
	}
	if job.Error != "" {
		row.Error = &job.Error
	}
	if job.HLSUrl != "" {
		row.HLSUrl = &job.HLSUrl
	}
	return row, nil
}

func (r *HLSJobRepository) rowToJob(row *hlsJobRow) (*models.HLSJob, error) {
	job := &models.HLSJob{
		ID:             row.ID,
		MediaPath:      row.MediaPath,
		OutputDir:      row.OutputDir,
		Status:         models.HLSStatus(row.Status),
		Progress:       row.Progress,
		StartedAt:      row.StartedAt,
		CompletedAt:    row.CompletedAt,
		LastAccessedAt: row.LastAccessedAt,
		FailCount:      row.FailCount,
		Available:      row.Available,
	}
	if row.Error != nil {
		job.Error = *row.Error
	}
	if row.HLSUrl != nil {
		job.HLSUrl = *row.HLSUrl
	}
	if row.Qualities != "" {
		if err := json.Unmarshal([]byte(row.Qualities), &job.Qualities); err != nil {
			return nil, fmt.Errorf("failed to unmarshal qualities for job %s: %w", row.ID, err)
		}
	}
	if job.Qualities == nil {
		job.Qualities = []string{}
	}
	return job, nil
}
