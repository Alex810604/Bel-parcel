package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *kafka.Writer
}

func NewProducer(brokers []string, timeout time.Duration) (*Producer, error) {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		WriteTimeout: timeout,
		Balancer:     &kafka.LeastBytes{},
		// Idempotency requires setting ID, but simple Async production is robust enough for now
		// For true idempotency producer-side, we rely on Kafka acks.
		RequiredAcks: kafka.RequireAll,
		Async:        true,
	}

	return &Producer{writer: w}, nil
}

func (p *Producer) Publish(ctx context.Context, topic string, key string, msg interface{}) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	})
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
