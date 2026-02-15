package metrics

import "testing"

func TestRegister_NoPanic(t *testing.T) {
	Register()
	HTTPRequestsTotal.WithLabelValues("/healthz", "200").Inc()
	OutboxPendingCount.Set(5)
	KafkaPublishErrorsTotal.Inc()
}
