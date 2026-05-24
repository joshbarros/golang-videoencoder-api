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

// TestVideoRepositoryDbInsert validates insert and lookup behavior for videos.
func TestVideoRepositoryDbInsert(t *testing.T) {
	db := database.NewDbTest()
	defer db.Close()

	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "path"
	video.CreatedAt = time.Now()

	repository := repositories.VideoRepositoryDb{Db: db}
	repository.Insert(video)

	v, err := repository.Find(video.ID)

	require.NotEmpty(t, v.ID)
	require.Nil(t, err)
	require.Equal(t, v.ID, video.ID)
}
