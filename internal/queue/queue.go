package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Client wraps RabbitMQ connection details used by publishers/consumers.
type Client struct {
	URL       string
	QueueName string
	// Prefetch bounds the number of unacknowledged deliveries the broker will
	// push to a consumer at once (channel QoS). 0 leaves it unbounded.
	Prefetch int

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

// Connect opens the RabbitMQ connection and channel and applies QoS.
func (c *Client) Connect() error {
	conn, err := amqp.Dial(c.URL)
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

// Reconnect closes any existing connection and opens a fresh one.
func (c *Client) Reconnect() error {
	_ = c.Close()
	return c.Connect()
}

// IsReady reports whether the connection and channel are open and usable.
func (c *Client) IsReady() bool {
	return c.Connection != nil && !c.Connection.IsClosed() &&
		c.Channel != nil && !c.Channel.IsClosed()
}

// Declare creates the work queue along with a dead-letter exchange and queue.
// Messages the consumer rejects (Nack without requeue) are routed to
// "<queue>.dlq" instead of being discarded.
func (c *Client) Declare() (amqp.Queue, error) {
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

// Publish sends a raw payload to the configured queue.
func (c *Client) Publish(ctx context.Context, body []byte) error {
	if c.Channel == nil {
		return fmt.Errorf("rabbitmq channel is not initialized")
	}

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

// Close releases RabbitMQ channel and connection resources.
func (c *Client) Close() error {
	var closeErr error

	if c.Channel != nil {
		if err := c.Channel.Close(); err != nil {
			closeErr = err
		}
	}

	if c.Connection != nil {
		if err := c.Connection.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	return closeErr
}
