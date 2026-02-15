package kafka

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	readers []*kafka.Reader
}

func NewConsumer(brokers []string, groupID string, topics []string, timeout time.Duration) *Consumer {
	var readers []*kafka.Reader
	for _, t := range topics {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:        brokers,
			GroupID:        groupID,
			Topic:          t,
			MaxWait:        timeout,
			CommitInterval: time.Second,
		})
		readers = append(readers, r)
	}
	return &Consumer{readers: readers}
}

func (c *Consumer) Start(ctx context.Context, handler func(topic string, key, value []byte) error) {
	for _, r := range c.readers {
		go func(reader *kafka.Reader) {
			for {
				m, err := reader.FetchMessage(ctx)
				if err != nil {
					return
				}
				if handler != nil {
					if err := handler(reader.Config().Topic, m.Key, m.Value); err != nil {
						continue
					}
				}
				_ = reader.CommitMessages(ctx, m)
			}
		}(r)
	}
}

func (c *Consumer) Close() error {
	var err error
	for _, r := range c.readers {
		e := r.Close()
		if e != nil {
			err = e
		}
	}
	return err
}
