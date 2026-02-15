package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reference_requests_total",
			Help: "Total HTTP requests in reference-service",
		},
		[]string{"method", "endpoint", "code"},
	)
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "reference_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)
	DBQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "reference_db_query_duration_seconds",
			Help:    "DB query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"query"},
	)
	EventsPublishedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reference_events_published_total",
			Help: "Total published events",
		},
		[]string{"topic", "status"},
	)
	EventsFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reference_events_failed_total",
			Help: "Total failed event publishes",
		},
		[]string{"topic"},
	)
	CacheHitsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "reference_cache_hits_total",
			Help: "Cache hits total",
		},
	)
	CacheMissesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "reference_cache_misses_total",
			Help: "Cache misses total",
		},
	)
	OutboxPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "reference_outbox_pending",
			Help: "Pending outbox events",
		},
	)
	OutboxEventAge = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "reference_outbox_event_age_seconds",
			Help:    "Age of outbox events at publish time",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func MustRegister() {
	prometheus.MustRegister(
		RequestsTotal,
		RequestDuration,
		DBQueryDuration,
		EventsPublishedTotal,
		EventsFailedTotal,
		CacheHitsTotal,
		CacheMissesTotal,
		OutboxPending,
		OutboxEventAge,
	)
}
