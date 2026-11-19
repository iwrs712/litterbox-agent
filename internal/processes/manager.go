package processes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

var (
	ErrNotFound    = errors.New("process not found")
	ErrRunning     = errors.New("process is still running")
	ErrTagConflict = errors.New("process tag already exists")
	ErrNotTerminal = errors.New("process is not running in tty mode")
	ErrNoStdin     = errors.New("process stdin is not available")
)

type OutputKind uint8

const (
	OutputStdout OutputKind = iota + 1
	OutputStderr
	OutputPTY
)

type Event struct {
	// Cursor is a monotonically increasing process-local output position. It is
	// intentionally part of the envd contract so reconnecting clients can
	// request only events they have not seen yet.
	Cursor uint64 `json:"cursor,omitempty"`
	Kind   OutputKind
	Data   []byte
}

// ReadySpec describes process-level startup conditions. Every configured
// condition must be observed before the process transitions to ready.
type ReadySpec struct {
	Log     string
	Port    int
	Host    string
	Timeout time.Duration
}

// ReadEvents returns bounded output history after cursor. A zero cursor means
// "tail" and returns the most recent limit events; non-zero cursors return
// only events whose cursor is greater than after. History is intentionally
// bounded by the managed process, so callers should treat the returned
// earliest cursor as the replay floor.
func (m *Manager) ReadEvents(selector Selector, after uint64, limit int) ([]Event, uint64, bool, error) {
	return m.ReadEventsWithOptions(selector, after, limit, false, nil)
}

// ReadEventsWithOptions is the process log primitive used by the REST API.
// “head“ changes only the zero-cursor selection; a non-zero cursor always
// means "after this cursor". A regex filters complete output lines while
// retaining the original event cursor for reconnects.
func (m *Manager) ReadEventsWithOptions(selector Selector, after uint64, limit int, head bool, pattern *regexp.Regexp) ([]Event, uint64, bool, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return nil, after, false, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	process.mu.RLock()
	history := make([]Event, len(process.history))
	for index, event := range process.history {
		history[index] = cloneEvent(event)
	}
	process.mu.RUnlock()
	if len(history) == 0 {
		return nil, after, false, nil
	}
	start := 0
	if after == 0 && !head {
		if len(history) > limit {
			start = len(history) - limit
		}
	} else {
		for start < len(history) && history[start].Cursor <= after {
			start++
		}
	}
	truncated := after > 0 && after+1 < history[0].Cursor
	if start >= len(history) {
		return nil, after, truncated, nil
	}
	end := start + limit
	if end > len(history) {
		end = len(history)
	}
	result := history[start:end]
	if pattern != nil {
		filtered := make([]Event, 0, len(result))
		for _, event := range result {
			lines := bytes.Split(event.Data, []byte("\n"))
			kept := make([][]byte, 0, len(lines))
			for _, line := range lines {
				if pattern.Match(line) {
					kept = append(kept, line)
				}
			}
			if len(kept) == 0 {
				continue
			}
			data := bytes.Join(kept, []byte("\n"))
			if len(data) > 0 && bytes.HasSuffix(event.Data, []byte("\n")) {
				data = append(data, '\n')
			}
			event.Data = data
			filtered = append(filtered, event)
		}
		result = filtered
	}
	if len(result) > limit {
		if head {
			result = result[:limit]
		} else {
			result = result[len(result)-limit:]
		}
	}
	next := after
	if len(result) > 0 {
		next = result[len(result)-1].Cursor
	}
	return result, next, truncated, nil
}

type Selector struct {
	PID int
	Tag string
}

type Manager struct {
	nextID    atomic.Uint64
	mu        sync.RWMutex
	processes map[string]*managed
	starting  map[string]struct{}
}

type StartSpec struct {
	Command     string
	Args        []string
	Cwd         string
	PublicCwd   string
	Env         map[string]string
	Tag         string
	Stdin       bool
	TTY         bool
	Cols        uint16
	Rows        uint16
	Interactive bool
	Ready       *ReadySpec
	Restart     string
	Persist     bool
	Detached    bool
}

type RunSpec struct {
	Command        string
	Args           []string
	Cwd            string
	Env            map[string]string
	Timeout        time.Duration
	MaxOutputBytes int
}

type RunResult struct {
	ExitCode        *int   `json:"exitCode"`
	TimedOut        bool   `json:"timedOut"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdoutTruncated"`
	StderrTruncated bool   `json:"stderrTruncated"`
	DurationMS      int64  `json:"durationMs"`
}

type Info struct {
	ID           string            `json:"id"`
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Cwd          string            `json:"cwd"`
	Env          map[string]string `json:"-"`
	TTY          bool              `json:"tty"`
	Interactive  bool              `json:"interactive"`
	Status       string            `json:"status"`
	PID          int               `json:"pid,omitempty"`
	Tag          string            `json:"tag,omitempty"`
	ExitCode     *int              `json:"exitCode,omitempty"`
	CreatedAtMS  int64             `json:"createdAtMs"`
	ExitedAtMS   *int64            `json:"exitedAtMs,omitempty"`
	ReadyAtMS    *int64            `json:"readyAtMs,omitempty"`
	ReadyMatch   string            `json:"readyMatch,omitempty"`
	ReadyPending []string          `json:"readyPending,omitempty"`
	Restart      string            `json:"restart,omitempty"`
	RestartCount int               `json:"restartCount,omitempty"`
	Persist      bool              `json:"persist"`
	Detached     bool              `json:"detached"`
}

type managed struct {
	id          string
	command     string
	args        []string
	cwd         string
	tty         bool
	interactive bool
	tag         string
	env         map[string]string
	stdin       io.WriteCloser
	cmd         *exec.Cmd
	pty         *os.File
	createdAt   time.Time

	mu               sync.RWMutex
	exitedAt         *time.Time
	exitCode         *int
	done             chan struct{}
	doneOnce         sync.Once
	subscribers      map[chan []byte]struct{}
	eventSubscribers map[chan Event]struct{}
	history          []Event
	startSpec        StartSpec
	nextCursor       atomic.Uint64
	streamsDone      sync.WaitGroup
	manager          *Manager
	readySpec        *ReadySpec
	readyPattern     *regexp.Regexp
	readyBuffer      string
	readyAt          *time.Time
	readyMatch       string
	readyPort        bool
	readyLog         bool
	readyPending     []string
	restartPolicy    string
	restartCount     int
	persist          bool
	detached         bool
	stopRequested    bool
	readyChanged     chan struct{}
}

func NewManager() *Manager {
	return &Manager{processes: make(map[string]*managed), starting: make(map[string]struct{})}
}

func validateStartSpec(spec *StartSpec) error {
	if spec.Restart == "" {
		spec.Restart = "no"
	}
	if spec.Restart != "no" && spec.Restart != "on-failure" && spec.Restart != "always" {
		return fmt.Errorf("restart must be one of: no, on-failure, always")
	}
	if spec.Ready != nil {
		if spec.Ready.Log == "" && spec.Ready.Port <= 0 {
			return errors.New("ready requires log or port")
		}
		if spec.Ready.Port < 0 || spec.Ready.Port > 65535 {
			return errors.New("ready.port must be between 1 and 65535")
		}
		if spec.Ready.Timeout <= 0 {
			spec.Ready.Timeout = 30 * time.Second
		}
		if spec.Ready.Log != "" {
			if _, err := regexp.Compile(spec.Ready.Log); err != nil {
				return fmt.Errorf("invalid ready.log: %w", err)
			}
		}
	}
	return nil
}

func cloneReadySpec(spec *ReadySpec) *ReadySpec {
	if spec == nil {
		return nil
	}
	clone := *spec
	return &clone
}

func (p *managed) initReadiness() {
	if p.readySpec == nil {
		return
	}
	p.readyPending = make([]string, 0, 2)
	if p.readySpec.Log != "" {
		p.readyPending = append(p.readyPending, "log")
	}
	if p.readySpec.Port > 0 {
		p.readyPending = append(p.readyPending, "port")
	}
}

func (m *Manager) Run(spec RunSpec) (RunResult, error) {
	if spec.Command == "" {
		return RunResult{}, errors.New("command is required")
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 120 * time.Second
	}
	if spec.MaxOutputBytes <= 0 {
		spec.MaxOutputBytes = 1 << 20
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), spec.Timeout)
	defer cancel()

	cmd := command(ctx, spec.Command, spec.Args, spec.Cwd, spec.Env)
	stdout := &boundedBuffer{limit: spec.MaxOutputBytes}
	stderr := &boundedBuffer{limit: spec.MaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	result := RunResult{
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded),
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
		DurationMS:      time.Since(started).Milliseconds(),
	}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	if err != nil && result.ExitCode == nil {
		return result, err
	}
	return result, nil
}

func (m *Manager) Start(spec StartSpec) (Info, error) {
	if spec.Command == "" {
		return Info{}, errors.New("command is required")
	}
	if err := validateStartSpec(&spec); err != nil {
		return Info{}, err
	}
	if spec.Detached {
		// Detached launches are deliberately non-interactive and survive the
		// client lifetime. stdin may remain true in the wire contract for
		// compatibility, but it is ignored by the detached child.
		spec.Persist = true
		spec.Stdin = false
		spec.TTY = false
		spec.Interactive = false
	}
	if spec.Tag != "" {
		m.mu.Lock()
		if _, exists := m.starting[spec.Tag]; exists {
			m.mu.Unlock()
			return Info{}, ErrTagConflict
		}
		for _, existing := range m.processes {
			if existing.tag == spec.Tag {
				info := existing.info()
				if info.Status == "running" {
					m.mu.Unlock()
					return Info{}, ErrTagConflict
				}
			}
		}
		m.starting[spec.Tag] = struct{}{}
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			delete(m.starting, spec.Tag)
			m.mu.Unlock()
		}()
	}
	cmd := command(context.Background(), spec.Command, spec.Args, spec.Cwd, spec.Env)
	id := fmt.Sprintf("p_%d", m.nextID.Add(1))
	process := &managed{
		id:               id,
		command:          spec.Command,
		args:             append([]string(nil), spec.Args...),
		cwd:              spec.PublicCwd,
		tty:              spec.TTY,
		interactive:      spec.Interactive,
		tag:              spec.Tag,
		env:              cloneEnv(spec.Env),
		cmd:              cmd,
		createdAt:        time.Now(),
		done:             make(chan struct{}),
		subscribers:      make(map[chan []byte]struct{}),
		eventSubscribers: make(map[chan Event]struct{}),
		startSpec:        cloneStartSpec(spec),
		manager:          m,
		readySpec:        cloneReadySpec(spec.Ready),
		readyChanged:     make(chan struct{}),
		restartPolicy:    spec.Restart,
		persist:          spec.Persist,
		detached:         spec.Detached,
	}
	process.initReadiness()
	if process.readySpec != nil && process.readySpec.Log != "" {
		process.readyPattern = regexp.MustCompile(process.readySpec.Log)
	}

	if spec.TTY {
		// pty.StartWithSize creates the controlling session/process group itself.
		// Keeping Setpgid here conflicts with that setup on macOS.
		cmd.SysProcAttr = nil
		cols, rows := spec.Cols, spec.Rows
		if cols == 0 {
			cols = 220
		}
		if rows == 0 {
			rows = 50
		}
		file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
		if err != nil {
			return Info{}, err
		}
		process.pty = file
	} else {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return Info{}, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return Info{}, err
		}
		if spec.Stdin {
			stdin, err := cmd.StdinPipe()
			if err != nil {
				return Info{}, err
			}
			process.stdin = stdin
		}
		if err := cmd.Start(); err != nil {
			return Info{}, err
		}
		process.streamsDone.Add(2)
		go process.readOutput(stdout, OutputStdout)
		go process.readOutput(stderr, OutputStderr)
	}

	m.mu.Lock()
	m.processes[id] = process
	m.mu.Unlock()
	if process.pty != nil {
		go process.readPTY()
	}
	go process.wait()
	return process.info(), nil
}

func (m *Manager) Get(id string) (Info, error) {
	process, err := m.get(id)
	if err != nil {
		return Info{}, err
	}
	return process.info(), nil
}

func (m *Manager) List() []Info {
	m.mu.RLock()
	processes := make([]*managed, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.mu.RUnlock()
	sort.Slice(processes, func(i, j int) bool { return processes[i].createdAt.Before(processes[j].createdAt) })
	result := make([]Info, 0, len(processes))
	for _, process := range processes {
		result = append(result, process.info())
	}
	return result
}

// CloseAll terminates every live child. envd owns these processes, so
// leaving them running after HTTP shutdown would orphan project-scoped jobs.
func (m *Manager) CloseAll(wait time.Duration) {
	m.mu.RLock()
	processList := make([]*managed, 0, len(m.processes))
	for _, process := range m.processes {
		processList = append(processList, process)
	}
	m.mu.RUnlock()
	for _, process := range processList {
		process.signal(syscall.SIGTERM)
	}
	if wait <= 0 {
		return
	}
	deadline := time.Now().Add(wait)
	for _, process := range processList {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			process.signal(syscall.SIGKILL)
			continue
		}
		select {
		case <-process.done:
		case <-time.After(remaining):
			process.signal(syscall.SIGKILL)
		}
	}
}

func (m *Manager) GetBySelector(selector Selector) (Info, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return Info{}, err
	}
	return process.info(), nil
}

// StartSpec returns the original launch specification for a managed process.
// It is used by the envd restart API; callers receive a deep copy.
func (m *Manager) StartSpec(selector Selector) (StartSpec, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return StartSpec{}, err
	}
	process.mu.RLock()
	defer process.mu.RUnlock()
	return cloneStartSpec(process.startSpec), nil
}

// Restart terminates a process and starts a fresh child with the same launch
// specification. The old process record is removed only after it exits, so a
// failed stop cannot accidentally orphan a running child.
func (m *Manager) Restart(selector Selector, wait time.Duration) (Info, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return Info{}, err
	}
	process.mu.Lock()
	process.stopRequested = true
	process.mu.Unlock()
	spec, err := m.StartSpec(selector)
	if err != nil {
		return Info{}, err
	}
	process.signal(syscall.SIGTERM)
	if wait > 0 {
		select {
		case <-process.done:
		case <-time.After(wait):
			return process.info(), ErrRunning
		}
	} else {
		select {
		case <-process.done:
		default:
			return process.info(), ErrRunning
		}
	}
	m.mu.Lock()
	if m.processes[process.id] == process {
		delete(m.processes, process.id)
	}
	m.mu.Unlock()
	return m.Start(spec)
}

// RestartWithSpec is the explicit variant used by remote callers that need
// to update a retained launch policy while preserving the process identity
// boundary (the returned Info always belongs to the replacement child).
func (m *Manager) RestartWithSpec(selector Selector, wait time.Duration, spec StartSpec) (Info, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return Info{}, err
	}
	process.mu.Lock()
	process.stopRequested = true
	process.mu.Unlock()
	process.signal(syscall.SIGTERM)
	if wait > 0 {
		select {
		case <-process.done:
		case <-time.After(wait):
			return process.info(), ErrRunning
		}
	} else {
		select {
		case <-process.done:
		default:
			return process.info(), ErrRunning
		}
	}
	m.mu.Lock()
	if m.processes[process.id] == process {
		delete(m.processes, process.id)
	}
	m.mu.Unlock()
	return m.Start(spec)
}

func (m *Manager) StopSelector(selector Selector, wait time.Duration) (Info, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return Info{}, err
	}
	process.mu.Lock()
	process.stopRequested = true
	process.mu.Unlock()
	process.signal(syscall.SIGTERM)
	if wait > 0 {
		select {
		case <-process.done:
		case <-time.After(wait):
		}
	}
	return process.info(), nil
}

func (m *Manager) Done(selector Selector) <-chan struct{} {
	process, err := m.getBySelector(selector)
	if err != nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return process.done
}

func (m *Manager) Stop(id string, wait time.Duration) (Info, error) {
	process, err := m.get(id)
	if err != nil {
		return Info{}, err
	}
	process.mu.Lock()
	process.stopRequested = true
	process.mu.Unlock()
	process.signal(syscall.SIGTERM)
	if wait > 0 {
		select {
		case <-process.done:
		case <-time.After(wait):
		}
	}
	return process.info(), nil
}

func (m *Manager) Delete(id string) error {
	process, err := m.get(id)
	if err != nil {
		return err
	}
	select {
	case <-process.done:
	default:
		return ErrRunning
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processes, id)
	return nil
}

func (m *Manager) Resize(id string, cols, rows uint16) error {
	process, err := m.get(id)
	if err != nil {
		return err
	}
	if process.pty == nil {
		return ErrNotTerminal
	}
	return pty.Setsize(process.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

func (m *Manager) ResizeSelector(selector Selector, cols, rows uint16) error {
	process, err := m.getBySelector(selector)
	if err != nil {
		return err
	}
	if process.pty == nil {
		return ErrNotTerminal
	}
	return pty.Setsize(process.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

func (m *Manager) Write(id string, data []byte) error {
	process, err := m.get(id)
	if err != nil {
		return err
	}
	if process.pty == nil {
		return ErrNotTerminal
	}
	_, err = process.pty.Write(data)
	return err
}

func (m *Manager) SendInput(selector Selector, data []byte, ptyInput bool) error {
	process, err := m.getBySelector(selector)
	if err != nil {
		return err
	}
	if ptyInput {
		if process.pty == nil {
			return ErrNotTerminal
		}
		_, err = process.pty.Write(data)
		return err
	}
	process.mu.RLock()
	stdin := process.stdin
	process.mu.RUnlock()
	if stdin == nil {
		return ErrNoStdin
	}
	_, err = stdin.Write(data)
	return err
}

func (m *Manager) CloseStdin(selector Selector) error {
	process, err := m.getBySelector(selector)
	if err != nil {
		return err
	}
	process.mu.Lock()
	stdin := process.stdin
	process.stdin = nil
	process.mu.Unlock()
	if stdin == nil {
		return ErrNoStdin
	}
	return stdin.Close()
}

func (m *Manager) SendSignal(selector Selector, signal syscall.Signal) error {
	process, err := m.getBySelector(selector)
	if err != nil {
		return err
	}
	process.signal(signal)
	return nil
}

func (m *Manager) Subscribe(id string) (<-chan []byte, func(), <-chan struct{}, error) {
	process, err := m.get(id)
	if err != nil {
		return nil, nil, nil, err
	}
	if process.pty == nil {
		return nil, nil, nil, ErrNotTerminal
	}
	updates := make(chan []byte, 128)
	process.mu.Lock()
	process.subscribers[updates] = struct{}{}
	process.mu.Unlock()
	unsubscribe := func() {
		process.mu.Lock()
		delete(process.subscribers, updates)
		process.mu.Unlock()
	}
	return updates, unsubscribe, process.done, nil
}

func (m *Manager) SubscribeEvents(selector Selector) (<-chan Event, func(), <-chan struct{}, error) {
	process, err := m.getBySelector(selector)
	if err != nil {
		return nil, nil, nil, err
	}
	updates := make(chan Event, 256)
	process.mu.Lock()
	for _, event := range process.history {
		updates <- cloneEvent(event)
	}
	process.eventSubscribers[updates] = struct{}{}
	process.mu.Unlock()
	unsubscribe := func() {
		process.mu.Lock()
		delete(process.eventSubscribers, updates)
		process.mu.Unlock()
	}
	return updates, unsubscribe, process.done, nil
}

func (m *Manager) get(id string) (*managed, error) {
	m.mu.RLock()
	process := m.processes[id]
	m.mu.RUnlock()
	if process == nil {
		return nil, ErrNotFound
	}
	return process, nil
}

func (m *Manager) getBySelector(selector Selector) (*managed, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if selector.PID != 0 {
		for _, process := range m.processes {
			if process.cmd.Process != nil && process.cmd.Process.Pid == selector.PID {
				return process, nil
			}
		}
	}
	if selector.Tag != "" {
		for _, process := range m.processes {
			if process.tag == selector.Tag {
				return process, nil
			}
		}
	}
	return nil, ErrNotFound
}

func (p *managed) readPTY() {
	buffer := make([]byte, 4096)
	for {
		count, err := p.pty.Read(buffer)
		if count > 0 {
			p.broadcast(append([]byte(nil), buffer[:count]...))
			p.broadcastEvent(Event{Kind: OutputPTY, Data: append([]byte(nil), buffer[:count]...)})
			p.trackReadiness(buffer[:count])
		}
		if err != nil {
			return
		}
	}
}

func (p *managed) readOutput(reader io.Reader, kind OutputKind) {
	defer p.streamsDone.Done()
	buffer := make([]byte, 4096)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			p.broadcastEvent(Event{Kind: kind, Data: append([]byte(nil), buffer[:count]...)})
			p.trackReadiness(buffer[:count])
		}
		if err != nil {
			return
		}
	}
}

func (p *managed) trackReadiness(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.readySpec == nil || p.readyAt != nil {
		return
	}
	if p.readyPattern != nil {
		p.readyBuffer += string(data)
		if len(p.readyBuffer) > 64*1024 {
			p.readyBuffer = p.readyBuffer[len(p.readyBuffer)-64*1024:]
		}
		if match := p.readyPattern.FindString(p.readyBuffer); match != "" {
			p.readyLog = true
			p.readyMatch = match
			p.removeReadyPending("log")
		}
	}
	if p.readySpec.Port > 0 && !p.readyPort {
		host := p.readySpec.Host
		if host == "" {
			host = "127.0.0.1"
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, p.readySpec.Port), 10*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			p.readyPort = true
			p.removeReadyPending("port")
		}
	}
	if len(p.readyPending) == 0 {
		now := time.Now()
		p.readyAt = &now
		select {
		case <-p.readyChanged:
		default:
			close(p.readyChanged)
		}
	}
}

func (p *managed) removeReadyPending(condition string) {
	for index, pending := range p.readyPending {
		if pending == condition {
			p.readyPending = append(p.readyPending[:index], p.readyPending[index+1:]...)
			return
		}
	}
}

func (p *managed) maybeRestart(code int) {
	p.mu.Lock()
	policy := p.restartPolicy
	stopRequested := p.stopRequested
	spec := cloneStartSpec(p.startSpec)
	if policy == "no" || stopRequested || (policy == "on-failure" && code == 0) || p.manager == nil {
		p.mu.Unlock()
		return
	}
	p.restartCount++
	p.mu.Unlock()
	// Restart is intentionally bounded to one replacement at a time. The old
	// process remains queryable and its cursor history intact; the manager
	// starts a new handle under the same logical tag.
	if _, err := p.manager.Start(spec); err != nil {
		return
	}
}

func (p *managed) wait() {
	err := p.cmd.Wait()
	if p.pty == nil {
		p.streamsDone.Wait()
	}
	code := -1
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	if err != nil && p.cmd.ProcessState == nil {
		code = -1
	}
	now := time.Now()
	p.mu.Lock()
	p.exitCode = &code
	p.exitedAt = &now
	if p.stdin != nil {
		_ = p.stdin.Close()
		p.stdin = nil
	}
	p.mu.Unlock()
	p.maybeRestart(code)
	if p.pty != nil {
		_ = p.pty.Close()
	}
	p.doneOnce.Do(func() { close(p.done) })
}

func (p *managed) signal(signal syscall.Signal) {
	p.mu.RLock()
	exited := p.exitedAt != nil
	p.mu.RUnlock()
	if exited || p.cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, signal); err != nil {
		_ = p.cmd.Process.Signal(signal)
	}
}

func (p *managed) broadcast(data []byte) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for subscriber := range p.subscribers {
		select {
		case subscriber <- data:
		default:
		}
	}
}

func (p *managed) broadcastEvent(event Event) {
	event = cloneEvent(event)
	if event.Cursor == 0 {
		event.Cursor = p.nextCursor.Add(1)
	}
	p.mu.Lock()
	if len(p.history) >= 256 {
		copy(p.history, p.history[1:])
		p.history = p.history[:255]
	}
	p.history = append(p.history, event)
	for subscriber := range p.eventSubscribers {
		select {
		case subscriber <- cloneEvent(event):
		default:
		}
	}
	p.mu.Unlock()
}

func cloneEvent(event Event) Event {
	event.Data = append([]byte(nil), event.Data...)
	return event
}

func (p *managed) info() Info {
	p.mu.RLock()
	defer p.mu.RUnlock()
	status := "running"
	var exitedAt *int64
	var readyAt *int64
	if p.exitedAt != nil {
		status = "exited"
		value := p.exitedAt.UnixMilli()
		exitedAt = &value
	}
	if p.readyAt != nil {
		value := p.readyAt.UnixMilli()
		readyAt = &value
	}
	readyPending := append([]string(nil), p.readyPending...)
	pid := 0
	if p.cmd.Process != nil {
		pid = p.cmd.Process.Pid
	}
	return Info{
		ID:           p.id,
		Command:      p.command,
		Args:         append([]string(nil), p.args...),
		Cwd:          p.cwd,
		Env:          cloneEnv(p.env),
		TTY:          p.tty,
		Interactive:  p.interactive,
		Tag:          p.tag,
		Status:       status,
		PID:          pid,
		ExitCode:     p.exitCode,
		CreatedAtMS:  p.createdAt.UnixMilli(),
		ExitedAtMS:   exitedAt,
		ReadyAtMS:    readyAt,
		ReadyMatch:   p.readyMatch,
		ReadyPending: readyPending,
		Restart:      p.restartPolicy,
		RestartCount: p.restartCount,
		Persist:      p.persist,
		Detached:     p.detached,
	}
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	result := make(map[string]string, len(env))
	for key, value := range env {
		result[key] = value
	}
	return result
}

func cloneStartSpec(spec StartSpec) StartSpec {
	spec.Args = append([]string(nil), spec.Args...)
	spec.Env = cloneEnv(spec.Env)
	return spec
}

func command(ctx context.Context, executable string, args []string, cwd string, env map[string]string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	return cmd
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.limit <= b.buffer.Len() {
		b.truncated = true
		return len(data), nil
	}
	remaining := b.limit - b.buffer.Len()
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	_, _ = b.buffer.Write(data)
	return len(data), nil
}

func (b *boundedBuffer) String() string {
	return b.buffer.String()
}
