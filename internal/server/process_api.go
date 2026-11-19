package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iwrs712/litterbox-agent/internal/fsapi"
	"github.com/iwrs712/litterbox-agent/internal/processes"
)

const (
	maxProcessArgs       = 256
	maxProcessArgBytes   = 1 << 20
	maxProcessLogEntries = 1000
)

type processStartRequest struct {
	Application string               `json:"application,omitempty"`
	Command     string               `json:"command,omitempty"`
	Args        []string             `json:"args,omitempty"`
	Cwd         string               `json:"cwd,omitempty"`
	Env         map[string]string    `json:"env,omitempty"`
	Tag         string               `json:"tag,omitempty"`
	Stdin       *bool                `json:"stdin,omitempty"`
	TTY         bool                 `json:"tty,omitempty"`
	Cols        uint16               `json:"cols,omitempty"`
	Rows        uint16               `json:"rows,omitempty"`
	Ready       *processReadyRequest `json:"ready,omitempty"`
	Restart     string               `json:"restart,omitempty"`
	Persist     bool                 `json:"persist,omitempty"`
	Detached    bool                 `json:"detached,omitempty"`
}

type processReadyRequest struct {
	Log     string  `json:"log,omitempty"`
	Port    int     `json:"port,omitempty"`
	Host    string  `json:"host,omitempty"`
	Timeout float64 `json:"timeout,omitempty"`
}

type processRestartRequest struct {
	WaitMS int                  `json:"wait_ms,omitempty"`
	Spec   *processStartRequest `json:"spec,omitempty"`
}

type processStartResponse struct {
	processes.Info
	Process processes.Info `json:"process"`
}

type processListResponse struct {
	Processes []processes.Info `json:"processes"`
}

// processRESTCompat dispatches the versioned REST process API without relying
// on method-aware ServeMux patterns. Some older envd images/proxies use the
// legacy path matcher, which otherwise turns `GET /v1/processes` into a 404.
// The explicit method routes registered in server.New remain the fast path.
func (s *Server) processRESTCompat(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/processes")
	path = strings.Trim(path, "/")
	if path == "" {
		switch r.Method {
		case http.MethodGet:
			s.processList(w, r)
		case http.MethodPost:
			s.processStart(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	r.SetPathValue("id", parts[0])
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.processGet(w, r)
		default:
			w.Header().Set("Allow", "GET")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}

	switch parts[1] {
	case "logs":
		if r.Method == http.MethodGet {
			s.processLogs(w, r)
			return
		}
	case "describe":
		if r.Method == http.MethodGet {
			s.processGet(w, r)
			return
		}
	case "input":
		if r.Method == http.MethodPost {
			s.processInput(w, r)
			return
		}
	case "signal":
		if r.Method == http.MethodPost {
			s.processSignal(w, r)
			return
		}
	case "stop":
		if r.Method == http.MethodPost {
			s.processStop(w, r)
			return
		}
	case "restart":
		if r.Method == http.MethodPost {
			s.processRestart(w, r)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Allow", "GET, POST")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

type processLogEvent struct {
	Cursor uint64 `json:"cursor"`
	Kind   string `json:"kind"`
	Data   string `json:"data"`
}

type processLogsResponse struct {
	Process    processes.Info    `json:"process"`
	Events     []processLogEvent `json:"events"`
	NextCursor uint64            `json:"next_cursor"`
	Truncated  bool              `json:"truncated,omitempty"`
	TimedOut   bool              `json:"timed_out,omitempty"`
}

type processInputRequest struct {
	Data  string   `json:"data,omitempty"`
	Text  string   `json:"text,omitempty"`
	Enter *bool    `json:"enter,omitempty"`
	PTY   bool     `json:"pty,omitempty"`
	Keys  []string `json:"keys,omitempty"`
}

type processSignalRequest struct {
	Signal string `json:"signal"`
}

type processWaitRequest struct {
	WaitMS int `json:"wait_ms,omitempty"`
}

func (s *Server) processStart(w http.ResponseWriter, r *http.Request) {
	var request processStartRequest
	if err := decodeJSONBody(w, r, &request, 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	command := strings.TrimSpace(request.Application)
	if command == "" {
		command = strings.TrimSpace(request.Command)
	}
	if command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "application or command is required"})
		return
	}
	if len(request.Args) > maxProcessArgs {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many process arguments"})
		return
	}
	argBytes := 0
	for _, arg := range request.Args {
		argBytes += len([]byte(arg))
	}
	if argBytes > maxProcessArgBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "process arguments are too large"})
		return
	}
	if request.Tag != "" && !validProcessTag(request.Tag) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag must be 1-48 letters, numbers, dots, underscores, or hyphens"})
		return
	}
	cwdPublic := request.Cwd
	if cwdPublic == "" {
		cwdPublic = s.fs.PublicRoot()
	}
	cwd, err := s.fs.ResolveDirectory(cwdPublic)
	if err != nil {
		writeProcessError(w, err)
		return
	}
	stdin := true
	if request.Stdin != nil {
		stdin = *request.Stdin
	}
	var ready *processes.ReadySpec
	if request.Ready != nil {
		if request.Ready.Log == "" && request.Ready.Port <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ready requires log or port"})
			return
		}
		if request.Ready.Port < 0 || request.Ready.Port > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ready.port must be between 1 and 65535"})
			return
		}
		if request.Ready.Log != "" {
			if _, err := regexp.Compile(request.Ready.Log); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ready.log"})
				return
			}
		}
		timeout := time.Duration(request.Ready.Timeout * float64(time.Second))
		ready = &processes.ReadySpec{Log: request.Ready.Log, Port: request.Ready.Port, Host: request.Ready.Host, Timeout: timeout}
	}
	info, err := s.processes.Start(processes.StartSpec{
		Command: command, Args: request.Args, Cwd: cwd, PublicCwd: cwdPublic,
		Env: request.Env, Tag: request.Tag, Stdin: stdin, TTY: request.TTY,
		Cols: request.Cols, Rows: request.Rows, Interactive: stdin,
		Ready: ready, Restart: request.Restart, Persist: request.Persist, Detached: request.Detached,
	})
	if err != nil {
		writeProcessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, processStartResponse{Info: info, Process: info})
}

func (s *Server) processList(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	items := s.processes.List()
	if tag != "" {
		items = slicesFilterProcessTag(items, tag)
	}
	writeJSON(w, http.StatusOK, processListResponse{Processes: items})
}

func (s *Server) processGet(w http.ResponseWriter, r *http.Request) {
	info, err := s.processes.Get(r.PathValue("id"))
	if err != nil {
		writeProcessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) processLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	selector := processes.Selector{Tag: ""}
	info, err := s.processes.Get(id)
	if err != nil {
		writeProcessError(w, err)
		return
	}
	selector.PID = info.PID
	after, err := queryUint64(r, "cursor", 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cursor must be a non-negative integer"})
		return
	}
	limit, err := queryInt(r, "limit", 100)
	if err != nil || limit < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
		return
	}
	if limit > maxProcessLogEntries {
		limit = maxProcessLogEntries
	}
	head := r.URL.Query().Get("head") == "true"
	grep := strings.TrimSpace(r.URL.Query().Get("grep"))
	var pattern *regexp.Regexp
	if grep != "" {
		pattern, err = regexp.Compile(grep)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "grep must be a valid regular expression"})
			return
		}
	}
	follow := r.URL.Query().Get("follow") == "true"
	timeoutMS, err := queryInt(r, "timeout_ms", 30000)
	if err != nil || timeoutMS < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "timeout_ms must be a non-negative integer"})
		return
	}
	deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	var events []processes.Event
	truncated := false
	for {
		events, _, truncated, err = s.processes.ReadEventsWithOptions(selector, after, limit, head, pattern)
		if err != nil {
			writeProcessError(w, err)
			return
		}
		if len(events) > 0 || !follow || time.Now().After(deadline) || info.Status != "running" {
			break
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(25 * time.Millisecond):
		}
		info, _ = s.processes.Get(id)
	}
	result := make([]processLogEvent, 0, len(events))
	next := after
	for _, event := range events {
		next = event.Cursor
		result = append(result, processLogEvent{Cursor: event.Cursor, Kind: outputKindName(event.Kind), Data: string(event.Data)})
	}
	writeJSON(w, http.StatusOK, processLogsResponse{Process: info, Events: result, NextCursor: next, Truncated: truncated, TimedOut: follow && len(events) == 0})
}

func (s *Server) processInput(w http.ResponseWriter, r *http.Request) {
	info, err := s.processes.Get(r.PathValue("id"))
	if err != nil {
		writeProcessError(w, err)
		return
	}
	var request processInputRequest
	if err := decodeJSONBody(w, r, &request, 1<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	data := request.Data
	if data == "" {
		data = request.Text
		enter := true
		if request.Enter != nil {
			enter = *request.Enter
		}
		if enter && data != "" && !strings.HasSuffix(data, "\n") {
			data += "\n"
		}
	}
	for _, key := range request.Keys {
		if key == "CTRL_C" {
			if err := s.processes.SendSignal(processes.Selector{PID: info.PID}, syscall.SIGINT); err != nil {
				writeProcessError(w, err)
				return
			}
			continue
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported key: " + key})
		return
	}
	if data == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "data or text is required"})
		return
	}
	if err := s.processes.SendInput(processes.Selector{PID: info.PID}, []byte(data), request.PTY); err != nil {
		writeProcessError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) processSignal(w http.ResponseWriter, r *http.Request) {
	info, err := s.processes.Get(r.PathValue("id"))
	if err != nil {
		writeProcessError(w, err)
		return
	}
	var request processSignalRequest
	if err := decodeJSONBody(w, r, &request, 16<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	signal, ok := parseProcessSignal(request.Signal)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported signal"})
		return
	}
	if err := s.processes.SendSignal(processes.Selector{PID: info.PID}, signal); err != nil {
		writeProcessError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) processStop(w http.ResponseWriter, r *http.Request) {
	wait := parseWaitBody(w, r)
	if wait < 0 {
		return
	}
	info, err := s.processes.StopSelector(processes.Selector{PID: mustProcessPID(s, r.PathValue("id"))}, time.Duration(wait)*time.Millisecond)
	if err != nil {
		writeProcessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) processRestart(w http.ResponseWriter, r *http.Request) {
	var request processRestartRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSONBody(w, r, &request, 2<<20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else {
		request.WaitMS = 5000
	}
	if request.WaitMS < 0 || request.WaitMS > 120000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wait_ms must be between 0 and 120000"})
		return
	}
	selector := processes.Selector{PID: mustProcessPID(s, r.PathValue("id"))}
	if request.Spec != nil {
		spec, err := s.startSpecFromRequest(*request.Spec)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		info, err := s.processes.RestartWithSpec(selector, time.Duration(request.WaitMS)*time.Millisecond, spec)
		if err != nil {
			writeProcessError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
		return
	}
	info, err := s.processes.Restart(selector, time.Duration(request.WaitMS)*time.Millisecond)
	if err != nil {
		writeProcessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) startSpecFromRequest(request processStartRequest) (processes.StartSpec, error) {
	command := strings.TrimSpace(request.Application)
	if command == "" {
		command = strings.TrimSpace(request.Command)
	}
	if command == "" {
		return processes.StartSpec{}, errors.New("application or command is required")
	}
	cwdPublic := request.Cwd
	if cwdPublic == "" {
		cwdPublic = s.fs.PublicRoot()
	}
	cwd, err := s.fs.ResolveDirectory(cwdPublic)
	if err != nil {
		return processes.StartSpec{}, err
	}
	stdin := true
	if request.Stdin != nil {
		stdin = *request.Stdin
	}
	var ready *processes.ReadySpec
	if request.Ready != nil {
		timeout := time.Duration(request.Ready.Timeout * float64(time.Second))
		ready = &processes.ReadySpec{Log: request.Ready.Log, Port: request.Ready.Port, Host: request.Ready.Host, Timeout: timeout}
	}
	return processes.StartSpec{Command: command, Args: request.Args, Cwd: cwd, PublicCwd: cwdPublic, Env: request.Env, Tag: request.Tag, Stdin: stdin, TTY: request.TTY, Cols: request.Cols, Rows: request.Rows, Interactive: stdin, Ready: ready, Restart: request.Restart, Persist: request.Persist, Detached: request.Detached}, nil
}

func parseWaitBody(w http.ResponseWriter, r *http.Request) int {
	if r.Body == nil || r.ContentLength == 0 {
		return 5000
	}
	var request processWaitRequest
	if err := decodeJSONBody(w, r, &request, 16<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return -1
	}
	if request.WaitMS < 0 || request.WaitMS > 120000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wait_ms must be between 0 and 120000"})
		return -1
	}
	return request.WaitMS
}

func mustProcessPID(s *Server, id string) int {
	info, err := s.processes.Get(id)
	if err != nil {
		return 0
	}
	return info.PID
}

func queryInt(r *http.Request, key string, fallback int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func queryUint64(r *http.Request, key string, fallback uint64) (uint64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func validProcessTag(tag string) bool {
	if len(tag) < 1 || len(tag) > 48 {
		return false
	}
	for _, char := range tag {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func slicesFilterProcessTag(items []processes.Info, tag string) []processes.Info {
	filtered := make([]processes.Info, 0, len(items))
	for _, item := range items {
		if item.Tag == tag {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func outputKindName(kind processes.OutputKind) string {
	switch kind {
	case processes.OutputStdout:
		return "stdout"
	case processes.OutputStderr:
		return "stderr"
	case processes.OutputPTY:
		return "pty"
	default:
		return "unknown"
	}
}

func parseProcessSignal(value string) (syscall.Signal, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SIGINT", "INT", "2":
		return syscall.SIGINT, true
	case "SIGTERM", "TERM", "15":
		return syscall.SIGTERM, true
	case "SIGHUP", "HUP", "1":
		return syscall.SIGHUP, true
	case "SIGQUIT", "QUIT", "3":
		return syscall.SIGQUIT, true
	case "SIGKILL", "KILL", "9":
		return syscall.SIGKILL, true
	default:
		return 0, false
	}
}

func writeProcessError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, processes.ErrNotFound), errors.Is(err, fsapi.ErrInvalidPath):
		status = http.StatusNotFound
	case errors.Is(err, processes.ErrTagConflict):
		status = http.StatusConflict
	case errors.Is(err, processes.ErrNotTerminal), errors.Is(err, processes.ErrNoStdin):
		status = http.StatusConflict
	case errors.Is(err, processes.ErrRunning):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
