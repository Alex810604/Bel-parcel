package kafka

import "testing"

func TestNewConsumerCreatesReaders(t *testing.T) {
	c := NewConsumer([]string{"127.0.0.1:0"}, "g", []string{"t1", "t2"}, 0)
	if c == nil {
		t.Fatalf("nil consumer")
	}
}
