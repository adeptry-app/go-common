package queue

import (
	"errors"
	"fmt"
	"time"

	"github.com/adeptry-app/go-common/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

// dial opens an AMQP connection with the configured heartbeat and a client
// connection name (visible in the RabbitMQ management UI). The config must
// already be normalized via WithDefaults.
func dial(cfg config.RabbitMQConfig) (*amqp.Connection, error) {
	props := amqp.NewConnectionProperties()
	if cfg.ConsumerTag != "" {
		props.SetClientConnectionName(cfg.ConsumerTag)
	}

	conn, err := amqp.DialConfig(cfg.URL(), amqp.Config{
		Heartbeat:  cfg.Heartbeat,
		Locale:     "en_US",
		Properties: props,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}
	return conn, nil
}

// closeResources closes a channel and connection (either may be nil) and
// returns the joined errors, if any.
func closeResources(ch *amqp.Channel, conn *amqp.Connection) error {
	var errs []error
	if ch != nil {
		if err := ch.Close(); err != nil {
			errs = append(errs, fmt.Errorf("channel: %v", err))
		}
	}
	if conn != nil {
		if err := conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("connection: %v", err))
		}
	}
	return errors.Join(errs...)
}

// declareExchangeAndQueue declares an exchange, queue, and binds them together
func declareExchangeAndQueue(ch *amqp.Channel, exchange, queue string, queueArgs amqp.Table) error {
	if err := ch.ExchangeDeclare(exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("%w: declare exchange %s: %v", ErrQueueSetupFailed, exchange, err)
	}

	if _, err := ch.QueueDeclare(queue, true, false, false, false, queueArgs); err != nil {
		return fmt.Errorf("%w: declare queue %s: %v", ErrQueueSetupFailed, queue, err)
	}

	if err := ch.QueueBind(queue, queue, exchange, false, nil); err != nil {
		return fmt.Errorf("%w: bind queue %s to exchange %s: %v", ErrQueueSetupFailed, queue, exchange, err)
	}

	return nil
}

// declareTopology declares the full queue infrastructure (DLX/DLQ, retry
// queues, main queue) and returns the retry queue names in order. All
// declarations are idempotent as long as the configuration is unchanged.
// Note: RabbitMQ rejects re-declaration with different arguments
// (PRECONDITION_FAILED), so changing RetryDelays for an existing topology
// requires deleting the retry queues first.
func declareTopology(ch *amqp.Channel, cfg config.RabbitMQConfig) ([]string, error) {
	dlxName := cfg.Exchange + "_dlx"
	dlqName := cfg.Queue + "_dlq"

	// Declare dead letter exchange and queue (permanent failures)
	if err := declareExchangeAndQueue(ch, dlxName, dlqName, nil); err != nil {
		return nil, err
	}

	// Declare retry queues with TTL (messages expire and route back to main queue)
	retryQueues := make([]string, len(cfg.RetryDelays))
	for i, delay := range cfg.RetryDelays {
		retryQueueName := fmt.Sprintf("%s_retry_%d", cfg.Queue, i)
		retryQueues[i] = retryQueueName

		retryArgs := amqp.Table{
			"x-message-ttl":             delay.Milliseconds(),
			"x-dead-letter-exchange":    cfg.Exchange,
			"x-dead-letter-routing-key": cfg.Queue,
		}
		if err := declareExchangeAndQueue(ch, cfg.Exchange, retryQueueName, retryArgs); err != nil {
			return nil, err
		}
	}

	// Declare main queue (failures route to DLQ if not explicitly retried)
	mainQueueArgs := amqp.Table{
		"x-dead-letter-exchange":    dlxName,
		"x-dead-letter-routing-key": dlqName,
	}
	if err := declareExchangeAndQueue(ch, cfg.Exchange, cfg.Queue, mainQueueArgs); err != nil {
		return nil, err
	}

	return retryQueues, nil
}

// reconnectDelay returns the backoff delay before reconnect attempt n
// (1-based): exponential growth from ReconnectInitialDelay capped at
// ReconnectMaxDelay, with up to 25% random jitter added to avoid
// synchronized reconnect storms. The config must already be normalized via
// WithDefaults.
func reconnectDelay(cfg config.RabbitMQConfig, attempt int) time.Duration {
	delay := cfg.ReconnectInitialDelay
	for i := 1; i < attempt && delay < cfg.ReconnectMaxDelay; i++ {
		delay *= 2
	}
	if delay > cfg.ReconnectMaxDelay {
		delay = cfg.ReconnectMaxDelay
	}

	jitter := time.Duration(randFloat64() * 0.25 * float64(delay))
	return delay + jitter
}

// closeError formats the *amqp.Error received from NotifyClose, which is nil
// on graceful shutdown.
func closeError(err *amqp.Error) string {
	if err == nil {
		return "graceful close"
	}
	return err.Error()
}
