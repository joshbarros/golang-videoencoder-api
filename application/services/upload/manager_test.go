package upload_test

import (
	videorepo "golang-videoencoder-api/application/repositories/video"
	upload "golang-videoencoder-api/application/services/upload"
	video "golang-videoencoder-api/application/services/video"
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

const defaultTestBucket = "joshbarrostest-20260523-1"

// init loads test environment variables needed for upload tests.
func init() {
	err := godotenv.Load("../../../.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	credentialsPath, err := filepath.Abs("../../../bucket-credentials.json")
	if err != nil {
		log.Fatal(err)
	}

	err = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	if err != nil {
		log.Fatal(err)
	}
}

func testSourceBucket() string {
	bucketName := os.Getenv("VIDEO_SOURCE_BUCKET")
	if bucketName == "" {
		return defaultTestBucket
	}

	return bucketName
}

func testOutputBucket() string {
	bucketName := os.Getenv("VIDEO_OUTPUT_BUCKET")
	if bucketName == "" {
		return testSourceBucket()
	}

	return bucketName
}

func prepare() (*domain.Video, videorepo.VideoRepositoryDb) {
	db := database.NewDbTest()
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer sqlDB.Close()

	v := domain.NewVideo()
	v.ID = uuid.NewV4().String()
	v.FilePath = "invite.mp4"
	v.CreatedAt = time.Now()

	repository := videorepo.VideoRepositoryDb{Db: db}

	return v, repository
}

// TestVideoServiceUpload validates the full pipeline including upload to GCS.
func TestVideoServiceUpload(t *testing.T) {
	v, repository := prepare()

	videoService, err := video.NewVideoService()
	require.Nil(t, err)
	videoService.Video = v
	videoService.VideoRepository = repository

	err = videoService.Download(testSourceBucket())
	require.Nil(t, err)

	err = videoService.Fragment()
	require.Nil(t, err)

	err = videoService.Encode()
	require.Nil(t, err)

	videoUpload := upload.NewVideoUpload()
	videoUpload.OutputBucket = testOutputBucket()
	videoUpload.VideoPath = os.Getenv("localStoragePath") + "/" + v.ID

	doneUpload := make(chan string)
	go videoUpload.ProcessUpload(50, doneUpload)

	result := <-doneUpload
	require.Equal(t, result, "upload complete")
}
