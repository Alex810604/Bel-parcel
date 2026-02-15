package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bel-parcel/services/reference-service/internal/auth"
	"bel-parcel/services/reference-service/internal/metrics"
)

type Handlers struct {
	svc       *Service
	validator *auth.Validator
}

func NewHandlers(svc *Service, v *auth.Validator) *Handlers {
	return &Handlers{svc: svc, validator: v}
}

func (h *Handlers) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{type}", metricsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		auth.RequireRoles(h.validator, []string{"user", "moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
			typ := r.PathValue("type")
			validTypes := map[string]bool{
				"pvz":        true,
				"carriers":   true,
				"warehouses": true,
			}
			if !validTypes[typ] {
				writeJSONError(w, http.StatusBadRequest, "invalid type: must be one of pvz, carriers, warehouses")
				return
			}
			q := r.URL.Query().Get("q")
			if strings.TrimSpace(q) == "" {
				writeJSON(w, http.StatusOK, []any{})
				return
			}
			page := 1
			if p := r.URL.Query().Get("page"); p != "" {
				if pi, err := strconv.Atoi(p); err == nil && pi > 0 {
					page = pi
				}
			}
			limit := 50
			offset := (page - 1) * limit
			res, err := h.svc.Search(r.Context(), typ, q, limit, offset)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, res)
		})(w, r)
	}))

	mux.HandleFunc("PUT /pvp/{id}", metricsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		auth.RequireRoles(h.validator, []string{"moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				IsHub  bool   `json:"is_hub"`
				Reason string `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if strings.TrimSpace(body.Reason) == "" {
				writeJSONError(w, http.StatusBadRequest, "reason is required")
				return
			}
			u := auth.FromContext(r)
			audit := AuditInfo{
				OperatorID: u.ID,
				Reason:     body.Reason,
				Timestamp:  time.Now(),
			}
			if err := h.svc.UpdatePVZHubFlag(r.Context(), r.PathValue("id"), body.IsHub, audit); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		})(w, r)
	}))

	mux.HandleFunc("PUT /carriers/{id}", metricsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		auth.RequireRoles(h.validator, []string{"admin"}, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				IsActive bool   `json:"is_active"`
				Reason   string `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if strings.TrimSpace(body.Reason) == "" {
				writeJSONError(w, http.StatusBadRequest, "reason is required")
				return
			}
			u := auth.FromContext(r)
			audit := AuditInfo{
				OperatorID: u.ID,
				Reason:     body.Reason,
				Timestamp:  time.Now(),
			}
			if err := h.svc.UpdateCarrierActive(r.Context(), r.PathValue("id"), body.IsActive, audit); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		})(w, r)
	}))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func metricsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rr := &responseRecorder{ResponseWriter: w, status: 200}
		next(rr, r)
		dur := time.Since(start).Seconds()
		endpoint := r.URL.Path
		metrics.RequestDuration.WithLabelValues(r.Method, endpoint).Observe(dur)
		metrics.RequestsTotal.WithLabelValues(r.Method, endpoint, strconv.Itoa(rr.status)).Inc()
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.status = code
	rr.ResponseWriter.WriteHeader(code)
}
