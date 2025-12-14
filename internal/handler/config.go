package handler

import (
	"net/http"

	"litterbox-agent/internal/service"
	"litterbox-agent/internal/utils"
)

type ConfigHandler struct {
	configService *service.ConfigService
}

func NewConfigHandler(configService *service.ConfigService) *ConfigHandler {
	return &ConfigHandler{
		configService: configService,
	}
}

func (h *ConfigHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	config := h.configService.GetConfig()
	utils.WriteSuccess(w, config)
}
