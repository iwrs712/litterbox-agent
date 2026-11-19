package main

import (
	"io"
	"reflect"
	"testing"
)

func TestDaemonCommandStartsDetachedServer(t *testing.T) {
	command := newDaemonCommand("/opt/litterbox/envd", []string{"--port", "49983"}, io.Discard)
	if !reflect.DeepEqual(command.Args, []string{"/opt/litterbox/envd", "server", "--port", "49983"}) {
		t.Fatalf("unexpected daemon args: %#v", command.Args)
	}
	if command.SysProcAttr == nil || !command.SysProcAttr.Setsid {
		t.Fatalf("daemon process must start in a new session: %#v", command.SysProcAttr)
	}
}
