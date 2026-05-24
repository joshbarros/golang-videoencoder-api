//go:build integration

package video_test

import (
	"context"
	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/internal/database"
	videorepo "golang-videoencoder-api/internal/repositories/video"
	videosvc "golang-videoencoder-api/internal/video"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

// defaultTestBucket is used when explicit test bucket env vars are not set.
const defaultTestBucket = "joshbarrostest-20260523-1"

// init loads environment variables and configures GCP credentials for tests.
func init() {
	err := godotenv.Load("../../.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	credentialsPath, err := filepath.Abs("../../bucket-credentials.json")
	if err != nil {
		log.Fatal(err)
	}

	err = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	if err != nil {
		log.Fatal(err)
	}
}

// testSourceBucket returns the source bucket configured for integration tests.
func testSourceBucket() string {
	bucketName := os.Getenv("VIDEO_SOURCE_BUCKET")
	if bucketName == "" {
		return defaultTestBucket
	}

	return bucketName
}

// testOutputBucket returns the output bucket configured for integration tests.
func testOutputBucket() string {
	bucketName := os.Getenv("VIDEO_OUTPUT_BUCKET")
	if bucketName == "" {
		return testSourceBucket()
	}

	return bucketName
}

// prepare creates a test video and repository backed by the test database.
func prepare() (*domain.Video, videorepo.VideoRepositoryDb) {
	db := database.NewDbTest()
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer sqlDB.Close()

	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "invite.mp4"
	video.CreatedAt = time.Now()

	repository := videorepo.VideoRepositoryDb{Db: db}

	return video, repository
}

// TestVideoServiceDownload validates the full download, fragment, encode, and cleanup flow.
func TestVideoServiceDownload(t *testing.T) {
	v, repository := prepare()

	videoService, err := videosvc.NewVideoService()
	require.Nil(t, err)
	videoService.Video = v
	videoService.VideoRepository = repository

	ctx := context.Background()

	err = videoService.Download(ctx, testSourceBucket())
	require.Nil(t, err)

	err = videoService.Fragment(ctx)
	require.Nil(t, err)

	err = videoService.Encode(ctx)
	require.Nil(t, err)

	err = videoService.Finish()
	require.Nil(t, err)
}
