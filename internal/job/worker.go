package job

import (
	"encoding/json"
	"fmt"
	"log"

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

// JobWorker consumes messages, processes each one, then acknowledges the source
// message on success or dead-letters it on failure.
func JobWorker(messageChannel <-chan Message, returnChannel chan<- JobWorkerResult, jobService *JobService, processor *Processor) {
	for msg := range messageChannel {
		result := processPayload(msg.Body, jobService, processor)
		acknowledge(msg, result)
		returnChannel <- result
	}
}

// processPayload validates the payload, creates a pending job, and (when the
// processor is enabled) runs the full media pipeline to completion.
func processPayload(payload []byte, jobService *JobService, processor *Processor) JobWorkerResult {
	result := JobWorkerResult{Payload: payload}

	message, err := parseJobWorkerMessage(payload)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	job, err := jobService.CreatePendingJob(message.VideoID, message.OutputBucketPath)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Job = job

	if processor.Enabled() {
		if perr := processor.Run(job); perr != nil {
			result.Error = perr.Error()
		} else {
			job.Status = JobStatusCompleted
		}
	}

	return result
}

// acknowledge confirms a successfully processed message, or rejects a failed one
// without requeue so it is routed to the configured dead-letter queue rather
// than being silently dropped or reprocessed forever.
func acknowledge(msg Message, result JobWorkerResult) {
	if msg.Ack == nil {
		return
	}

	if result.Error != "" {
		if err := msg.Ack.Nack(false); err != nil {
			log.Printf("nack error: %v", err)
		}
		return
	}

	if err := msg.Ack.Ack(); err != nil {
		log.Printf("ack error: %v", err)
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
