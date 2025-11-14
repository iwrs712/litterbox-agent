package handler

import (
	"net/http"

	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type MetricsHandler struct {
	metricsService *service.MetricsService
}

func NewMetricsHandler(metricsService *service.MetricsService) *MetricsHandler {
	return &MetricsHandler{
		metricsService: metricsService,
	}
}

func (h *MetricsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.metricsService.IncrementRequest()

	if r.Method != http.MethodGet {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	metrics := h.metricsService.GetMetrics()
	utils.WriteSuccess(w, metrics)
}
