package service

import (
	"log"
	"os"
	"sync"
)

type EnvService struct {
	mu sync.RWMutex
}

func NewEnvService() *EnvService {
	return &EnvService{}
}

type EnvSetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type EnvGetResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type EnvListResponse struct {
	Variables map[string]string `json:"variables"`
}

// SetEnv sets an environment variable
func (s *EnvService) SetEnv(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("[Env] Setting %s = '%s'", key, value)
	err := os.Setenv(key, value)
	if err != nil {
		log.Printf("[Env] Error setting %s: %v", key, err)
	}
	return err
}

// GetEnv gets an environment variable
func (s *EnvService) GetEnv(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return os.Getenv(key)
}

// ListEnv lists all environment variables (filtered to common ones)
func (s *EnvService) ListEnv() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return commonly used environment variables
	vars := map[string]string{
		"PORT":        os.Getenv("PORT"),
		"AGENT_TOKEN": os.Getenv("AGENT_TOKEN"),
		"DEFAULT_DIR": os.Getenv("DEFAULT_DIR"),
	}

	return vars
}

// UnsetEnv unsets an environment variable
func (s *EnvService) UnsetEnv(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Unsetenv(key)
}
