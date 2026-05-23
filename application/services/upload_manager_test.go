package services_test

import (
	"golang-videoencoder-api/application/services"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

func init() {
	err := godotenv.Load("../../.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func TestVideoServiceUpload(t *testing.T) {
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

	videoUpload := services.NewVideoUpload()
	videoUpload.OutputBucket = testOutputBucket()
	videoUpload.VideoPath = os.Getenv("localStoragePath") + "/" + video.ID

	doneUpload := make(chan string)
	go videoUpload.ProcessUpload(50, doneUpload)

	result := <-doneUpload
	require.Equal(t, result, "upload complete")
}
