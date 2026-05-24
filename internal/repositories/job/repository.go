package jobrepo

import (
	"errors"
	"fmt"
	"golang-videoencoder-api/domain"

	"gorm.io/gorm"
)

// ErrJobNotFound is returned by Find when no job matches the given id.
var ErrJobNotFound = errors.New("job does not exist")

// JobRepository defines persistence operations for jobs.
type JobRepository interface {
	Insert(job *domain.Job) (*domain.Job, error)
	Find(id string) (*domain.Job, error)
	Update(job *domain.Job) (*domain.Job, error)
}

// JobRepositoryDb implements JobRepository using GORM.
type JobRepositoryDb struct {
	Db *gorm.DB
}

// Insert persists a new job record.
func (repository JobRepositoryDb) Insert(job *domain.Job) (*domain.Job, error) {
	err := repository.Db.Create(job).Error

	if err != nil {
		return nil, err
	}

	return job, nil
}

// Find loads a job by id and preloads its related video.
func (repository JobRepositoryDb) Find(id string) (*domain.Job, error) {
	var job domain.Job
	err := repository.Db.Preload("Video").First(&job, "id = ?", id).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find job: %w", err)
	}

	return &job, nil
}

// Update saves the current job fields to the database.
func (repository JobRepositoryDb) Update(job *domain.Job) (*domain.Job, error) {
	err := repository.Db.Save(job).Error

	if err != nil {
		return nil, err
	}

	return job, nil
}
