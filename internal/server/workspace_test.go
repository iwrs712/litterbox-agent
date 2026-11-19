package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	tenantPrefix = "workspaces/2b9c8f0e-1a2b-4c3d-8e4f-5a6b7c8d9e0f/"
	appDir       = "proj-k3x9m2q7v1a8c4z0"
	appPrefix    = tenantPrefix + appDir + "/"
)

func workspaceRequest(prefix string) map[string]any {
	return map[string]any{
		"bucket":   "runlet-library",
		"prefix":   prefix,
		"endpoint": "https://rustfs.runlet.cn",
		"credentials": map[string]string{
			"access_key": "AK", "secret_key": "SK", "session_token": "TOKEN",
		},
	}
}

// fakeRclone writes a script that records its arguments and exits with code.
func fakeRclone(t *testing.T, code int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "rclone")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + filepath.Join(dir, "args") + "\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := rcloneBinary
	rcloneBinary = script
	t.Cleanup(func() { rcloneBinary = previous })
	return dir
}

func newWorkspaceServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := New(Config{WorkspaceRoot: t.TempDir()})
	return httptest.NewServer(handler)
}

func post(t *testing.T, url string, payload any) (*http.Response, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	response, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response, decoded
}

func TestAWorkspaceStaysClosedUntilItIsRestored(t *testing.T) {
	ts := newWorkspaceServer(t)
	defer ts.Close()

	health, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	var state map[string]string
	_ = json.NewDecoder(health.Body).Decode(&state)
	if state["workspace"] != "restoring" {
		t.Fatalf("health reports workspace=%q before a restore", state["workspace"])
	}

	// A process call is the shape every workspace read and write takes.
	response, err := http.Post(ts.URL+"/v1/processes", "application/json", strings.NewReader(`{"application":"sh"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("process call answered %d before a restore", response.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body["code"] != "restoring_workspace" {
		t.Fatalf("refusal does not name the reason: %v", body)
	}
}

func TestRestoreOpensTheGate(t *testing.T) {
	fakeRclone(t, 0)
	ts := newWorkspaceServer(t)
	defer ts.Close()

	response, body := post(t, ts.URL+"/v1/workspace/restore", workspaceRequest(tenantPrefix))
	if response.StatusCode != http.StatusOK || body["restored"] != true {
		t.Fatalf("restore answered %d %v", response.StatusCode, body)
	}

	health, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	var state map[string]string
	_ = json.NewDecoder(health.Body).Decode(&state)
	if state["workspace"] != "ready" {
		t.Fatalf("health still reports %q after a restore", state["workspace"])
	}

	// The same call that was refused now reaches the handler.
	response, err = http.Post(ts.URL+"/v1/processes", "application/json", strings.NewReader(`{"application":"sh"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusServiceUnavailable {
		t.Fatal("the gate is still closed after a restore")
	}
}

func TestRestoreRefusesAPrefixThatIsNotATenantWorkspace(t *testing.T) {
	for _, prefix := range []string{
		"",
		"workspaces/",
		"workspaces/not-a-uuid/",
		"library/2b9c8f0e-1a2b-4c3d-8e4f-5a6b7c8d9e0f/",
		tenantPrefix + "proj-abc/",
	} {
		ts := newWorkspaceServer(t)
		response, _ := post(t, ts.URL+"/v1/workspace/restore", workspaceRequest(prefix))
		ts.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("prefix %q answered %d", prefix, response.StatusCode)
		}
	}
}

func TestAFailedRestoreLeavesTheGateClosed(t *testing.T) {
	fakeRclone(t, 3)
	ts := newWorkspaceServer(t)
	defer ts.Close()

	response, body := post(t, ts.URL+"/v1/workspace/restore", workspaceRequest(tenantPrefix))
	if response.StatusCode != http.StatusBadGateway || body["restored"] != false {
		t.Fatalf("restore answered %d %v", response.StatusCode, body)
	}

	response, _ = post(t, ts.URL+"/v1/workspace/sync", map[string]any{"root": "/workspace/" + appDir, "bucket": "runlet-library", "prefix": appPrefix, "endpoint": "https://rustfs.runlet.cn", "credentials": map[string]string{"access_key": "a", "secret_key": "b", "session_token": "c"}})
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a sync was allowed after a failed restore: %d", response.StatusCode)
	}
}

func TestSyncRefusesAPrefixThatNamesAnotherDirectory(t *testing.T) {
	fakeRclone(t, 0)
	ts := newWorkspaceServer(t)
	defer ts.Close()
	post(t, ts.URL+"/v1/workspace/restore", workspaceRequest(tenantPrefix))

	response, _ := post(t, ts.URL+"/v1/workspace/sync", map[string]any{
		"root": "/workspace/" + appDir, "bucket": "runlet-library",
		"prefix": tenantPrefix + "proj-other0000000000/", "endpoint": "https://rustfs.runlet.cn",
		"credentials": map[string]string{"access_key": "a", "secret_key": "b", "session_token": "c"},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("a mismatched prefix answered %d", response.StatusCode)
	}
}

func TestSyncMirrorsDeletionsAndExcludesOnlyRebuildableDirectories(t *testing.T) {
	args := strings.Join(syncArgs("/workspace/"+appDir, storageTarget{Bucket: "b", Endpoint: "https://e", Prefix: appPrefix}), " ")
	for _, want := range []string{
		"sync /workspace/" + appDir + " :s3:b/" + appPrefix,
		"--exclude **/node_modules/**",
		"--exclude /.pnpm-store/**",
		"--links",
		"--metadata",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("sync args are missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, "--ignore-existing") {
		t.Error("a sync that ignores existing files cannot mirror a deletion")
	}
	if strings.Contains(args, "--exclude **/.*") {
		t.Error("dot paths are part of the workspace and must be mirrored")
	}
}

func TestRestoreSkipsFilesTheSandboxAlreadyHas(t *testing.T) {
	args := strings.Join(restoreArgs(storageTarget{Bucket: "b", Endpoint: "https://e", Prefix: tenantPrefix}), " ")
	if !strings.Contains(args, "--ignore-existing") {
		t.Errorf("restore would overwrite a workspace that is newer than the bucket: %s", args)
	}
	if !strings.Contains(args, "--include /proj-*/**") {
		t.Errorf("restore would materialise keys that are not conversation directories: %s", args)
	}
	if strings.Contains(strings.Join(workspaceExclusions, " "), ".git") {
		t.Error(".git is what the turn snapshots are built on and must be mirrored")
	}
}

func TestAWorkspaceWithNoDurableCopyIsOpenedByTheSameCall(t *testing.T) {
	ts := newWorkspaceServer(t)
	defer ts.Close()

	response, body := post(t, ts.URL+"/v1/workspace/restore", map[string]any{})
	if response.StatusCode != http.StatusOK || body["restored"] != true {
		t.Fatalf("an empty restore answered %d %v", response.StatusCode, body)
	}
	response, err := http.Post(ts.URL+"/v1/processes", "application/json", strings.NewReader(`{"application":"sh"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusServiceUnavailable {
		t.Fatal("the gate stayed closed for a workspace with no durable copy")
	}
}
