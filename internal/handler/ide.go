package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"

	"litterbox-agent/internal/model"
	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

// IDEHandler handles IDE-related requests
type IDEHandler struct {
	ideService *service.IDEService
}

// NewIDEHandler creates a new IDEHandler
func NewIDEHandler(ideService *service.IDEService) *IDEHandler {
	return &IDEHandler{
		ideService: ideService,
	}
}

// HandleFileTree handles file tree requests
func (h *IDEHandler) HandleFileTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get root path from query parameter, default to current working directory
	rootPath := r.URL.Query().Get("path")
	if rootPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "Failed to get current directory: "+err.Error())
			return
		}
		rootPath = cwd
	}

	tree, err := h.ideService.GetFileTree(rootPath)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get file tree: "+err.Error())
		return
	}

	utils.WriteJSON(w, http.StatusOK, tree)
}

// HandleFiles unified file operations handler
func (h *IDEHandler) HandleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleReadFile(w, r)
	case http.MethodPost:
		h.handleCreateFile(w, r)
	case http.MethodPut:
		h.handleSaveFile(w, r)
	case http.MethodDelete:
		h.handleDeleteFile(w, r)
	default:
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleReadFile handles file content read requests (GET)
func (h *IDEHandler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		utils.WriteError(w, http.StatusBadRequest, "Path is required")
		return
	}

	// Get file info for metadata
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get file info: "+err.Error())
		return
	}

	// Check if it's an image file
	isImage := h.ideService.IsImageFile(filePath)

	var content string
	if isImage {
		// Read image file and encode as base64
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "Failed to read image file: "+err.Error())
			return
		}
		content = base64.StdEncoding.EncodeToString(fileContent)
	} else {
		// Check if it's a binary file (no extension, < 100KB, contains null bytes)
		isBinary, err := h.ideService.IsBinaryFile(filePath)
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "Failed to check file type: "+err.Error())
			return
		}

		if isBinary {
			utils.WriteError(w, http.StatusBadRequest, "File is binary and too large to edit as text")
			return
		}

		// Read text file normally
		content, err = h.ideService.ReadFileContent(filePath)
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "Failed to read file: "+err.Error())
			return
		}
	}

	language := h.ideService.DetectLanguage(filePath)

	response := model.FileContentResponse{
		Content:  content,
		Path:     filePath,
		Language: language,
		Size:     fileInfo.Size(),
		ModTime:  fileInfo.ModTime().Format("2006-01-02 15:04:05"),
	}

	utils.WriteJSON(w, http.StatusOK, response)
}

// handleSaveFile handles file save requests (PUT)
func (h *IDEHandler) handleSaveFile(w http.ResponseWriter, r *http.Request) {

	var req model.FileSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Path == "" {
		utils.WriteError(w, http.StatusBadRequest, "Path is required")
		return
	}

	if err := h.ideService.SaveFileContent(req.Path, req.Content); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to save file: "+err.Error())
		return
	}

	response := model.FileSaveResponse{
		Success: true,
		Message: "File saved successfully",
	}

	utils.WriteJSON(w, http.StatusOK, response)
}

// handleCreateFile handles file/directory creation requests (POST)
func (h *IDEHandler) handleCreateFile(w http.ResponseWriter, r *http.Request) {

	var req model.FileCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Path == "" {
		utils.WriteError(w, http.StatusBadRequest, "Path is required")
		return
	}

	if err := h.ideService.CreateFile(req.Path, req.IsDir); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create: "+err.Error())
		return
	}

	response := model.FileOperationResult{
		Success: true,
		Message: "Created successfully",
	}

	utils.WriteJSON(w, http.StatusOK, response)
}

// handleDeleteFile handles file/directory deletion requests (DELETE)
func (h *IDEHandler) handleDeleteFile(w http.ResponseWriter, r *http.Request) {

	var req model.FileDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Path == "" {
		utils.WriteError(w, http.StatusBadRequest, "Path is required")
		return
	}

	if err := h.ideService.DeleteFile(req.Path); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to delete: "+err.Error())
		return
	}

	response := model.FileOperationResult{
		Success: true,
		Message: "Deleted successfully",
	}

	utils.WriteJSON(w, http.StatusOK, response)
}
