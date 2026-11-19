package envd

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	processv1 "github.com/iwrs712/litterbox-agent/internal/envd/proto/process"
	processv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/process/processv1connect"
	"github.com/iwrs712/litterbox-agent/internal/fsapi"
	"github.com/iwrs712/litterbox-agent/internal/processes"
)

func TestProcessServiceStreamsE2BStartEvents(t *testing.T) {
	filesystem, err := fsapi.NewWithPublicRoot(t.TempDir(), "/")
	if err != nil {
		t.Fatal(err)
	}
	path, handler := processv1connect.NewProcessHandler(NewProcessService(processes.NewManager(), filesystem))
	server := httptest.NewServer(httpPathHandler(path, handler))
	defer server.Close()
	client := processv1connect.NewProcessClient(server.Client(), server.URL)

	stream, err := client.Start(context.Background(), connect.NewRequest(&processv1.StartRequest{
		Process: &processv1.ProcessConfig{Cmd: "sh", Args: []string{"-c", "printf out; printf err >&2"}},
		Tag:     stringPtr("demo"),
		Stdin:   boolPtr(false),
	}))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr string
	var sawStart, sawEnd bool
	for stream.Receive() {
		event := stream.Msg().Event
		switch value := event.Event.(type) {
		case *processv1.ProcessEvent_Start:
			sawStart = value.Start.Pid != 0
		case *processv1.ProcessEvent_Data:
			switch output := value.Data.Output.(type) {
			case *processv1.ProcessEvent_DataEvent_Stdout:
				stdout += string(output.Stdout)
			case *processv1.ProcessEvent_DataEvent_Stderr:
				stderr += string(output.Stderr)
			}
		case *processv1.ProcessEvent_End:
			sawEnd = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if !sawStart || !sawEnd || stdout != "out" || stderr != "err" {
		t.Fatalf("unexpected process stream: start=%v end=%v stdout=%q stderr=%q", sawStart, sawEnd, stdout, stderr)
	}
}

func TestProcessServiceAcceptsStreamInputAndList(t *testing.T) {
	filesystem, err := fsapi.NewWithPublicRoot(t.TempDir(), "/")
	if err != nil {
		t.Fatal(err)
	}
	manager := processes.NewManager()
	path, handler := processv1connect.NewProcessHandler(NewProcessService(manager, filesystem))
	server := httptest.NewServer(httpPathHandler(path, handler))
	defer server.Close()
	client := processv1connect.NewProcessClient(server.Client(), server.URL)

	start, err := client.Start(context.Background(), connect.NewRequest(&processv1.StartRequest{
		Process: &processv1.ProcessConfig{Cmd: "cat"},
		Stdin:   boolPtr(true),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !start.Receive() {
		t.Fatalf("missing start event: %v", start.Err())
	}
	pid := start.Msg().Event.GetStart().Pid
	input := client.StreamInput(context.Background())
	if err := input.Send(&processv1.StreamInputRequest{Event: &processv1.StreamInputRequest_Start{Start: &processv1.StreamInputRequest_StartEvent{Process: &processv1.ProcessSelector{Selector: &processv1.ProcessSelector_Pid{Pid: pid}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := input.Send(&processv1.StreamInputRequest{Event: &processv1.StreamInputRequest_Data{Data: &processv1.StreamInputRequest_DataEvent{Input: &processv1.ProcessInput{Input: &processv1.ProcessInput_Stdin{Stdin: []byte("hello\n")}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := input.CloseAndReceive(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CloseStdin(context.Background(), connect.NewRequest(&processv1.CloseStdinRequest{Process: &processv1.ProcessSelector{Selector: &processv1.ProcessSelector_Pid{Pid: pid}}})); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		list, err := client.List(ctx, connect.NewRequest(&processv1.ListRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Msg.Processes) == 1 && list.Msg.Processes[0].Pid == pid {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("process did not appear in list")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }
