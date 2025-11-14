package handler

import (
	"encoding/json"
	"net/http"

	"litterbox-agent/internal/model"
	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type ExecHandler struct {
	execService    *service.ExecService
	metricsService *service.MetricsService
}

func NewExecHandler(execService *service.ExecService, metricsService *service.MetricsService) *ExecHandler {
	return &ExecHandler{
		execService:    execService,
		metricsService: metricsService,
	}
}

func (h *ExecHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.metricsService.IncrementRequest()

	if r.Method != http.MethodPost {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req model.CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Command == "" {
		utils.WriteError(w, http.StatusBadRequest, "Command required")
		return
	}

	response := h.execService.ExecuteCommand(req.Command)
	h.metricsService.IncrementCommand()

	utils.WriteSuccess(w, response)
}
