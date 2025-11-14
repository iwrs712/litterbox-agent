package middleware

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

type AuthManager struct {
	token       string
	initialized bool
	mu          sync.RWMutex
}

func NewAuthManager() *AuthManager {
	return &AuthManager{
		token:       "",
		initialized: false,
	}
}

// Initialize 只能成功调用一次
func (m *AuthManager) Initialize() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return "", ErrAlreadyInitialized
	}

	// 生成 token
	m.token = "tok-" + uuid.New().String()
	m.initialized = true

	return m.token, nil
}

// Verify 验证 token
func (m *AuthManager) Verify(token string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.initialized {
		return false
	}

	return m.token == token
}

// IsInitialized 检查是否已初始化
func (m *AuthManager) IsInitialized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialized
}

var ErrAlreadyInitialized = &InitError{"Token already initialized"}

type InitError struct {
	Message string
}

func (e *InitError) Error() string {
	return e.Message
}

// Protect 保护需要认证的接口
func (m *AuthManager) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否已初始化
		if !m.IsInitialized() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Token not initialized. Please call /init first.",
				"code":  "NOT_INITIALIZED",
			})
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
