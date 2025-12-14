package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

type TerminalWSHandler struct{}

func NewTerminalWSHandler() *TerminalWSHandler {
	return &TerminalWSHandler{}
}

func (h *TerminalWSHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Note: Authentication is handled by the authManager.Protect middleware
	// which checks X-Token header before this handler is called

	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[TerminalWS] Failed to upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Get initial directory from DEFAULT_DIR env or user home
	initialDir := os.Getenv("DEFAULT_DIR")
	if initialDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			initialDir = home
		}
	}

	// Start shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if initialDir != "" {
		cmd.Dir = initialDir
	}

	// Start PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[TerminalWS] Failed to start PTY: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to start terminal"))
		return
	}
	defer func() {
		ptmx.Close()
		cmd.Process.Kill()
	}()

	// Set PTY size if provided in query params
	rows := 24
	cols := 80
	if r.URL.Query().Get("rows") != "" {
		if n, err := parseSize(r.URL.Query().Get("rows")); err == nil {
			rows = n
		}
	}
	if r.URL.Query().Get("cols") != "" {
		if n, err := parseSize(r.URL.Query().Get("cols")); err == nil {
			cols = n
		}
	}
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})

	log.Printf("[TerminalWS] New terminal session started: shell=%s, dir=%s, size=%dx%d", shell, initialDir, rows, cols)

	// Handle PTY -> WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("[TerminalWS] PTY read error: %v", err)
				}
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Printf("[TerminalWS] WebSocket write error: %v", err)
				return
			}
		}
	}()

	// Handle WebSocket -> PTY
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[TerminalWS] WebSocket closed normally")
			} else {
				log.Printf("[TerminalWS] WebSocket read error: %v", err)
			}
			return
		}

		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			// Check if this is a resize message
			if len(data) > 0 && data[0] == 1 {
				// Resize message format: [1, rows_high, rows_low, cols_high, cols_low]
				if len(data) >= 5 {
					rows := int(data[1])<<8 | int(data[2])
					cols := int(data[3])<<8 | int(data[4])
					pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(rows),
						Cols: uint16(cols),
					})
					log.Printf("[TerminalWS] Terminal resized to %dx%d", rows, cols)
				}
				continue
			}

			// Normal input
			if _, err := ptmx.Write(data); err != nil {
				log.Printf("[TerminalWS] PTY write error: %v", err)
				return
			}
		}
	}
}

func parseSize(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
