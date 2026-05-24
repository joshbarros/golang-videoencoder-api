package job

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync"

	"golang-videoencoder-api/internal/queue"
)

const defaultWorkerConcurrency = 1

// Manager orchestrates worker goroutines and optionally bridges queue payloads.
type Manager struct {
	JobService *JobService
	Workers    int
	Processor  *Processor

	MessageChannel chan []byte
	ResultChannel  chan JobWorkerResult
}

// NewManager builds a manager with channels, worker count, and a media
// processor configured from env.
func NewManager(jobService *JobService) *Manager {
	workers := workerConcurrencyFromEnv()

	return &Manager{
		JobService:     jobService,
		Workers:        workers,
		Processor:      NewProcessorFromEnv(jobService),
		MessageChannel: make(chan []byte),
		ResultChannel:  make(chan JobWorkerResult),
	}
}

// Start launches worker goroutines and a result logger loop.
// It returns a stop function that closes channels in safe order.
func (m *Manager) Start() (stop func()) {
	if m.JobService == nil {
		log.Println("job manager started without JobService; workers will fail")
	}

	var wg sync.WaitGroup
	for i := 0; i < m.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			JobWorker(m.MessageChannel, m.ResultChannel, m.JobService, m.Processor)
		}()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for result := range m.ResultChannel {
			if result.Error != "" {
				log.Printf("job worker failed: %s payload=%s", result.Error, string(result.Payload))
				continue
			}
			if result.Job != nil {
				log.Printf("job processed: id=%s video_id=%s status=%s", result.Job.ID, result.Job.VideoID, result.Job.Status)
			}
		}
	}()

	return func() {
		close(m.MessageChannel)
		wg.Wait()
		close(m.ResultChannel)
		<-done
	}
}

// Enqueue pushes a payload into the local worker input channel.
func (m *Manager) Enqueue(payload []byte) {
	m.MessageChannel <- payload
}

// RunQueueConsumer consumes messages from queue.Client and forwards payloads to workers.
// Message acks are handled only after the payload is handed to the manager.
func (m *Manager) RunQueueConsumer(ctx context.Context, q *queue.Client, consumerName string) error {
	deliveries, err := q.Consume(consumerName, false)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-deliveries:
			if !ok {
				return nil
			}
			m.Enqueue(msg.Body)
			if err := msg.Ack(false); err != nil {
				log.Printf("ack error: %v", err)
			}
		}
	}
}

func workerConcurrencyFromEnv() int {
	value := os.Getenv("CONCURRENCY_WORKERS")
	if value == "" {
		return defaultWorkerConcurrency
	}

	workers, err := strconv.Atoi(value)
	if err != nil || workers <= 0 {
		return defaultWorkerConcurrency
	}

	return workers
}
