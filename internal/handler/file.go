package handler

import (
	"encoding/json"
	"net/http"

	"litterbox-agent/internal/model"
	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type FileHandler struct {
	fileService    *service.FileService
	metricsService *service.MetricsService
}

func NewFileHandler(fileService *service.FileService, metricsService *service.MetricsService) *FileHandler {
	return &FileHandler{
		fileService:    fileService,
		metricsService: metricsService,
	}
}

// HandleOperation handles unified file operations
func (h *FileHandler) HandleOperation(w http.ResponseWriter, r *http.Request) {
	h.metricsService.IncrementRequest()

	if r.Method != http.MethodPost {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req model.FileOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Command == "" {
		utils.WriteError(w, http.StatusBadRequest, "Command required")
		return
	}

	if req.Path == "" {
		utils.WriteError(w, http.StatusBadRequest, "Path required")
		return
	}

	switch req.Command {
	case "create":
		if req.FileText == "" {
			utils.WriteError(w, http.StatusBadRequest, "file_text required for create command")
			return
		}
	case "str_replace":
		if req.OldStr == "" {
			utils.WriteError(w, http.StatusBadRequest, "old_str required for str_replace command")
			return
		}
	case "insert":
		if req.NewStr == "" {
			utils.WriteError(w, http.StatusBadRequest, "new_str required for insert command")
			return
		}
	}

	response, err := h.fileService.FileOperation(&req)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	utils.WriteSuccess(w, response)
}
