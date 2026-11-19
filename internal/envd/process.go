package envd

import (
	"context"
	"errors"
	"syscall"
	"time"

	"connectrpc.com/connect"
	processv1 "github.com/iwrs712/litterbox-agent/internal/envd/proto/process"
	processv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/process/processv1connect"
	"github.com/iwrs712/litterbox-agent/internal/fsapi"
	"github.com/iwrs712/litterbox-agent/internal/processes"
)

type ProcessService struct {
	processv1connect.UnimplementedProcessHandler
	manager    *processes.Manager
	filesystem *fsapi.Filesystem
}

func NewProcessService(manager *processes.Manager, filesystem *fsapi.Filesystem) *ProcessService {
	return &ProcessService{manager: manager, filesystem: filesystem}
}

func (s *ProcessService) List(_ context.Context, _ *connect.Request[processv1.ListRequest]) (*connect.Response[processv1.ListResponse], error) {
	result := &processv1.ListResponse{Processes: make([]*processv1.ProcessInfo, 0)}
	for _, info := range s.manager.List() {
		result.Processes = append(result.Processes, processInfo(info))
	}
	return connect.NewResponse(result), nil
}

func (s *ProcessService) Start(ctx context.Context, request *connect.Request[processv1.StartRequest], stream *connect.ServerStream[processv1.StartResponse]) error {
	if request.Msg.Process == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("process config is required"))
	}
	config := request.Msg.Process
	cwd, err := s.resolveCwd(config.GetCwd())
	if err != nil {
		return err
	}
	stdin := true
	if request.Msg.Stdin != nil {
		stdin = request.Msg.GetStdin()
	}
	var cols, rows uint16
	if request.Msg.Pty != nil && request.Msg.Pty.Size != nil {
		if request.Msg.Pty.Size.Cols > 65535 || request.Msg.Pty.Size.Rows > 65535 {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("pty size exceeds 65535"))
		}
		cols = uint16(request.Msg.Pty.Size.Cols)
		rows = uint16(request.Msg.Pty.Size.Rows)
	}
	info, err := s.manager.Start(processes.StartSpec{
		Command:     config.Cmd,
		Args:        config.Args,
		Cwd:         cwd,
		PublicCwd:   config.GetCwd(),
		Env:         config.Envs,
		Tag:         request.Msg.GetTag(),
		Stdin:       stdin,
		TTY:         request.Msg.Pty != nil,
		Cols:        cols,
		Rows:        rows,
		Interactive: stdin,
	})
	if err != nil {
		return processError(err)
	}
	if err := stream.Send(&processv1.StartResponse{Event: startEvent(info.PID)}); err != nil {
		return err
	}
	updates, unsubscribe, done, err := s.manager.SubscribeEvents(processes.Selector{PID: info.PID})
	if err != nil {
		return processError(err)
	}
	defer unsubscribe()
	return streamEvents(ctx, updates, done, func() processes.Info {
		current, _ := s.manager.Get(info.ID)
		return current
	}, func(event processes.Event) error {
		return stream.Send(&processv1.StartResponse{Event: processEvent(event)})
	})
}

func (s *ProcessService) Connect(ctx context.Context, request *connect.Request[processv1.ConnectRequest], stream *connect.ServerStream[processv1.ConnectResponse]) error {
	selector, err := processSelector(request.Msg.Process)
	if err != nil {
		return err
	}
	info, err := s.manager.GetBySelector(selector)
	if err != nil {
		return processError(err)
	}
	if err := stream.Send(&processv1.ConnectResponse{Event: startEvent(info.PID)}); err != nil {
		return err
	}
	updates, unsubscribe, done, err := s.manager.SubscribeEvents(selector)
	if err != nil {
		return processError(err)
	}
	defer unsubscribe()
	return streamEvents(ctx, updates, done, func() processes.Info {
		current, _ := s.manager.GetBySelector(selector)
		return current
	}, func(event processes.Event) error {
		return stream.Send(&processv1.ConnectResponse{Event: processEvent(event)})
	})
}

func (s *ProcessService) Update(_ context.Context, request *connect.Request[processv1.UpdateRequest]) (*connect.Response[processv1.UpdateResponse], error) {
	selector, err := processSelector(request.Msg.Process)
	if err != nil {
		return nil, err
	}
	if request.Msg.Pty != nil && request.Msg.Pty.Size != nil {
		if request.Msg.Pty.Size.Cols == 0 || request.Msg.Pty.Size.Rows == 0 || request.Msg.Pty.Size.Cols > 65535 || request.Msg.Pty.Size.Rows > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("pty cols and rows must be between 1 and 65535"))
		}
		if err := s.manager.ResizeSelector(selector, uint16(request.Msg.Pty.Size.Cols), uint16(request.Msg.Pty.Size.Rows)); err != nil {
			return nil, processError(err)
		}
	}
	return connect.NewResponse(&processv1.UpdateResponse{}), nil
}

func (s *ProcessService) StreamInput(_ context.Context, stream *connect.ClientStream[processv1.StreamInputRequest]) (*connect.Response[processv1.StreamInputResponse], error) {
	var selector processes.Selector
	for stream.Receive() {
		request := stream.Msg()
		if start := request.GetStart(); start != nil {
			selector, _ = processSelector(start.Process)
			continue
		}
		if data := request.GetData(); data != nil {
			if selector.PID == 0 && selector.Tag == "" {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("stream input must start with a process selector"))
			}
			input := data.Input
			if input == nil {
				continue
			}
			ptyInput := input.GetPty() != nil
			payload := input.GetStdin()
			if ptyInput {
				payload = input.GetPty()
			}
			if err := s.manager.SendInput(selector, payload, ptyInput); err != nil {
				return nil, processError(err)
			}
		}
		if request.GetKeepalive() != nil {
			continue
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return connect.NewResponse(&processv1.StreamInputResponse{}), nil
}

func (s *ProcessService) SendInput(_ context.Context, request *connect.Request[processv1.SendInputRequest]) (*connect.Response[processv1.SendInputResponse], error) {
	selector, err := processSelector(request.Msg.Process)
	if err != nil {
		return nil, err
	}
	if request.Msg.Input == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("input is required"))
	}
	ptyInput := request.Msg.Input.GetPty() != nil
	payload := request.Msg.Input.GetStdin()
	if ptyInput {
		payload = request.Msg.Input.GetPty()
	}
	if err := s.manager.SendInput(selector, payload, ptyInput); err != nil {
		return nil, processError(err)
	}
	return connect.NewResponse(&processv1.SendInputResponse{}), nil
}

func (s *ProcessService) SendSignal(_ context.Context, request *connect.Request[processv1.SendSignalRequest]) (*connect.Response[processv1.SendSignalResponse], error) {
	selector, err := processSelector(request.Msg.Process)
	if err != nil {
		return nil, err
	}
	signal, ok := protocolSignal(request.Msg.Signal)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("signal must be SIGTERM or SIGKILL"))
	}
	if err := s.manager.SendSignal(selector, signal); err != nil {
		return nil, processError(err)
	}
	return connect.NewResponse(&processv1.SendSignalResponse{}), nil
}

func (s *ProcessService) CloseStdin(_ context.Context, request *connect.Request[processv1.CloseStdinRequest]) (*connect.Response[processv1.CloseStdinResponse], error) {
	selector, err := processSelector(request.Msg.Process)
	if err != nil {
		return nil, err
	}
	if err := s.manager.CloseStdin(selector); err != nil {
		return nil, processError(err)
	}
	return connect.NewResponse(&processv1.CloseStdinResponse{}), nil
}

func (s *ProcessService) resolveCwd(cwd string) (string, error) {
	if cwd == "" {
		cwd = s.filesystem.PublicRoot()
	}
	resolved, err := s.filesystem.ResolveDirectory(cwd)
	if err != nil {
		return "", filesystemError(err)
	}
	return resolved, nil
}

func streamEvents(ctx context.Context, updates <-chan processes.Event, done <-chan struct{}, info func() processes.Info, send func(processes.Event) error) error {
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	doneChannel := done
	for {
		select {
		case event := <-updates:
			if err := send(event); err != nil {
				return err
			}
		case <-doneChannel:
			for {
				select {
				case event := <-updates:
					if err := send(event); err != nil {
						return err
					}
				default:
					current := info()
					return send(processes.Event{Kind: processEventEnd, Data: encodeExit(current)})
				}
			}
		case <-keepalive.C:
			if err := send(processes.Event{Kind: processEventKeepAlive}); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

const (
	processEventEnd       processes.OutputKind = 250
	processEventKeepAlive processes.OutputKind = 251
)

func encodeExit(info processes.Info) []byte {
	code := int32(-1)
	if info.ExitCode != nil {
		code = int32(*info.ExitCode)
	}
	return []byte{byte(code), byte(code >> 8), byte(code >> 16), byte(code >> 24)}
}

func processEvent(event processes.Event) *processv1.ProcessEvent {
	switch event.Kind {
	case processes.OutputStdout:
		return &processv1.ProcessEvent{Event: &processv1.ProcessEvent_Data{Data: &processv1.ProcessEvent_DataEvent{Output: &processv1.ProcessEvent_DataEvent_Stdout{Stdout: event.Data}}}}
	case processes.OutputStderr:
		return &processv1.ProcessEvent{Event: &processv1.ProcessEvent_Data{Data: &processv1.ProcessEvent_DataEvent{Output: &processv1.ProcessEvent_DataEvent_Stderr{Stderr: event.Data}}}}
	case processes.OutputPTY:
		return &processv1.ProcessEvent{Event: &processv1.ProcessEvent_Data{Data: &processv1.ProcessEvent_DataEvent{Output: &processv1.ProcessEvent_DataEvent_Pty{Pty: event.Data}}}}
	case processEventEnd:
		code := int32(-1)
		if len(event.Data) == 4 {
			code = int32(event.Data[0]) | int32(event.Data[1])<<8 | int32(event.Data[2])<<16 | int32(event.Data[3])<<24
		}
		return &processv1.ProcessEvent{Event: &processv1.ProcessEvent_End{End: &processv1.ProcessEvent_EndEvent{ExitCode: code, Exited: true, Status: "exited"}}}
	case processEventKeepAlive:
		return &processv1.ProcessEvent{Event: &processv1.ProcessEvent_Keepalive{Keepalive: &processv1.ProcessEvent_KeepAlive{}}}
	default:
		return &processv1.ProcessEvent{}
	}
}

func startEvent(pid int) *processv1.ProcessEvent {
	return &processv1.ProcessEvent{Event: &processv1.ProcessEvent_Start{Start: &processv1.ProcessEvent_StartEvent{Pid: uint32(pid)}}}
}

func processInfo(info processes.Info) *processv1.ProcessInfo {
	config := &processv1.ProcessConfig{Cmd: info.Command, Args: info.Args, Envs: info.Env}
	if info.Cwd != "" {
		config.Cwd = &info.Cwd
	}
	result := &processv1.ProcessInfo{Config: config, Pid: uint32(info.PID)}
	if info.Tag != "" {
		result.Tag = &info.Tag
	}
	return result
}

func processSelector(selector *processv1.ProcessSelector) (processes.Selector, error) {
	if selector == nil {
		return processes.Selector{}, connect.NewError(connect.CodeInvalidArgument, errors.New("process selector is required"))
	}
	result := processes.Selector{}
	if selector.GetPid() != 0 {
		result.PID = int(selector.GetPid())
	} else if selector.GetTag() != "" {
		result.Tag = selector.GetTag()
	} else {
		return processes.Selector{}, connect.NewError(connect.CodeInvalidArgument, errors.New("process selector is empty"))
	}
	return result, nil
}

func protocolSignal(signal processv1.Signal) (syscall.Signal, bool) {
	switch signal {
	case processv1.Signal_SIGNAL_SIGTERM:
		return syscall.SIGTERM, true
	case processv1.Signal_SIGNAL_SIGKILL:
		return syscall.SIGKILL, true
	default:
		return 0, false
	}
}

func processError(err error) error {
	code := connect.CodeInternal
	switch {
	case errors.Is(err, processes.ErrNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, processes.ErrNotTerminal), errors.Is(err, processes.ErrNoStdin):
		code = connect.CodeFailedPrecondition
	case errors.Is(err, processes.ErrRunning):
		code = connect.CodeFailedPrecondition
	}
	return connect.NewError(code, err)
}
