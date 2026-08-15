package queue

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/adeptry-app/go-common/config"
)

// sqsAPI is the SQS surface the publisher and consumer use, so tests can
// substitute a fake for the real client.
type sqsAPI interface {
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

// newSQSClient builds an SQS client from cfg. Credentials come from the default
// chain, so an ECS task role needs no code path of its own; Endpoint redirects
// to LocalStack when set.
func newSQSClient(ctx context.Context, cfg config.SQSConfig) (*sqs.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrClientFailed, err)
	}

	var opts []func(*sqs.Options)
	if cfg.Endpoint != "" {
		opts = append(opts, func(o *sqs.Options) { o.BaseEndpoint = aws.String(cfg.Endpoint) })
	}
	return sqs.NewFromConfig(awsCfg, opts...), nil
}

// verifyQueue checks that the queue exists and is reachable.
func verifyQueue(ctx context.Context, client sqsAPI, queueURL string) error {
	_, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrQueueUnavailable, queueURL, err)
	}
	return nil
}

// queueName is the last path segment of a queue URL, used as the metrics label.
func queueName(queueURL string) string {
	if i := strings.LastIndex(queueURL, "/"); i >= 0 {
		return queueURL[i+1:]
	}
	return queueURL
}

// stringAttribute builds a String message attribute.
func stringAttribute(value string) types.MessageAttributeValue {
	return types.MessageAttributeValue{
		DataType:    aws.String("String"),
		StringValue: aws.String(value),
	}
}

// seconds converts a duration to the whole seconds SQS takes. A positive
// sub-second value rounds up, since 0 means "visible now" on a visibility
// change and "unset" on a receive.
func seconds(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}
	s := int64((d + time.Second - 1) / time.Second)
	if s < 0 {
		return 0
	}
	if s > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(s)
}

// count clamps a message count to the int32 range the API uses.
func count(n int) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}
