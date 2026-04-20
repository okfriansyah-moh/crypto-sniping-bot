package check

import (
	"encoding/json"
	"net/http"
)

// Handler handles HTTP requests for health checks.
type Handler struct {
	service *Service
}

// NewHandler creates a new health check handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Handle processes the health check request.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	result := h.service.Execute()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
