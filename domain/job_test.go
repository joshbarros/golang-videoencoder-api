package domain_test

import (
	"golang-videoencoder-api/domain"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
)

// TestNewJob verifies a job is created and validated successfully.
func TestNewJob(t *testing.T) {
	video := domain.NewVideo()
	video.ID = uuid.NewV4().String()
	video.FilePath = "C://"
	video.CreatedAt = time.Now()

	job, err := domain.NewJob("C://", "Converted", video)
	require.NotNil(t, job)
	require.Nil(t, err)
}
