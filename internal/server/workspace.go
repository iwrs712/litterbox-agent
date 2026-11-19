// Package server — the sandbox's one piece of durable state.
//
// A sandbox's `/workspace` is a working copy of what object storage holds. The
// copy is authoritative only while the process that owns it is running: a new
// envd process means a workspace that has not been restored yet, whether the
// volume is brand new or merely belonged to a previous container.
//
// So the process starts closed. Every route that can read or write the
// workspace answers 503 `restoring_workspace` until `/v1/workspace/restore` has
// copied the tenant's files back. The gate is a wrapper around the whole mux
// rather than a check inside each handler, and the exemptions are listed by
// name: a route added later is closed by default, which is the direction that
// fails safe.
//
// The one exemption that is not about liveness is removing a directory. It does
// not depend on what the directory holds, and refusing it would leave a deleted
// conversation's files behind forever.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// rcloneBinary is installed by the sandbox image. A variable so a test can
// substitute a script: the argument construction is the part worth pinning, and
// it needs no real object storage to check.
var rcloneBinary = "rclone"

const (

	// workspaceRoot is where a restore lands and where a sync reads from. Fixed
	// here, never taken from a request: it is the one directory whose contents
	// this process vouches for.
	workspaceRoot = "/workspace"

	restoreDefaultTimeout = 300 * time.Second
	restoreMaxTimeout     = 900 * time.Second
	syncDefaultTimeout    = 300 * time.Second
	syncMaxTimeout        = 600 * time.Second

	// outputLimit bounds what is echoed back for the backend's log.
	outputLimit = 2000
	bodyLimit   = 1 << 16
)

var (
	// The tenant prefix a restore reads. The key layout mirrors /workspace
	// exactly, which is what makes a restore a single copy.
	tenantPrefixPattern = regexp.MustCompile(
		`^workspaces/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/$`)

	// A project's own workspace: the tenant prefix plus the one directory name it
	// owns, which is the project id. A project owns exactly one direct
	// /workspace/proj-* child, and the two spellings have to name the same thing.
	conversationPrefixPattern = regexp.MustCompile(
		`^workspaces/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/proj-[0-9a-z]{16}/$`)

	libraryWorkspacePattern = regexp.MustCompile(`^/workspace/proj-[0-9a-z]{16}$`)
	libraryBucketPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

// workspaceExclusions are what never leaves the sandbox: both are rebuilt from
// the image, and a coding workspace's node_modules alone is most of its size.
//
// Everything else is mirrored, including .git and dot paths. A restore that
// dropped them would lose the version history the turn snapshots are built on,
// and the environment a running app needs.
var workspaceExclusions = []string{
	"/node_modules/**", "**/node_modules/**",
	"/.pnpm-store/**",
}

type workspace struct {
	mu       sync.Mutex
	restored bool
}

func (w *workspace) ready() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.restored
}

// gate closes every route that can touch the workspace until a restore finished.
func (w *workspace) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if w.ready() || gateExempt(r.Method, r.URL.Path) {
			next.ServeHTTP(rw, r)
			return
		}
		writeJSON(rw, http.StatusServiceUnavailable, map[string]string{
			"code":    "restoring_workspace",
			"message": "the sandbox workspace has not been restored yet",
		})
	})
}

// gateExempt names the routes that work before a workspace exists.
//
// Everything here either reports liveness, performs the restore, or does not
// read the workspace at all (skill installation writes to /workspace/.agents,
// which no restore touches).
func gateExempt(method, urlPath string) bool {
	switch {
	case method == http.MethodGet && (urlPath == "/health" || urlPath == "/"):
		return true
	case method == http.MethodPost && urlPath == "/v1/workspace/restore":
		return true
	case strings.HasPrefix(urlPath, "/v1/skills/"):
		return true
	case strings.HasPrefix(urlPath, "/filesystem.Filesystem/") && strings.HasSuffix(urlPath, "/Remove"):
		return true
	}
	return false
}

type storageCredentials struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token"`
}

type storageTarget struct {
	Bucket      string             `json:"bucket"`
	Endpoint    string             `json:"endpoint"`
	Region      string             `json:"region"`
	Credentials storageCredentials `json:"credentials"`
	TimeoutSecs float64            `json:"timeout_seconds"`
	Prefix      string             `json:"prefix"`
}

func (t storageTarget) validate(prefixPattern *regexp.Regexp) error {
	switch {
	case !prefixPattern.MatchString(t.Prefix):
		return errors.New("prefix does not name a tenant workspace prefix this sandbox may use")
	case !libraryBucketPattern.MatchString(t.Bucket):
		return errors.New("bucket is not a valid name")
	case !strings.HasPrefix(t.Endpoint, "https://") && !strings.HasPrefix(t.Endpoint, "http://"):
		return errors.New("endpoint must be an http(s) URL")
	case t.Credentials.AccessKey == "" || t.Credentials.SecretKey == "" || t.Credentials.SessionToken == "":
		return errors.New("credentials are required")
	}
	return nil
}

func (t storageTarget) timeout(fallback, limit time.Duration) time.Duration {
	if t.TimeoutSecs <= 0 {
		return fallback
	}
	value := time.Duration(t.TimeoutSecs * float64(time.Second))
	if value > limit {
		return limit
	}
	return value
}

type workspaceResult struct {
	ExitCode  int    `json:"exit_code"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Output    string `json:"output,omitempty"`
}

// restore copies the tenant's files into /workspace, once.
//
// `--ignore-existing` is what makes this safe to call on every wake-up: a
// workspace that is already there is the newer copy and is left alone, and a
// fresh one is empty, where "skip what exists" and "copy everything" are the
// same instruction.
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		storageTarget
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, bodyLimit)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// A deployment with no object storage (local development) has no durable
	// copy: the volume is the workspace. It still opens the gate through this
	// one call, so "restored" has a single meaning — the backend said this
	// workspace may be used — instead of a second path that skips the gate.
	local := body.Prefix == "" && body.Bucket == ""
	if !local {
		if err := body.validate(tenantPrefixPattern); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	s.workspace.mu.Lock()
	defer s.workspace.mu.Unlock()
	if s.workspace.restored {
		// A second caller waits for the first and finds the work done. That is
		// the whole concurrency story: no lock in the backend, no flag there to
		// go stale.
		writeJSON(w, http.StatusOK, map[string]any{"restored": true, "result": workspaceResult{ExitCode: 0}})
		return
	}

	if local {
		s.workspace.restored = true
		writeJSON(w, http.StatusOK, map[string]any{"restored": true, "result": workspaceResult{ExitCode: 0, Output: "no durable copy configured"}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), body.timeout(restoreDefaultTimeout, restoreMaxTimeout))
	defer cancel()

	result := runRclone(ctx, restoreArgs(body.storageTarget))
	if result.ExitCode != 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"restored": false, "result": result})
		return
	}
	s.workspace.restored = true
	writeJSON(w, http.StatusOK, map[string]any{"restored": true, "result": result})
}

func restoreArgs(target storageTarget) []string {
	// No --create-empty-src-dirs: object storage has no directories, and the
	// flag would only create the tenant root locally.
	//
	// Only project directories are restored. The tenant prefix is the sandbox's
	// /workspace by definition, and a project's directory is the only thing in
	// /workspace that is a project's: anything else under the prefix is not a
	// workspace and has no business becoming one.
	args := []string{
		"copy", remotePath(target), workspaceRoot,
		"--include", "/proj-*/**",
		"--ignore-existing",
		"--links",
		"--metadata",
	}
	return append(args, storageFlags(target)...)
}

// sync makes the bucket hold exactly what the conversation's workspace holds,
// deletions included. A deleted file that stayed in the bucket would come back
// on the next restore.
func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Root string `json:"root"`
		storageTarget
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, bodyLimit)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := body.validate(conversationPrefixPattern); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !libraryWorkspacePattern.MatchString(body.Root) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "root must be a managed /workspace/proj-* directory"})
		return
	}
	// The root and the prefix have to name the same directory. Without this a
	// caller could sync one conversation's files into another's prefix, or into
	// the tenant root where a restore would read them as whole workspaces.
	if path.Base(strings.TrimSuffix(body.Prefix, "/")) != filepath.Base(body.Root) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prefix does not name the workspace being synced"})
		return
	}
	// A restore that has not happened yet means this directory is not the
	// conversation's content; syncing it would make the bucket match a partial
	// workspace.
	if !s.workspace.ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"code":    "restoring_workspace",
			"message": "the sandbox workspace has not been restored yet",
		})
		return
	}
	if info, err := os.Stat(body.Root); err != nil || !info.IsDir() {
		writeJSON(w, http.StatusOK, workspaceResult{ExitCode: 0, Output: "workspace is absent"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), body.timeout(syncDefaultTimeout, syncMaxTimeout))
	defer cancel()
	writeJSON(w, http.StatusOK, runRclone(ctx, syncArgs(body.Root, body.storageTarget)))
}

func syncArgs(root string, target storageTarget) []string {
	args := []string{
		"sync", root, remotePath(target),
		"--links",
		"--metadata",
	}
	for _, pattern := range workspaceExclusions {
		args = append(args, "--exclude", pattern)
	}
	return append(args, storageFlags(target)...)
}

func storageFlags(target storageTarget) []string {
	region := target.Region
	if region == "" {
		region = "us-east-1"
	}
	return []string{
		"--s3-provider", "Other",
		"--s3-endpoint", target.Endpoint,
		"--s3-region", region,
		"--s3-force-path-style",
		// The bucket is created once by the backend. rclone otherwise tries to
		// create it when a listing fails, which the session policy refuses.
		"--s3-no-check-bucket",
		"--s3-access-key-id", target.Credentials.AccessKey,
		"--s3-secret-access-key", target.Credentials.SecretKey,
		"--s3-session-token", target.Credentials.SessionToken,
		// The final summary only; per-file INFO would be thousands of lines.
		"--stats", "0",
		"--stats-one-line",
	}
}

func remotePath(target storageTarget) string {
	return ":s3:" + target.Bucket + "/" + target.Prefix
}

func runRclone(ctx context.Context, args []string) workspaceResult {
	started := time.Now()
	command := exec.CommandContext(ctx, rcloneBinary, args...)
	// rclone inherits nothing: what this process holds is not its business, and
	// an inherited environment is how a sandbox ends up with credentials it was
	// never given.
	command.Env = []string{"PATH=" + libraryPath(), "HOME=/tmp", "TMPDIR=/tmp"}

	output, err := command.CombinedOutput()
	result := workspaceResult{
		ElapsedMS: time.Since(started).Milliseconds(),
		Output:    outputTail(string(output)),
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
		} else {
			// The binary could not be started at all: a deployment fault, told
			// apart from "rclone ran and failed".
			result.ExitCode = -1
			result.Output = strings.TrimSpace(result.Output + " " + err.Error())
		}
	}
	return result
}

func libraryPath() string {
	if value := os.Getenv("PATH"); value != "" {
		return value
	}
	return "/usr/local/bin:/usr/bin:/bin"
}

func outputTail(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= outputLimit {
		return trimmed
	}
	return trimmed[len(trimmed)-outputLimit:]
}
