package services_test

import (
	"golang-videoencoder-api/application/repositories"
	"golang-videoencoder-api/application/services"
	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/framework/database"
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
func prepare() (*domain.Video, repositories.VideoRepositoryDb) {
	db := database.NewDbTest()
	defer db.Close()

	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "invite.mp4"
	video.CreatedAt = time.Now()

	repository := repositories.VideoRepositoryDb{Db: db}

	return video, repository
}

// TestVideoServiceDownload validates the full download, fragment, encode, and cleanup flow.
func TestVideoServiceDownload(t *testing.T) {
	video, repository := prepare()

	videoService, err := services.NewVideoService()
	require.Nil(t, err)
	videoService.Video = video
	videoService.VideoRepository = repository

	err = videoService.Download(testSourceBucket())
	require.Nil(t, err)

	err = videoService.Fragment()
	require.Nil(t, err)

	err = videoService.Encode()
	require.Nil(t, err)

	err = videoService.Finish()
	require.Nil(t, err)
}
