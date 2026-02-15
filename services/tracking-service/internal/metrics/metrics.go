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
	ActiveAlerts = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "tracking_active_alerts",
			Help: "Number of active alerts",
		},
	)
	RouteDeviationsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "tracking_route_deviations_total",
			Help: "Total route deviation alerts",
		},
	)
	LateDriversTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "tracking_late_drivers_total",
			Help: "Total late driver alerts",
		},
	)
	ManualAssignmentsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "tracking_manual_assignments_total",
			Help: "Total manual assignment alerts",
		},
	)
	WebsocketConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "tracking_websocket_connections",
			Help: "Current websocket connections",
		},
	)
	GPSUpdatesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "tracking_gps_updates_total",
			Help: "Total GPS updates processed",
		},
	)
	OutboxQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "tracking_outbox_queue_size",
			Help: "Outbox pending/error size",
		},
	)
	DLQSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "tracking_dlq_size",
			Help: "Dead letter queue size",
		},
	)
)

func Init(mux *http.ServeMux) {
	prometheus.MustRegister(
		ActiveAlerts,
		RouteDeviationsTotal,
		LateDriversTotal,
		ManualAssignmentsTotal,
		WebsocketConnections,
		GPSUpdatesTotal,
		OutboxQueueSize,
		DLQSize,
	)
	mux.Handle("/metrics", promhttp.Handler())
}

func StartGauges(ctx context.Context, db *pgxpool.Pool) {
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var cnt int
				_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM active_alerts WHERE resolved_at IS NULL`).Scan(&cnt)
				ActiveAlerts.Set(float64(cnt))
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
				_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM websocket_sessions`).Scan(&cnt)
				WebsocketConnections.Set(float64(cnt))
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
				_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM outbox_events WHERE status IN ('pending','error')`).Scan(&cnt)
				OutboxQueueSize.Set(float64(cnt))
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
				_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM dead_letter_queue`).Scan(&cnt)
				DLQSize.Set(float64(cnt))
			}
		}
	}()
}
