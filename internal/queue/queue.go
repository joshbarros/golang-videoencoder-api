package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// dialTimeout bounds how long a (re)connect attempt may block, so readiness
// checks and publishes fail fast instead of hanging on an unreachable broker.
const dialTimeout = 5 * time.Second

// Client wraps RabbitMQ connection details used by publishers/consumers.
//
// It is safe for concurrent use: the connection/channel are guarded by a mutex
// so concurrent publishes can transparently reconnect after an outage without
// racing each other.
type Client struct {
	URL       string
	QueueName string
	// Prefetch bounds the number of unacknowledged deliveries the broker will
	// push to a consumer at once (channel QoS). 0 leaves it unbounded.
	Prefetch int

	mu         sync.Mutex
	Connection *amqp.Connection
	Channel    *amqp.Channel
}

// NewClient creates an empty queue client for manual configuration.
func NewClient() *Client {
	return &Client{}
}

// NewClientFromEnv creates a queue client from required environment variables.
func NewClientFromEnv() (*Client, error) {
	queueName := os.Getenv("RABBITMQ_QUEUE")
	if queueName == "" {
		return nil, fmt.Errorf("RABBITMQ_QUEUE is required")
	}

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}

	return &Client{
		URL:       rabbitURL,
		QueueName: queueName,
	}, nil
}

// connectLocked dials the broker and opens a channel. Caller must hold c.mu.
func (c *Client) connectLocked() error {
	conn, err := amqp.DialConfig(c.URL, amqp.Config{Dial: amqp.DefaultDial(dialTimeout)})
	if err != nil {
		return err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}

	if c.Prefetch > 0 {
		if err := ch.Qos(c.Prefetch, 0, false); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return err
		}
	}

	c.Connection = conn
	c.Channel = ch
	return nil
}

// closeLocked releases channel and connection. Caller must hold c.mu.
func (c *Client) closeLocked() error {
	var closeErr error
	if c.Channel != nil {
		if err := c.Channel.Close(); err != nil {
			closeErr = err
		}
		c.Channel = nil
	}
	if c.Connection != nil {
		if err := c.Connection.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		c.Connection = nil
	}
	return closeErr
}

// readyLocked reports whether the connection and channel are usable. Caller must hold c.mu.
func (c *Client) readyLocked() bool {
	return c.Connection != nil && !c.Connection.IsClosed() &&
		c.Channel != nil && !c.Channel.IsClosed()
}

// Connect opens the RabbitMQ connection and channel and applies QoS.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectLocked()
}

// Reconnect closes any existing connection and opens a fresh one.
func (c *Client) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.closeLocked()
	return c.connectLocked()
}

// IsReady reports whether the connection and channel are open and usable.
func (c *Client) IsReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readyLocked()
}

// EnsureReady reconnects if the connection is not currently usable, so callers
// (e.g. a readiness probe) recover from a transient broker outage without a
// process restart.
func (c *Client) EnsureReady() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readyLocked() {
		return nil
	}
	_ = c.closeLocked()
	return c.connectLocked()
}

// Close releases RabbitMQ channel and connection resources.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

// Declare creates the work queue along with a dead-letter exchange and queue.
// Messages the consumer rejects (Nack without requeue) are routed to
// "<queue>.dlq" instead of being discarded.
func (c *Client) Declare() (amqp.Queue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Channel == nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq channel is not initialized")
	}

	deadLetterExchange := c.QueueName + ".dlx"
	deadLetterQueue := c.QueueName + ".dlq"

	if err := c.Channel.ExchangeDeclare(deadLetterExchange, "fanout", true, false, false, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("declare dead-letter exchange: %w", err)
	}
	if _, err := c.Channel.QueueDeclare(deadLetterQueue, true, false, false, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("declare dead-letter queue: %w", err)
	}
	if err := c.Channel.QueueBind(deadLetterQueue, "", deadLetterExchange, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("bind dead-letter queue: %w", err)
	}

	return c.Channel.QueueDeclare(
		c.QueueName,
		true,
		false,
		false,
		false,
		amqp.Table{"x-dead-letter-exchange": deadLetterExchange},
	)
}

// Publish sends a raw payload to the configured queue, reconnecting and
// retrying once if the channel has been closed by a broker outage.
func (c *Client) Publish(ctx context.Context, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.readyLocked() {
		if err := c.reconnectLocked(); err != nil {
			return fmt.Errorf("publish: queue unavailable: %w", err)
		}
	}

	if err := c.publishOnce(ctx, body); err != nil {
		// The channel may have just died; reconnect once and retry.
		if rerr := c.reconnectLocked(); rerr != nil {
			return fmt.Errorf("publish failed: %v; reconnect failed: %w", err, rerr)
		}
		return c.publishOnce(ctx, body)
	}

	return nil
}

func (c *Client) reconnectLocked() error {
	_ = c.closeLocked()
	return c.connectLocked()
}

func (c *Client) publishOnce(ctx context.Context, body []byte) error {
	return c.Channel.PublishWithContext(
		ctx,
		"",
		c.QueueName,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
}

// PublishJSON marshals a payload and publishes it to the queue.
func (c *Client) PublishJSON(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return c.Publish(ctx, body)
}

// Consume creates a consumer stream for the configured queue.
func (c *Client) Consume(consumerName string, autoAck bool) (<-chan amqp.Delivery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Channel == nil {
		return nil, fmt.Errorf("rabbitmq channel is not initialized")
	}

	return c.Channel.Consume(
		c.QueueName,
		consumerName,
		autoAck,
		false,
		false,
		false,
		nil,
	)
}
