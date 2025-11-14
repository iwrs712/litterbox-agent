package handler

import (
	"net/http"
	"path/filepath"

	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type DownloadHandler struct {
	fileService    *service.FileService
	metricsService *service.MetricsService
}

func NewDownloadHandler(fileService *service.FileService, metricsService *service.MetricsService) *DownloadHandler {
	return &DownloadHandler{
		fileService:    fileService,
		metricsService: metricsService,
	}
}

func (h *DownloadHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.metricsService.IncrementRequest()

	if r.Method != http.MethodGet {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		utils.WriteError(w, http.StatusBadRequest, "File path required")
		return
	}

	file, stat, err := h.fileService.DownloadFile(filePath)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	defer file.Close()

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(filePath))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)

	h.metricsService.IncrementDownload()
}
