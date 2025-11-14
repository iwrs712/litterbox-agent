package main

import (
	"log"
	"net/http"
	"os"

	"litterbox-agent/internal/handler"
	"litterbox-agent/internal/service"
)

func main() {
	// Initialize services
	fileService := service.NewFileService()
	execService := service.NewExecService()
	metricsService := service.NewMetricsService()

	// Initialize handlers
	uploadHandler := handler.NewUploadHandler(fileService, metricsService)
	downloadHandler := handler.NewDownloadHandler(fileService, metricsService)
	execHandler := handler.NewExecHandler(execService, metricsService)
	metricsHandler := handler.NewMetricsHandler(metricsService)
	fileHandler := handler.NewFileHandler(fileService, metricsService)

	// Register routes
	http.HandleFunc("/upload", uploadHandler.Handle)
	http.HandleFunc("/download", downloadHandler.Handle)
	http.HandleFunc("/exec", execHandler.Handle)
	http.HandleFunc("/metrics", metricsHandler.Handle)
	http.HandleFunc("/file", fileHandler.HandleOperation)

	// Get port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Agent server starting on port %s", port)
	log.Printf("Available endpoints:")
	log.Printf("  POST   /upload       - Upload files")
	log.Printf("  GET    /download     - Download files")
	log.Printf("  POST   /exec         - Execute commands")
	log.Printf("  GET    /metrics      - View metrics")
	log.Printf("  POST   /file         - File operations (view/create/str_replace/insert/undo_edit)")

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
