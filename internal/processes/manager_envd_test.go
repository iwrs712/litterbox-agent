package processes

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestManagerPublishesSeparateStdoutAndStderrForPIDAndTag(t *testing.T) {
	manager := NewManager()
	info, err := manager.Start(StartSpec{
		Command: "sh",
		Args:    []string{"-c", "printf out; printf err >&2"},
		Tag:     "tagged",
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.PID == 0 || info.Tag != "tagged" {
		t.Fatalf("expected real pid and tag, got %#v", info)
	}

	updates, unsubscribe, done, err := manager.SubscribeEvents(Selector{PID: info.PID})
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if _, err := manager.GetBySelector(Selector{Tag: "tagged"}); err != nil {
		t.Fatalf("tag selector failed: %v", err)
	}

	events := collectEvents(updates, done)
	var stdout, stderr string
	for _, event := range events {
		switch event.Kind {
		case OutputStdout:
			stdout += string(event.Data)
		case OutputStderr:
			stderr += string(event.Data)
		}
	}
	if stdout != "out" || stderr != "err" {
		t.Fatalf("unexpected output events: stdout=%q stderr=%q events=%#v", stdout, stderr, events)
	}
}

func TestManagerAtomicallyRejectsConcurrentDuplicateTags(t *testing.T) {
	manager := NewManager()
	const attempts = 8
	ready := make(chan struct{})
	results := make(chan error, attempts)
	var group sync.WaitGroup
	for range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			<-ready
			_, err := manager.Start(StartSpec{Command: "sh", Args: []string{"-c", "sleep 10"}, Tag: "same"})
			results <- err
		}()
	}
	close(ready)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if err != ErrTagConflict {
			t.Fatalf("unexpected start error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful starts = %d, want 1", successes)
	}
	info, err := manager.GetBySelector(Selector{Tag: "same"})
	if err != nil {
		t.Fatal(err)
	}
	_ = manager.SendSignal(Selector{PID: info.PID}, syscall.SIGKILL)
}

func TestManagerReadEventsReportsCursorBehindReplayFloor(t *testing.T) {
	manager := NewManager()
	process := &managed{
		id:               "p_history",
		done:             make(chan struct{}),
		subscribers:      make(map[chan []byte]struct{}),
		eventSubscribers: make(map[chan Event]struct{}),
	}
	manager.processes[process.id] = process
	for index := 0; index < 300; index++ {
		process.broadcastEvent(Event{Kind: OutputStdout, Data: []byte("x")})
	}
	process.cmd = &exec.Cmd{Process: &os.Process{Pid: 999999}}
	events, next, truncated, err := manager.ReadEvents(Selector{PID: 999999}, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(events) != 10 || events[0].Cursor <= 1 || next != events[len(events)-1].Cursor {
		t.Fatalf("events=%#v next=%d truncated=%v", events, next, truncated)
	}
}

func TestManagerAcceptsAndClosesNonPTYStdin(t *testing.T) {
	manager := NewManager()
	info, err := manager.Start(StartSpec{
		Command: "cat",
		Stdin:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	updates, unsubscribe, done, err := manager.SubscribeEvents(Selector{PID: info.PID})
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if err := manager.SendInput(Selector{PID: info.PID}, []byte("hello\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := manager.CloseStdin(Selector{PID: info.PID}); err != nil {
		t.Fatal(err)
	}
	var stdout string
	for _, event := range collectEvents(updates, done) {
		if event.Kind == OutputStdout {
			stdout += string(event.Data)
		}
	}
	if stdout != "hello\n" {
		t.Fatalf("unexpected stdin echo: %q", stdout)
	}
}

func TestManagerAppliesInitialPTYSize(t *testing.T) {
	manager := NewManager()
	info, err := manager.Start(StartSpec{
		Command: "sh",
		Args:    []string{"-c", "stty size"},
		TTY:     true,
		Cols:    91,
		Rows:    37,
	})
	if err != nil {
		t.Fatal(err)
	}
	updates, unsubscribe, done, err := manager.SubscribeEvents(Selector{PID: info.PID})
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	var output string
	for _, event := range collectEvents(updates, done) {
		if event.Kind == OutputPTY {
			output += string(event.Data)
		}
	}
	if !strings.Contains(output, "37 91") {
		t.Fatalf("expected PTY size 37x91, got %q", output)
	}
}

func TestManagerSendsSignalThroughSelector(t *testing.T) {
	manager := NewManager()
	info, err := manager.Start(StartSpec{Command: "sh", Args: []string{"-c", "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SendSignal(Selector{Tag: info.Tag, PID: info.PID}, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-manager.Done(Selector{PID: info.PID}):
	case <-time.After(2 * time.Second):
		t.Fatal("process did not exit after SIGTERM")
	}
}

func collectEvents(updates <-chan Event, done <-chan struct{}) []Event {
	events := make([]Event, 0)
	for {
		select {
		case event := <-updates:
			events = append(events, event)
		case <-done:
			for {
				select {
				case event := <-updates:
					events = append(events, event)
				default:
					return events
				}
			}
		}
	}
}

func TestManagerListIncludesStartedProcess(t *testing.T) {
	manager := NewManager()
	info, err := manager.Start(StartSpec{Command: "sh", Args: []string{"-c", "sleep 1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.SendSignal(Selector{PID: info.PID}, syscall.SIGKILL) }()
	listed := manager.List()
	if len(listed) != 1 || !strings.Contains(listed[0].Command, "sh") {
		t.Fatalf("unexpected list: %#v", listed)
	}
}
