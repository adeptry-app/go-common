package queue

import (
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Delivery is the transport-neutral message a MessageHandler receives. SQS
// types never leave this package.
type Delivery struct {
	// Body is the raw message payload.
	Body []byte

	// MessageID is the queue's own id, carried into the DLQ copy on quarantine.
	MessageID string

	// CorrelationID links the message back to the originating request.
	CorrelationID string

	// ReceiveCount is the SQS ApproximateReceiveCount: every receive, including
	// ones a deploy or a Spot interruption cut short. It never decides whether
	// a message retries; it only indexes the ladder for a failure that happened
	// before any business attempt was charged.
	ReceiveCount int

	// ReceivedAt is when this delivery was received, the instant the SQS
	// visibility ceiling is measured from.
	ReceivedAt time.Time
}

// deliveryFrom builds the handler's view of an SQS message.
func deliveryFrom(m types.Message, receivedAt time.Time) Delivery {
	d := Delivery{
		MessageID:    aws.ToString(m.MessageId),
		ReceiveCount: 1,
		ReceivedAt:   receivedAt,
	}
	if m.Body != nil {
		d.Body = []byte(*m.Body)
	}
	if raw, ok := m.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]; ok {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			d.ReceiveCount = n
		}
	}
	if attr, ok := m.MessageAttributes[correlationAttribute]; ok {
		d.CorrelationID = aws.ToString(attr.StringValue)
	}
	return d
}
