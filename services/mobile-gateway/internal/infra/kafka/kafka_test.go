package kafka

import (
	"context"
	"testing"
	"time"
)

func TestProducer_PublishMarshalError(t *testing.T) {
	p, err := NewProducer([]string{"localhost:9092"}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewProducer error: %v", err)
	}
	defer p.Close()
	ch := make(chan int)
	err = p.Publish(context.Background(), "topic", "key", ch)
	if err == nil {
		t.Fatalf("expected marshal error, got nil")
	}
}
