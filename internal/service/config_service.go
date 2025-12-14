package service

import (
	"log"
	"os"
	"path/filepath"
)

type ConfigService struct{}

func NewConfigService() *ConfigService {
	return &ConfigService{}
}

type ConfigResponse struct {
	DefaultDirectory string `json:"default_directory"`
}

// GetConfig returns configuration information
func (s *ConfigService) GetConfig() *ConfigResponse {
	// Get default directory from environment variable
	defaultDir := os.Getenv("DEFAULT_DIR")
	log.Printf("[Config] DEFAULT_DIR env var: '%s'", defaultDir)

	// If not set, use current working directory
	if defaultDir == "" {
		cwd, err := os.Getwd()
		if err == nil {
			defaultDir = cwd
		}
	}

	// Convert to absolute path and normalize slashes
	if defaultDir != "" {
		absPath, err := filepath.Abs(defaultDir)
		if err == nil {
			defaultDir = filepath.ToSlash(absPath)
		}
	}

	log.Printf("[Config] Returning default directory: '%s'", defaultDir)
	return &ConfigResponse{
		DefaultDirectory: defaultDir,
	}
}
