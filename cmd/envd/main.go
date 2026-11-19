package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/iwrs712/litterbox-agent/internal/server"
)

const (
	defaultHost           = "0.0.0.0"
	defaultPort           = 49983
	defaultFilesystemRoot = "/"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: envd server|daemon [options]")
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "server":
		err = runServer(args[1:])
	case "daemon":
		err = runDaemon(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "usage: envd server|daemon [options]")
		os.Exit(2)
	}
	if err != nil {
		log.Printf("envd stopped: %v", err)
		os.Exit(1)
	}
}

func runDaemon(args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := envOr("ENVD_DAEMON_LOG", "/tmp/envd.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	command := newDaemonCommand(executable, args, logFile)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func newDaemonCommand(executable string, args []string, output io.Writer) *exec.Cmd {
	commandArgs := append([]string{"server"}, args...)
	command := exec.Command(executable, commandArgs...)
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return command
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("envd server", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	host := flags.String("host", defaultHost, "listen host")
	port := flags.Int("port", defaultPort, "listen port")
	filesystemRoot := flags.String("filesystem-root", os.Getenv("ENVD_FILESYSTEM_ROOT"), "filesystem root exposed by envd (default: /)")
	workspaceRoot := flags.String("workspace-root", os.Getenv("ENVD_WORKSPACE_ROOT"), "workspace-only compatibility mode; ignored when filesystem-root is set")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	serverConfig := server.Config{}
	if *filesystemRoot != "" {
		serverConfig.FilesystemRoot = *filesystemRoot
		serverConfig.FilesystemPublicRoot = "/"
	} else if *workspaceRoot != "" {
		serverConfig.WorkspaceRoot = *workspaceRoot
	} else {
		serverConfig.FilesystemRoot = defaultFilesystemRoot
		serverConfig.FilesystemPublicRoot = "/"
	}
	handler := server.New(serverConfig)
	defer handler.Close()
	httpServer := &http.Server{
		Addr:              *host + ":" + strconv.Itoa(*port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errorsCh := make(chan error, 1)
	go func() {
		log.Printf("envd listening on %s with filesystem root %s", httpServer.Addr, filesystemRootForLog(serverConfig))
		errorsCh <- httpServer.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals:
		handler.Close()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownContext)
	}
}

func filesystemRootForLog(config server.Config) string {
	if config.FilesystemRoot != "" {
		return config.FilesystemRoot
	}
	return config.WorkspaceRoot
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
