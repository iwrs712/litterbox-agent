package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/iwrs712/litterbox-agent/internal/envd"
	filesystemv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem/filesystemv1connect"
	processv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/process/processv1connect"
	"github.com/iwrs712/litterbox-agent/internal/fsapi"
	"github.com/iwrs712/litterbox-agent/internal/processes"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Config struct {
	WorkspaceRoot        string
	FilesystemRoot       string
	FilesystemPublicRoot string
}

type Server struct {
	fs        *fsapi.Filesystem
	processes *processes.Manager
	workspace *workspace
	handler   http.Handler
	// skills serialises syncs: each one sets the whole skills directory, so two
	// at once would remove each other's staging directories.
	skills sync.Mutex
}

func New(config Config) *Server {
	var filesystem *fsapi.Filesystem
	var err error
	if config.FilesystemRoot != "" {
		publicRoot := config.FilesystemPublicRoot
		if publicRoot == "" {
			publicRoot = "/"
		}
		filesystem, err = fsapi.NewWithPublicRoot(config.FilesystemRoot, publicRoot)
	} else {
		filesystem, err = fsapi.New(config.WorkspaceRoot)
	}
	if err != nil {
		panic(err)
	}
	server := &Server{fs: filesystem, processes: processes.NewManager(), workspace: &workspace{}}
	mux := http.NewServeMux()
	filesystemPath, filesystemHandler := filesystemv1connect.NewFilesystemHandler(envd.NewFilesystemService(filesystem))
	processPath, processHandler := processv1connect.NewProcessHandler(envd.NewProcessService(server.processes, filesystem))
	mux.Handle(filesystemPath, filesystemHandler)
	mux.Handle(processPath, processHandler)
	mux.Handle("/files", envd.NewFilesHandler(filesystem))
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("GET /{$}", server.root)
	// The skills directory is set to the tenant's selection in one call.
	mux.HandleFunc("POST /v1/skills/ensure", server.ensureSkills)
	mux.HandleFunc("POST /v1/workspace/restore", server.restore)
	mux.HandleFunc("POST /v1/workspace/sync", server.sync)
	mux.HandleFunc("POST /v1/processes", server.processStart)
	// Keep a method-agnostic fallback for older Go HTTP mux behavior and
	// proxies that normalize the collection URL. The method-specific routes
	// above remain the primary registrations on current runtimes.
	mux.HandleFunc("/v1/processes", server.processRESTCompat)
	mux.HandleFunc("/v1/processes/", server.processRESTCompat)
	// Keep removed legacy routes explicitly 404 instead of net/http's automatic
	// 405 so clients can distinguish an unsupported API from a method typo.
	mux.HandleFunc("POST /v1/processes/run", server.notFound)
	mux.HandleFunc("GET /v1/processes", server.processList)
	mux.HandleFunc("GET /v1/processes/{id}", server.processGet)
	mux.HandleFunc("GET /v1/processes/{id}/logs", server.processLogs)
	mux.HandleFunc("POST /v1/processes/{id}/input", server.processInput)
	mux.HandleFunc("POST /v1/processes/{id}/signal", server.processSignal)
	mux.HandleFunc("POST /v1/processes/{id}/stop", server.processStop)
	mux.HandleFunc("POST /v1/processes/{id}/restart", server.processRestart)
	mux.HandleFunc("GET /v1/processes/{id}/describe", server.processGet)
	// Every route goes through the gate; a route added later is closed until a
	// restore has finished unless it is named in gateExempt.
	server.handler = h2c.NewHandler(server.workspace.gate(mux), &http2.Server{})
	return server
}

// Close releases child processes before the enclosing HTTP server stops.
func (s *Server) Close() {
	s.processes.CloseAll(2 * time.Second)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name": "envd",
		"docs": "https://github.com/iwrs712/litterbox/tree/main/envd",
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	state := "restoring"
	if s.workspace.ready() {
		state = "ready"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"service":   "envd",
		"workspace": state,
	})
}

func (s *Server) notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, fsapi.ErrInvalidPath) {
		status = http.StatusBadRequest
	} else if errors.Is(err, os.ErrNotExist) {
		status = http.StatusNotFound
	} else if errors.Is(err, io.EOF) {
		status = http.StatusBadRequest
	} else if errors.Is(err, http.ErrBodyReadAfterClose) {
		status = http.StatusBadRequest
	} else if errors.Is(err, http.ErrNotSupported) {
		status = http.StatusBadRequest
	} else if errors.Is(err, io.ErrUnexpectedEOF) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
