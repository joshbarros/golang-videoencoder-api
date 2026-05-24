package repositories_test

import (
	"golang-videoencoder-api/application/repositories"
	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/framework/database"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

// TestJobRepositoryDbInsert validates insert and lookup behavior for jobs.
func TestJobRepositoryDbInsert(t *testing.T) {
	db := database.NewDbTest()
	sqlDB, err := db.DB()
	require.Nil(t, err)
	defer sqlDB.Close()

	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "C://"
	video.CreatedAt = time.Now()

	repository := repositories.VideoRepositoryDb{Db: db}
	repository.Insert(video)

	job, err := domain.NewJob("output_path", "Pending", video)
	require.Nil(t, err)

	jobRepository := repositories.JobRepositoryDb{Db: db}
	jobRepository.Insert(job)

	j, err := jobRepository.Find(job.ID)
	require.NotEmpty(t, j.ID)
	require.Nil(t, err)
	require.Equal(t, j.ID, job.ID)
	require.Equal(t, j.VideoID, video.ID)
}

// TestJobRepositoryDbUpdate validates status updates are persisted.
func TestJobRepositoryDbUpdate(t *testing.T) {
	db := database.NewDbTest()
	sqlDB, err := db.DB()
	require.Nil(t, err)
	defer sqlDB.Close()

	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "C://"
	video.CreatedAt = time.Now()

	repository := repositories.VideoRepositoryDb{Db: db}
	repository.Insert(video)

	job, err := domain.NewJob("output_path", "Pending", video)
	require.Nil(t, err)

	jobRepository := repositories.JobRepositoryDb{Db: db}
	jobRepository.Insert(job)

	job.Status = "Complete"

	jobRepository.Update(job)

	j, err := jobRepository.Find(job.ID)
	require.NotEmpty(t, j.ID)
	require.Nil(t, err)
	require.Equal(t, j.Status, job.Status)
}
