package videorepo

import (
	"fmt"
	"golang-videoencoder-api/domain"

	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

// VideoRepository defines persistence operations for videos.
type VideoRepository interface {
	Insert(video *domain.Video) (*domain.Video, error)
	Find(id string) (*domain.Video, error)
}

// VideoRepositoryDb implements VideoRepository using GORM.
type VideoRepositoryDb struct {
	Db *gorm.DB
}

// NewVideoRepository creates a repository bound to the provided database connection.
func NewVideoRepository(db *gorm.DB) *VideoRepositoryDb {
	return &VideoRepositoryDb{Db: db}
}

// Insert persists a video, generating an id when one is not already set.
func (repository VideoRepositoryDb) Insert(video *domain.Video) (*domain.Video, error) {
	if video.ID == "" {
		video.ID = uuid.NewV4().String()
	}

	err := repository.Db.Create(video).Error

	if err != nil {
		return nil, err
	}

	return video, nil
}

// Find loads a video by id and preloads associated jobs.
func (repository VideoRepositoryDb) Find(id string) (*domain.Video, error) {
	var video domain.Video
	repository.Db.Preload("Jobs").First(&video, "id = ?", id)

	if video.ID == "" {
		return nil, fmt.Errorf("video does not exist")
	}

	return &video, nil
}
