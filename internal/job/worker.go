package job

import (
	"encoding/json"
	"fmt"
	"golang-videoencoder-api/domain"
)

// JobWorkerMessage is the transport-agnostic payload consumed by a worker.
type JobWorkerMessage struct {
	VideoID          string `json:"video_id"`
	OutputBucketPath string `json:"output_bucket_path"`
}

// JobWorkerResult is the outcome returned by a worker for each payload.
type JobWorkerResult struct {
	Job     *domain.Job
	Payload []byte
	Error   string
}

// JobWorker consumes raw payloads, validates input, creates a pending job, and
// (when the processor is enabled) runs the full media pipeline to completion.
func JobWorker(messageChannel <-chan []byte, returnChannel chan<- JobWorkerResult, jobService *JobService, processor *Processor) {
	for payload := range messageChannel {
		result := JobWorkerResult{Payload: payload}

		message, err := parseJobWorkerMessage(payload)
		if err != nil {
			result.Error = err.Error()
			returnChannel <- result
			continue
		}

		job, err := jobService.CreatePendingJob(message.VideoID, message.OutputBucketPath)
		if err != nil {
			result.Error = err.Error()
			returnChannel <- result
			continue
		}

		result.Job = job

		if processor.Enabled() {
			if perr := processor.Run(job); perr != nil {
				result.Error = perr.Error()
			} else {
				job.Status = JobStatusCompleted
			}
		}

		returnChannel <- result
	}
}

// parseJobWorkerMessage decodes and validates the required worker fields.
func parseJobWorkerMessage(payload []byte) (*JobWorkerMessage, error) {
	var message JobWorkerMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return nil, err
	}

	if message.VideoID == "" {
		return nil, fmt.Errorf("video_id is required")
	}

	if message.OutputBucketPath == "" {
		return nil, fmt.Errorf("output_bucket_path is required")
	}

	return &message, nil
}
