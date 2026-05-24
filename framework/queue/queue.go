package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultQueueName = "video_jobs"
	defaultRabbitURL = "amqp://rabbitmq:rabbitmq@rabbit:5672/"
)

// Client wraps RabbitMQ connection details used by publishers/consumers.
type Client struct {
	URL       string
	QueueName string

	Connection *amqp.Connection
	Channel    *amqp.Channel
}

// NewClient creates a queue client using env defaults when values are not provided.
func NewClient() *Client {
	queueName := os.Getenv("RABBITMQ_QUEUE")
	if queueName == "" {
		queueName = defaultQueueName
	}

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = defaultRabbitURL
	}

	return &Client{
		URL:       rabbitURL,
		QueueName: queueName,
	}
}

// Connect opens the RabbitMQ connection and channel.
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

	c.Connection = conn
	c.Channel = ch

	return nil
}

// Declare creates the queue if it does not exist.
func (c *Client) Declare() (amqp.Queue, error) {
	if c.Channel == nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq channel is not initialized")
	}

	return c.Channel.QueueDeclare(
		c.QueueName,
		true,
		false,
		false,
		false,
		nil,
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
