package domain_test

import (
	"golang-videoencoder-api/domain"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

// TestValidateIfVideoIsEmpty ensures validation fails when required fields are missing.
func TestValidateIfVideoIsEmpty(t *testing.T) {
	video := domain.NewVideo()
	err := video.Validate()

	require.Error(t, err)
}

// TestVideoIsISNotAnUuid ensures validation fails for an invalid UUID.
func TestVideoIsISNotAnUuid(t *testing.T) {
	video := domain.NewVideo()

	video.ID = "123"
	video.ResourceID = "test"
	video.FilePath = "path"
	video.CreatedAt = time.Now()

	err := video.Validate()
	require.Error(t, err)
}

// TestVideoValidation ensures validation passes for a fully valid video.
func TestVideoValidation(t *testing.T) {
	video := domain.NewVideo()

	video.ID = uuid.NewV4().String()
	video.ResourceID = "test"
	video.FilePath = "path"
	video.CreatedAt = time.Now()

	err := video.Validate()
	require.Nil(t, err)
}
