package metrics

import "testing"

func TestMustRegister_NoPanic(t *testing.T) {
	MustRegister()
	RequestsTotal.WithLabelValues("GET", "/healthz", "200").Inc()
	RequestDuration.WithLabelValues("GET", "/readyz").Observe(0.01)
	DBQueryDuration.WithLabelValues("SELECT").Observe(0.01)
	EventsPublishedTotal.WithLabelValues("topic", "ok").Inc()
	EventsFailedTotal.WithLabelValues("topic").Inc()
	CacheHitsTotal.Inc()
	CacheMissesTotal.Inc()
	OutboxPending.Set(3)
	OutboxEventAge.Observe(1.0)
}
