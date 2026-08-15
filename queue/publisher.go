package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
)

// Common errors returned by the queue package.
var (
	ErrClientFailed    = errors.New("failed to create SQS client")
	ErrMarshalFailed   = errors.New("failed to marshal message")
	ErrPublishFailed   = errors.New("failed to publish message")
	ErrPublisherClosed = errors.New("publisher is closed")
)

// Message attributes carrying what SQS has no field for.
const (
	correlationAttribute = "correlationId"
	sourceIDAttribute    = "sourceMessageId"
)

// Publisher defines the interface for message queue publishing.
type Publisher interface {
	EventPublisher
	MaxRetries() int
	Close() error
}

// SQSPublisher implements Publisher for SQS.
// Publish is safe for concurrent use.
type SQSPublisher struct {
	mu       sync.Mutex
	closed   bool
	inflight sync.WaitGroup // sends in progress
	client   sqsAPI
	cfg      config.SQSConfig
	name     string
	metrics  MetricsRecorder
}

// NewSQSPublisher creates a publisher for the queue named by cfg.QueueURL.
//
// The queue itself is created by Terraform (deployed) or the LocalStack init
// script (local); nothing is declared here. ctx bounds credential resolution
// only, not the lifetime of the publisher.
func NewSQSPublisher(ctx context.Context, cfg config.SQSConfig, opts ...PublisherOption) (*SQSPublisher, error) {
	client, err := newSQSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return newPublisher(client, cfg, opts...), nil
}

// newPublisher wires an already-built client, so tests can pass a fake.
func newPublisher(client sqsAPI, cfg config.SQSConfig, opts ...PublisherOption) *SQSPublisher {
	p := &SQSPublisher{
		client:  client,
		cfg:     cfg.WithDefaults(),
		name:    queueName(cfg.QueueURL),
		metrics: noopMetrics{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Publish sends a message to the main queue.
// The correlation ID is taken from the context (logger.GetCorrelationID) when
// present so messages can be traced back to the originating request; otherwise
// a new one is generated. It travels as a message attribute.
func (p *SQSPublisher) Publish(ctx context.Context, message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMarshalFailed, err)
	}

	correlationID := logger.GetCorrelationID(ctx)
	if correlationID == "" {
		correlationID = uuid.NewString()
	}

	start := time.Now()
	err = p.send(ctx, body, correlationID)
	p.metrics.RecordPublish(p.name, err == nil, time.Since(start))
	return err
}

func (p *SQSPublisher) send(ctx context.Context, body []byte, correlationID string) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPublisherClosed
	}
	// Registered under the lock so Close cannot start waiting between the
	// closed check and the send.
	p.inflight.Add(1)
	p.mu.Unlock()
	defer p.inflight.Done()

	_, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.cfg.QueueURL),
		MessageBody: aws.String(string(body)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			correlationAttribute: stringAttribute(correlationID),
		},
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPublishFailed, err)
	}
	return nil
}

// MaxRetries returns the number of ladder steps, which is the retry budget
// handlers compare the claimed row's attempt against.
func (p *SQSPublisher) MaxRetries() int {
	return len(p.cfg.RetryDelays)
}

// Close rejects further publishes and waits for in-flight ones to finish, so a
// publish whose context has no deadline can hold it open.
// Idempotent: subsequent calls return nil.
func (p *SQSPublisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	p.inflight.Wait()
	return nil
}
