package videorepo_test

import (
	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/internal/database"
	videorepo "golang-videoencoder-api/internal/repositories/video"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

// TestVideoRepositoryDbInsert validates insert and lookup behavior for videos.
func TestVideoRepositoryDbInsert(t *testing.T) {
	db := database.NewDbTest()
	sqlDB, err := db.DB()
	require.Nil(t, err)
	defer sqlDB.Close()

	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "path"
	video.CreatedAt = time.Now()

	repository := videorepo.VideoRepositoryDb{Db: db}
	repository.Insert(video)

	v, err := repository.Find(video.ID)

	require.NotEmpty(t, v.ID)
	require.Nil(t, err)
	require.Equal(t, v.ID, video.ID)
}
