package domain_test

import (
	"golang-videoencoder-api/domain"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateIfVideoIsEmpty(t *testing.T) {
	video := domain.NewVideo()
	err := video.Validate()

	require.Error(t, err)
}

func TestVideoIsISNotAnUuid(t *testing.T) {
	video := domain.NewVideo()

	video.ID = "123"
	video.ResourceID = "test"
	video.FilePath = "C://"
	video.CreatedAt = time.Now()

	err := video.Validate()
	require.Error(t, err)
}

func TestVideoValidation(t *testing.T) {
	video := domain.NewVideo()

	video.ID = uuid.NewV4().String()
	video.ResourceID = "test"
	video.FilePath = "C://"
	video.CreatedAt = time.Now()

	err := video.Validate()
	require.Nil(t, err)
}
