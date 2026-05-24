package job

import (
	"fmt"
	jobrepo "golang-videoencoder-api/application/repositories/job"
	videorepo "golang-videoencoder-api/application/repositories/video"
	"golang-videoencoder-api/domain"
)

const (
	// JobStatusPending represents a queued job waiting to be processed.
	JobStatusPending = "Pending"
	// JobStatusProcessing represents a job currently being processed.
	JobStatusProcessing = "Processing"
	// JobStatusCompleted represents a job that finished successfully.
	JobStatusCompleted = "Completed"
	// JobStatusFailed represents a job that failed during processing.
	JobStatusFailed = "Failed"
)

// JobService coordinates job lifecycle operations and persistence.
type JobService struct {
	JobRepository   jobrepo.JobRepository
	VideoRepository videorepo.VideoRepository
}

// NewJobService creates a JobService with the required repositories.
func NewJobService(jobRepository jobrepo.JobRepository, videoRepository videorepo.VideoRepository) *JobService {
	return &JobService{
		JobRepository:   jobRepository,
		VideoRepository: videoRepository,
	}
}

// CreatePendingJob loads a video and creates a new pending job for it.
func (s *JobService) CreatePendingJob(videoID string, outputBucketPath string) (*domain.Job, error) {
	if s.JobRepository == nil || s.VideoRepository == nil {
		return nil, fmt.Errorf("job service repositories are not configured")
	}

	video, err := s.VideoRepository.Find(videoID)
	if err != nil {
		return nil, err
	}

	job, err := domain.NewJob(outputBucketPath, JobStatusPending, video)
	if err != nil {
		return nil, err
	}

	return s.JobRepository.Insert(job)
}

// Find returns a persisted job by id.
func (s *JobService) Find(jobID string) (*domain.Job, error) {
	if s.JobRepository == nil {
		return nil, fmt.Errorf("job repository is not configured")
	}

	return s.JobRepository.Find(jobID)
}

// MarkProcessing updates the job status to processing.
func (s *JobService) MarkProcessing(jobID string) (*domain.Job, error) {
	return s.updateStatus(jobID, JobStatusProcessing, "")
}

// MarkCompleted updates the job status to completed and clears any previous error.
func (s *JobService) MarkCompleted(jobID string) (*domain.Job, error) {
	return s.updateStatus(jobID, JobStatusCompleted, "")
}

// MarkFailed updates the job status to failed and stores an error message.
func (s *JobService) MarkFailed(jobID string, processErr error) (*domain.Job, error) {
	errMessage := ""
	if processErr != nil {
		errMessage = processErr.Error()
	}

	return s.updateStatus(jobID, JobStatusFailed, errMessage)
}

// updateStatus loads a job, mutates state, and persists the update.
func (s *JobService) updateStatus(jobID string, status string, errMessage string) (*domain.Job, error) {
	if s.JobRepository == nil {
		return nil, fmt.Errorf("job repository is not configured")
	}

	job, err := s.JobRepository.Find(jobID)
	if err != nil {
		return nil, err
	}

	job.Status = status
	job.Error = errMessage

	return s.JobRepository.Update(job)
}
