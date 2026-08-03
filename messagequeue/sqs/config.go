package sqs

// Config configures a SQS-backed consumer.
type Config struct {
	QueueAddress string `env:"QUEUE_ADDRESS" json:"queueAddress,omitempty" yaml:"queueAddress,omitempty"`
}
