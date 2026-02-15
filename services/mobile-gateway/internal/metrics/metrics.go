package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests by path and status",
		},
		[]string{"path", "status"},
	)
	OutboxPendingCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "outbox_pending_count",
			Help: "Number of pending outbox events",
		},
	)
	KafkaPublishErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kafka_publish_errors_total",
			Help: "Total Kafka publish errors",
		},
	)
)

func Register() {
	prometheus.MustRegister(HTTPRequestsTotal)
	prometheus.MustRegister(OutboxPendingCount)
	prometheus.MustRegister(KafkaPublishErrorsTotal)
}
