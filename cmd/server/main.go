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

	// Initialize handlers
	initHandler := handler.NewInitHandler(authManager)
	uploadHandler := handler.NewUploadHandler(fileService, metricsService)
	downloadHandler := handler.NewDownloadHandler(fileService, metricsService)
	execHandler := handler.NewExecHandler(execService, metricsService)
	metricsHandler := handler.NewMetricsHandler(metricsService)
	fileHandler := handler.NewFileHandler(fileService, metricsService)

	// Register routes
	// /init does not require authentication (but can only succeed once)
	http.HandleFunc("/init", initHandler.Handle)

	// /health does not require authentication
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Protected routes (require authentication)
	http.Handle("/upload", authManager.Protect(http.HandlerFunc(uploadHandler.Handle)))
	http.Handle("/download", authManager.Protect(http.HandlerFunc(downloadHandler.Handle)))
	http.Handle("/exec", authManager.Protect(http.HandlerFunc(execHandler.Handle)))
	http.Handle("/metrics", authManager.Protect(http.HandlerFunc(metricsHandler.Handle)))
	http.Handle("/file", authManager.Protect(http.HandlerFunc(fileHandler.HandleOperation)))

	// Get port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Agent server starting on port %s", port)
	log.Printf("Available endpoints:")
	log.Printf("  POST   /init         - Initialize authentication (one-time only)")
	log.Printf("  GET    /health       - Health check")
	log.Printf("  POST   /upload       - Upload files")
	log.Printf("  GET    /download     - Download files")
	log.Printf("  POST   /exec         - Execute commands")
	log.Printf("  GET    /metrics      - View metrics")
	log.Printf("  POST   /file         - File operations (view/create/str_replace/insert/undo_edit)")

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
