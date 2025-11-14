package handler

import (
	"encoding/json"
	"net/http"

	"litterbox-agent/internal/middleware"
)

type InitHandler struct {
	authManager *middleware.AuthManager
}

func NewInitHandler(authManager *middleware.AuthManager) *InitHandler {
	return &InitHandler{
		authManager: authManager,
	}
}

func (h *InitHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, err := h.authManager.Initialize()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
			"code":  "ALREADY_INITIALIZED",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":   token,
		"message": "Token initialized successfully. Save this token, it cannot be retrieved again.",
	})
}
