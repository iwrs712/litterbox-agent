package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwrs712/litterbox-agent/internal/server"
)

// newTestServer starts an envd daemon whose workspace is already restored.
//
// The restore gate itself is exercised in workspace_test.go. Everything else is
// about what happens once the workspace is there, so these tests pay one fake
// restore — a shim on PATH, never a real object store — and then talk to the
// server normally.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return restoreTestWorkspace(t, httptest.NewServer(server.New(server.Config{WorkspaceRoot: t.TempDir()})))
}

// restoreTestWorkspace opens the gate on a freshly built server, which starts
// closed by design. The shim also gives a test somewhere to assert rclone's
// arguments if it wants to.
func restoreTestWorkspace(t *testing.T, ts *httptest.Server) *httptest.Server {
	t.Helper()
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "rclone"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	restore := `{"bucket":"runlet-library","prefix":"workspaces/2b9c8f0e-1a2b-4c3d-8e4f-5a6b7c8d9e0f/",` +
		`"endpoint":"https://rustfs.runlet.cn",` +
		`"credentials":{"access_key":"a","secret_key":"b","session_token":"c"}}`
	response, err := http.Post(ts.URL+"/v1/workspace/restore", "application/json", strings.NewReader(restore))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("test server could not restore its workspace: %d", response.StatusCode)
	}
	return ts
}

func request(t *testing.T, client *http.Client, method, rawURL string, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestRootDescribesEnvd(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	response := request(t, ts.Client(), http.MethodGet, ts.URL+"/", nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "envd" {
		t.Fatalf("name = %q", body["name"])
	}
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	response := request(t, ts.Client(), http.MethodGet, ts.URL+"/health", nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["service"] != "envd" {
		t.Fatalf("health = %#v", body)
	}
}

func TestLegacyFilesystemAndProcessRoutesAreRemoved(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	for _, endpoint := range []struct {
		method string
		path   string
		body   io.Reader
	}{
		{method: http.MethodGet, path: "/v1/fs/entries?path=/workspace"},
		{method: http.MethodGet, path: "/v1/fs/file?path=/workspace/example.txt"},
		{method: http.MethodPost, path: "/v1/processes/run", body: strings.NewReader(`{"command":"true"}`)},
		{method: http.MethodGet, path: "/v1/processes/1"},
	} {
		response := request(t, ts.Client(), endpoint.method, ts.URL+endpoint.path, endpoint.body, map[string]string{"Content-Type": "application/json"})
		if response.StatusCode != http.StatusNotFound {
			body, _ := io.ReadAll(response.Body)
			t.Errorf("%s %s status = %d: %s", endpoint.method, endpoint.path, response.StatusCode, body)
		}
	}
}
