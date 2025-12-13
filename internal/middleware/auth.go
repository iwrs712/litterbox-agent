package middleware

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
)

const TokenEnvVar = "AGENT_TOKEN"

type AuthManager struct {
	mu sync.RWMutex
}

func NewAuthManager() *AuthManager {
	return &AuthManager{}
}

// Verify 验证 token
func (m *AuthManager) Verify(token string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	envToken := os.Getenv(TokenEnvVar)
	if envToken == "" {
		// 如果环境变量中没有token，则不需要验证，直接返回true
		return true
	}

	return envToken == token
}

// IsAuthEnabled 检查是否启用了认证
func (m *AuthManager) IsAuthEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return os.Getenv(TokenEnvVar) != ""
}

// ReloadToken 重新加载token（由于每次都从环境变量读取，这个方法主要用于显式通知）
func (m *AuthManager) ReloadToken() {
	// 这个方法存在是为了API的明确性
}

// Protect 保护需要认证的接口
func (m *AuthManager) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 如果未启用认证，直接放行
		if !m.IsAuthEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		// 验证 token
		clientToken := r.Header.Get("X-Token")
		if !m.Verify(clientToken) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid or missing token",
				"code":  "INVALID_TOKEN",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}
