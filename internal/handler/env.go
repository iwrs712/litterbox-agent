package handler

import (
	"encoding/json"
	"net/http"

	"litterbox-agent/internal/middleware"
	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type EnvHandler struct {
	envService  *service.EnvService
	authManager *middleware.AuthManager
}

func NewEnvHandler(envService *service.EnvService, authManager *middleware.AuthManager) *EnvHandler {
	return &EnvHandler{
		envService:  envService,
		authManager: authManager,
	}
}

func (h *EnvHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPost:
		h.handleSet(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// GET /api/env - List all environment variables
// GET /api/env?key=xxx - Get specific environment variable
func (h *EnvHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")

	if key != "" {
		// Get specific variable
		value := h.envService.GetEnv(key)
		utils.WriteSuccess(w, service.EnvGetResponse{
			Key:   key,
			Value: value,
		})
	} else {
		// List all variables
		vars := h.envService.ListEnv()
		utils.WriteSuccess(w, service.EnvListResponse{
			Variables: vars,
		})
	}
}

// POST /api/env - Set environment variable
func (h *EnvHandler) handleSet(w http.ResponseWriter, r *http.Request) {
	var req service.EnvSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Key == "" {
		utils.WriteError(w, http.StatusBadRequest, "Key is required")
		return
	}

	if err := h.envService.SetEnv(req.Key, req.Value); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to set environment variable")
		return
	}

	// If AGENT_TOKEN is updated, reload auth manager
	if req.Key == "AGENT_TOKEN" {
		h.authManager.ReloadToken()
	}

	response := map[string]interface{}{
		"message": "Environment variable set successfully",
		"key":     req.Key,
		"value":   req.Value,
	}

	// If DEFAULT_DIR is updated, signal that a refresh is needed
	if req.Key == "DEFAULT_DIR" {
		response["requires_refresh"] = true
	}

	utils.WriteSuccess(w, response)
}

// DELETE /api/env?key=xxx - Unset environment variable
func (h *EnvHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")

	if key == "" {
		utils.WriteError(w, http.StatusBadRequest, "Key is required")
		return
	}

	if err := h.envService.UnsetEnv(key); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to unset environment variable")
		return
	}

	// If AGENT_TOKEN is removed, reload auth manager
	if key == "AGENT_TOKEN" {
		h.authManager.ReloadToken()
	}

	utils.WriteSuccess(w, map[string]string{
		"message": "Environment variable unset successfully",
		"key":     key,
	})
}
