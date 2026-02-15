package kafka

import (
	"context"
	"testing"
	"time"
)

func TestPublishMarshalError(t *testing.T) {
	p := NewProducer([]string{"127.0.0.1:0"}, 100*time.Millisecond)
	defer p.Close()
	ch := make(chan int)
	if err := p.Publish(context.Background(), "t", "k", ch); err == nil {
		t.Fatalf("expected marshal error")
	}
}
