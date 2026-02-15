package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	outboxErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "outbox_errors_total",
			Help: "Count of outbox publish errors",
		},
		[]string{"topic", "status"},
	)
	retryAttempts = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "outbox_retry_attempts_bucket",
			Help:    "Histogram of outbox retry attempts",
			Buckets: []float64{3, 6, 9},
		},
	)
	dlqSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "dead_letter_queue_size",
			Help: "Number of events in DLQ",
		},
	)
	outboxQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "outbox_queue_size",
			Help: "Number of pending/error events in outbox",
		},
	)
)

func Init(mux *http.ServeMux) {
	prometheus.MustRegister(outboxErrors, retryAttempts, dlqSize, outboxQueueSize)
	mux.Handle("/metrics", promhttp.Handler())
}

func IncOutboxError(topic string) {
	outboxErrors.WithLabelValues(topic, "error").Inc()
}

func ObserveRetryAttempt(attempt int) {
	retryAttempts.Observe(float64(attempt))
}

func StartDLQGauge(ctx context.Context, db *pgxpool.Pool) {
	t := time.NewTicker(10 * time.Second)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var cnt int
				_ = db.QueryRow(context.Background(), "SELECT COUNT(*) FROM dead_letter_queue").Scan(&cnt)
				dlqSize.Set(float64(cnt))
			}
		}
	}()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var cnt int
				_ = db.QueryRow(context.Background(), `
					SELECT COUNT(*) FROM outbox_events 
					WHERE status IN ('pending','error')
				`).Scan(&cnt)
				outboxQueueSize.Set(float64(cnt))
			}
		}
	}()
}
