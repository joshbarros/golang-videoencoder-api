package job

import (
	"fmt"
	"log"
	"os"

	"golang-videoencoder-api/domain"
	"golang-videoencoder-api/internal/upload"
	"golang-videoencoder-api/internal/video"
)

// defaultUploadConcurrency bounds how many output files upload in parallel per job.
const defaultUploadConcurrency = 4

// Processor runs the full media pipeline for a single job: mark processing,
// download the source, fragment, encode to DASH, upload the output, clean up,
// and record the terminal job status.
type Processor struct {
	JobService        *JobService
	SourceBucket      string
	OutputBucket      string
	UploadConcurrency int
}

// NewProcessorFromEnv builds a Processor from environment configuration.
func NewProcessorFromEnv(jobService *JobService) *Processor {
	return &Processor{
		JobService:        jobService,
		SourceBucket:      os.Getenv("VIDEO_SOURCE_BUCKET"),
		OutputBucket:      os.Getenv("VIDEO_OUTPUT_BUCKET"),
		UploadConcurrency: defaultUploadConcurrency,
	}
}

// Enabled reports whether the processor has everything it needs to run.
// When disabled (e.g. buckets unset), the worker falls back to creating
// pending jobs only, preserving the queue-only behavior.
func (p *Processor) Enabled() bool {
	return p != nil && p.JobService != nil && p.SourceBucket != "" && p.OutputBucket != ""
}

// Run executes the pipeline for a job and records the terminal status.
func (p *Processor) Run(job *domain.Job) error {
	if job == nil || job.Video == nil {
		return fmt.Errorf("processor requires a job with a loaded video")
	}

	if err := p.process(job); err != nil {
		if _, markErr := p.JobService.MarkFailed(job.ID, err); markErr != nil {
			return fmt.Errorf("processing failed: %v (also could not mark job failed: %v)", err, markErr)
		}
		return err
	}

	if _, err := p.JobService.MarkCompleted(job.ID); err != nil {
		return fmt.Errorf("processing succeeded but could not mark completed: %w", err)
	}

	return nil
}

// process performs the media work for a single job.
func (p *Processor) process(job *domain.Job) error {
	if _, err := p.JobService.MarkProcessing(job.ID); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	videoService, err := video.NewVideoService()
	if err != nil {
		return fmt.Errorf("video service init: %w", err)
	}
	defer func() {
		if cerr := videoService.Close(); cerr != nil {
			log.Printf("video service close warning: %v", cerr)
		}
	}()
	videoService.Video = job.Video

	if err := videoService.Download(p.SourceBucket); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := videoService.Fragment(); err != nil {
		return fmt.Errorf("fragment: %w", err)
	}
	if err := videoService.Encode(); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := p.uploadOutputs(job.Video.ID); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	// Cleanup is best-effort: the artifacts are already uploaded, so a local
	// cleanup failure must not flip a successful job to Failed.
	if err := videoService.Finish(); err != nil {
		log.Printf("cleanup warning for video %s: %v", job.Video.ID, err)
	}

	return nil
}

// uploadOutputs pushes the encoded output directory to the output bucket.
func (p *Processor) uploadOutputs(videoID string) error {
	videoUpload := upload.NewVideoUpload()
	videoUpload.OutputBucket = p.OutputBucket
	videoUpload.VideoPath = os.Getenv("localStoragePath") + "/" + videoID

	concurrency := p.UploadConcurrency
	if concurrency <= 0 {
		concurrency = defaultUploadConcurrency
	}

	// doneUpload is buffered so ProcessUpload's completion send never blocks;
	// that lets us call it synchronously and still read the outcome, and it
	// avoids the deadlock on ProcessUpload's early-return error paths.
	doneUpload := make(chan string, 1)
	if err := videoUpload.ProcessUpload(concurrency, doneUpload); err != nil {
		return err
	}

	if result := <-doneUpload; result != "upload complete" {
		return fmt.Errorf("upload reported failure: %s", result)
	}

	return nil
}
