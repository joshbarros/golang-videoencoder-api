package job

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"golang-videoencoder-api/internal/queue"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultWorkerConcurrency = 1
	minReconnectBackoff      = 1 * time.Second
	maxReconnectBackoff      = 30 * time.Second
)

// Acknowledger confirms or rejects a consumed message after processing.
type Acknowledger interface {
	Ack() error
	Nack(requeue bool) error
}

// Message is a unit of work for a worker: a raw payload plus an optional
// acknowledger so the source message is confirmed (or dead-lettered) only
// after processing finishes. Ack is nil for in-process/test dispatch.
type Message struct {
	Body []byte
	Ack  Acknowledger
}

// Manager orchestrates worker goroutines and bridges queue deliveries.
type Manager struct {
	JobService *JobService
	Workers    int
	Processor  *Processor

	MessageChannel chan Message
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
		MessageChannel: make(chan Message, workers),
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

// Enqueue pushes a raw payload into the local worker input channel without an
// acknowledger (used for in-process dispatch and tests).
func (m *Manager) Enqueue(payload []byte) {
	m.MessageChannel <- Message{Body: payload}
}

// RunQueueConsumer streams deliveries to workers, reconnecting with backoff so
// a dropped connection does not silently stop the service. It returns only
// when ctx is cancelled.
func (m *Manager) RunQueueConsumer(ctx context.Context, q *queue.Client, consumerName string) error {
	backoff := minReconnectBackoff

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := m.consumeOnce(ctx, q, consumerName)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			err = fmt.Errorf("delivery channel closed")
		}
		log.Printf("queue consumer interrupted: %v; reconnecting in %s", err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		if rerr := m.reconnect(q); rerr != nil {
			log.Printf("queue reconnect failed: %v", rerr)
			backoff = nextBackoff(backoff)
			continue
		}

		log.Println("queue consumer reconnected")
		backoff = minReconnectBackoff
	}
}

// consumeOnce forwards deliveries to workers until the delivery channel closes
// or ctx is cancelled. Each source message is acked/nacked by the worker that
// handles it, not here.
func (m *Manager) consumeOnce(ctx context.Context, q *queue.Client, consumerName string) error {
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
			select {
			case m.MessageChannel <- Message{Body: msg.Body, Ack: deliveryAck{delivery: msg}}:
			case <-ctx.Done():
				// Shutting down before handoff: return the message to the queue.
				_ = msg.Nack(false, true)
				return ctx.Err()
			}
		}
	}
}

// reconnect re-establishes the connection and re-declares the topology.
func (m *Manager) reconnect(q *queue.Client) error {
	if err := q.Reconnect(); err != nil {
		return err
	}
	if _, err := q.Declare(); err != nil {
		return err
	}
	return nil
}

// deliveryAck adapts an amqp delivery to the Acknowledger interface.
type deliveryAck struct {
	delivery amqp.Delivery
}

func (a deliveryAck) Ack() error              { return a.delivery.Ack(false) }
func (a deliveryAck) Nack(requeue bool) error { return a.delivery.Nack(false, requeue) }

// nextBackoff doubles the backoff up to the configured ceiling.
func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxReconnectBackoff {
		return maxReconnectBackoff
	}
	return next
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
