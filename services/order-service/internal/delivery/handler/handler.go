package handler

import (
	"bel-parcel/services/order-service/internal/app"
	"encoding/json"
	"log/slog"
	"net/http"
)

type Handler struct {
	service *app.OrderService
}

func NewHandler(service *app.OrderService) http.Handler {
	mux := http.NewServeMux()
	h := &Handler{service: service}
	mux.HandleFunc("POST /orders", h.CreateOrder)
	mux.HandleFunc("GET /healthz", h.HealthCheck)
	mux.HandleFunc("GET /readyz", h.ReadinessCheck)
	return mux
}

type CreateOrderRequest struct {
	SellerID       string  `json:"seller_id"`
	PVZID          string  `json:"pvz_id"`
	CustomerPhone  string  `json:"customer_phone"`
	CustomerEmail  string  `json:"customer_email"`
	WarehouseLat   float64 `json:"warehouse_lat"`
	WarehouseLng   float64 `json:"warehouse_lng"`
	DestinationLat float64 `json:"destination_lat"`
	DestinationLng float64 `json:"destination_lng"`
}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (h *Handler) ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.Ping(ctx); err != nil {
		slog.ErrorContext(ctx, "readiness check failed", "error", err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SellerID == "" || req.PVZID == "" {
		http.Error(w, "seller_id and pvz_id are required", http.StatusBadRequest)
		return
	}

	order, err := h.service.CreateOrder(ctx, app.CreateOrderParams{
		SellerID:       req.SellerID,
		PVZID:          req.PVZID,
		CustomerPhone:  req.CustomerPhone,
		CustomerEmail:  req.CustomerEmail,
		WarehouseLat:   req.WarehouseLat,
		WarehouseLng:   req.WarehouseLng,
		DestinationLat: req.DestinationLat,
		DestinationLng: req.DestinationLng,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create order", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}
