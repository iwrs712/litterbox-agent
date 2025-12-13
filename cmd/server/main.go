package main

import (
	"log"
	"net/http"
	"os"

	"litterbox-agent/internal/handler"
	"litterbox-agent/internal/middleware"
	"litterbox-agent/internal/service"
)

func main() {
	// Initialize authentication manager
	authManager := middleware.NewAuthManager()

	// Initialize services
	fileService := service.NewFileService()
	execService := service.NewExecService()
	metricsService := service.NewMetricsService()
	ideService := service.NewIDEService()
	configService := service.NewConfigService()
	envService := service.NewEnvService()

	// Initialize handlers
	uploadHandler := handler.NewUploadHandler(fileService, metricsService)
	downloadHandler := handler.NewDownloadHandler(fileService, metricsService)
	execHandler := handler.NewExecHandler(execService, metricsService)
	metricsHandler := handler.NewMetricsHandler(metricsService)
	fileHandler := handler.NewFileHandler(fileService, metricsService)
	ideHandler := handler.NewIDEHandler(ideService)
	configHandler := handler.NewConfigHandler(configService)
	envHandler := handler.NewEnvHandler(envService, authManager)
	terminalWSHandler := handler.NewTerminalWSHandler()

	// Register routes
	// WebSocket endpoint for interactive terminal
	http.Handle("/ws/terminal", authManager.Protect(http.HandlerFunc(terminalWSHandler.Handle)))
	// System endpoints (no authentication required)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	http.Handle("/api/config", authManager.Protect(http.HandlerFunc(configHandler.Handle)))
	http.Handle("/api/env", authManager.Protect(http.HandlerFunc(envHandler.Handle)))

	// Common API routes (protected) - Used by both Agent and IDE
	http.Handle("/api/upload", authManager.Protect(http.HandlerFunc(uploadHandler.Handle)))
	http.Handle("/api/download", authManager.Protect(http.HandlerFunc(downloadHandler.Handle)))
	http.Handle("/api/exec", authManager.Protect(http.HandlerFunc(execHandler.Handle)))
	http.Handle("/api/tree", authManager.Protect(http.HandlerFunc(ideHandler.HandleFileTree)))
	http.Handle("/api/files", authManager.Protect(http.HandlerFunc(ideHandler.HandleFiles)))
	http.Handle("/metrics", http.HandlerFunc(metricsHandler.Handle))

	// Agent special routes (protected)
	http.Handle("/api/agent/file", authManager.Protect(http.HandlerFunc(fileHandler.HandleOperation)))

	// Serve static files from front/dist directory (built React app) - Must be last
	fs := http.FileServer(http.Dir("./front/dist"))
	http.Handle("/", fs)

	// Get port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "22531"
	}

	log.Printf("Agent server starting on port %s", port)

	// Check if authentication is enabled
	if authManager.IsAuthEnabled() {
		log.Printf("Authentication: ENABLED")
	} else {
		log.Printf("Authentication: DISABLED")
	}

	log.Printf("Available endpoints:")
	log.Printf("    GET    /health            - Health check")
	log.Printf("    GET    /                  - Web IDE interface")
	log.Printf("    WS     /ws/terminal       - Interactive terminal (WebSocket)")
	log.Printf("    GET    /api/config        - Get configuration")
	log.Printf("    GET    /api/env           - List environment variables")
	log.Printf("    POST   /api/env           - Set environment variable")
	log.Printf("    DELETE /api/env           - Unset environment variable")
	log.Printf("    POST   /api/upload        - Upload files")
	log.Printf("    GET    /api/download      - Download files")
	log.Printf("    POST   /api/exec          - Execute commands")
	log.Printf("    GET    /api/tree          - Get file tree")
	log.Printf("    GET    /api/files         - Read file content")
	log.Printf("    POST   /api/files         - Create file or directory")
	log.Printf("    PUT    /api/files         - Save file content")
	log.Printf("    DELETE /api/files         - Delete file or directory")
	log.Printf("    GET    /metrics           - View metrics")
	log.Printf("    POST   /api/agent/file    - File operations (view/create/str_replace/insert/undo_edit)")

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
