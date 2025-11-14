package handler

import (
	"net/http"

	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type UploadHandler struct {
	fileService    *service.FileService
	metricsService *service.MetricsService
}

func NewUploadHandler(fileService *service.FileService, metricsService *service.MetricsService) *UploadHandler {
	return &UploadHandler{
		fileService:    fileService,
		metricsService: metricsService,
	}
}

func (h *UploadHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.metricsService.IncrementRequest()

	if r.Method != http.MethodPost {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer file.Close()

	uploadDir := r.FormValue("path")
	dst, err := h.fileService.UploadFile(file, header, uploadDir)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.metricsService.IncrementUpload()
	utils.WriteSuccess(w, map[string]string{
		"status": "success",
		"path":   dst,
	})
}
