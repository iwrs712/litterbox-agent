package service

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"litterbox-agent/internal/model"
)

type ExecService struct{}

func NewExecService() *ExecService {
	return &ExecService{}
}

// ExecuteCommand executes a shell command and returns the result
func (s *ExecService) ExecuteCommand(req *model.CommandRequest) *model.CommandResponse {
	cmd := exec.Command("sh", "-c", req.Command)

	// 设置工作目录
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	} else {
		// 如果没有指定工作目录，检查 DEFAULT_DIR 环境变量
		defaultDir := os.Getenv("DEFAULT_DIR")
		if defaultDir != "" {
			cmd.Dir = defaultDir
		}
	}

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
