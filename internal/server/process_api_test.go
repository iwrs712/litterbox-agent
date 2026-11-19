package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwrs712/litterbox-agent/internal/server"
)

func TestProcessRESTSupportsTaggedLifecycleAndCursorLogs(t *testing.T) {
	root := t.TempDir()
	ts := newServerWithRoot(t, root)
	defer ts.Close()

	start := request(t, ts.Client(), http.MethodPost, ts.URL+"/v1/processes", strings.NewReader(`{"application":"sh","args":["-c","printf 'hello\\n'; while IFS= read -r line; do printf 'got:%s\\n' \"$line\"; done"],"tag":"web","cwd":"/workspace","stdin":true}`), map[string]string{"Content-Type": "application/json"})
	if start.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(start.Body)
		t.Fatalf("start status = %d: %s", start.StatusCode, body)
	}
	var process map[string]any
	if err := json.NewDecoder(start.Body).Decode(&process); err != nil {
		t.Fatal(err)
	}
	id, ok := process["id"].(string)
	if !ok || id == "" || process["tag"] != "web" {
		t.Fatalf("start response = %#v", process)
	}

	list := request(t, ts.Client(), http.MethodGet, ts.URL+"/v1/processes?tag=web", nil, nil)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", list.StatusCode)
	}
	var listed map[string]any
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed["processes"].([]any)) != 1 {
		t.Fatalf("listed = %#v", listed)
	}

	// Keep the REST surface reachable for clients/proxies that normalize the
	// collection URL with a trailing slash.
	trailingList := request(t, ts.Client(), http.MethodGet, ts.URL+"/v1/processes/", nil, nil)
	if trailingList.StatusCode != http.StatusOK {
		t.Fatalf("trailing list status = %d", trailingList.StatusCode)
	}

	logs := request(t, ts.Client(), http.MethodGet, ts.URL+"/v1/processes/"+url.PathEscape(id)+"/logs?limit=10&follow=true&timeout_ms=1000", nil, nil)
	if logs.StatusCode != http.StatusOK {
		t.Fatalf("logs status = %d", logs.StatusCode)
	}
	var snapshot map[string]any
	if err := json.NewDecoder(logs.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	events := snapshot["events"].([]any)
	if len(events) == 0 {
		t.Fatalf("expected output events: %#v", snapshot)
	}
	first := events[0].(map[string]any)
	cursor := first["cursor"].(float64)
	if !strings.Contains(first["data"].(string), "hello") {
		t.Fatalf("first event = %#v", first)
	}

	input := request(t, ts.Client(), http.MethodPost, ts.URL+"/v1/processes/"+url.PathEscape(id)+"/input", strings.NewReader(`{"data":"world\n"}`), map[string]string{"Content-Type": "application/json"})
	if input.StatusCode != http.StatusAccepted {
		t.Fatalf("input status = %d", input.StatusCode)
	}

	follow := request(t, ts.Client(), http.MethodGet, ts.URL+"/v1/processes/"+url.PathEscape(id)+"/logs?cursor="+formatFloat(cursor)+"&follow=true&timeout_ms=1000", nil, nil)
	if follow.StatusCode != http.StatusOK {
		t.Fatalf("follow status = %d", follow.StatusCode)
	}
	var followed map[string]any
	if err := json.NewDecoder(follow.Body).Decode(&followed); err != nil {
		t.Fatal(err)
	}
	followedEvents := followed["events"].([]any)
	if len(followedEvents) == 0 || !strings.Contains(followedEvents[0].(map[string]any)["data"].(string), "got:world") {
		t.Fatalf("followed = %#v", followed)
	}

	stop := request(t, ts.Client(), http.MethodPost, ts.URL+"/v1/processes/"+url.PathEscape(id)+"/stop", strings.NewReader(`{"wait_ms":1000}`), map[string]string{"Content-Type": "application/json"})
	if stop.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d", stop.StatusCode)
	}
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("process did not stop")
	default:
	}
}

func TestProcessRESTRestartRetainsSpecAndRejectsDuplicateRunningTag(t *testing.T) {
	ts := newServerWithRoot(t, t.TempDir())
	defer ts.Close()
	payload := `{"application":"sh","args":["-c","printf 'started:%s\\n' \"$TOKEN\"; trap 'exit 0' TERM; while :; do sleep 1; done"],"tag":"worker","cwd":"/workspace","env":{"TOKEN":"kept"},"stdin":false}`
	start := request(t, ts.Client(), http.MethodPost, ts.URL+"/v1/processes", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if start.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(start.Body)
		t.Fatalf("start status = %d: %s", start.StatusCode, body)
	}
	var original map[string]any
	if err := json.NewDecoder(start.Body).Decode(&original); err != nil {
		t.Fatal(err)
	}
	originalID := original["id"].(string)
	duplicate := request(t, ts.Client(), http.MethodPost, ts.URL+"/v1/processes", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status = %d", duplicate.StatusCode)
	}
	restart := request(t, ts.Client(), http.MethodPost, ts.URL+"/v1/processes/"+originalID+"/restart", strings.NewReader(`{"wait_ms":2000}`), map[string]string{"Content-Type": "application/json"})
	if restart.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(restart.Body)
		t.Fatalf("restart status = %d: %s", restart.StatusCode, body)
	}
	var restarted map[string]any
	if err := json.NewDecoder(restart.Body).Decode(&restarted); err != nil {
		t.Fatal(err)
	}
	if restarted["id"] == originalID || restarted["tag"] != "worker" || restarted["cwd"] != "/workspace" {
		t.Fatalf("restarted process = %#v", restarted)
	}
	if _, exposed := restarted["env"]; exposed {
		t.Fatalf("process response exposed environment: %#v", restarted)
	}
	logs := request(t, ts.Client(), http.MethodGet, ts.URL+"/v1/processes/"+restarted["id"].(string)+"/logs?follow=true&timeout_ms=1000", nil, nil)
	var logBody map[string]any
	if err := json.NewDecoder(logs.Body).Decode(&logBody); err != nil {
		t.Fatal(err)
	}
	if len(logBody["events"].([]any)) == 0 || !strings.Contains(logBody["events"].([]any)[0].(map[string]any)["data"].(string), "started:kept") {
		t.Fatalf("restart did not retain env/spec: %#v", logBody)
	}
}

func formatFloat(value float64) string {
	return strconv.FormatUint(uint64(value), 10)
}

func newServerWithRoot(t *testing.T, root string) *httptest.Server {
	t.Helper()
	handler := server.New(server.Config{WorkspaceRoot: root})
	return restoreTestWorkspace(t, httptest.NewServer(handler))
}
