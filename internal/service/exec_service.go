package service

import (
	"bytes"
	"os/exec"
	"strings"

	"litterbox-agent/internal/model"
)

type ExecService struct{}

func NewExecService() *ExecService {
	return &ExecService{}
}

// ExecuteCommand executes a shell command and returns the result
func (s *ExecService) ExecuteCommand(command string) *model.CommandResponse {
	cmd := exec.Command("sh", "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// 捕获特殊异常
			stderr.WriteString(err.Error())
			exitCode = 127
		}
	}

	return &model.CommandResponse{
		Stdout:   strings.TrimRight(stdout.String(), "\n"),
		Stderr:   strings.TrimRight(stderr.String(), "\n"),
		ExitCode: exitCode,
	}
}
