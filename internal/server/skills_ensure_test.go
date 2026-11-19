package server_test

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iwrs712/litterbox-agent/internal/server"
)

// manifestFile mirrors one entry of the manifest the console writes.
type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// consoleDigest is `console/app/artifacts.py`'s digest for ASCII paths, where
// Go's encoding and Python's `ensure_ascii` agree byte for byte. Non-ASCII
// paths are pinned against digests the console itself computed, below.
func consoleDigest(files []manifestFile) string {
	sorted := append([]manifestFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	parts := make([]string, 0, len(sorted))
	for _, file := range sorted {
		parts = append(parts, fmt.Sprintf(`{"path":%q,"sha256":%q,"size":%d}`, file.Path, file.SHA256, file.Size))
	}
	sum := sha256.Sum256([]byte(`{"files":[` + strings.Join(parts, ",") + `]}`))
	return hex.EncodeToString(sum[:])
}

func describe(files map[string]string) []manifestFile {
	described := make([]manifestFile, 0, len(files))
	for name, body := range files {
		sum := sha256.Sum256([]byte(body))
		described = append(described, manifestFile{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))})
	}
	sort.Slice(described, func(i, j int) bool { return described[i].Path < described[j].Path })
	return described
}

// packTar writes a manifest and files as the console's canonical tar does.
func packTar(manifest any, files map[string]string) []byte {
	encoded, _ := json.Marshal(manifest)
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	write := func(name string, body []byte) {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			panic(err)
		}
		if _, err := writer.Write(body); err != nil {
			panic(err)
		}
	}
	write("manifest.json", encoded)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		write(name, []byte(files[name]))
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return archive.Bytes()
}

// skillArtifact is a valid artifact for these files, with its digest.
func skillArtifact(files map[string]string) (string, []byte) {
	described := describe(files)
	digest := consoleDigest(described)
	return digest, packTar(map[string]any{"digest": digest, "files": described}, files)
}

// skillHost serves one artifact and counts how often it was asked for, so a test
// can assert that a skill already present is not fetched again.
func skillHost(t *testing.T, archive *[]byte) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(*archive)
	}))
	t.Cleanup(remote.Close)
	return remote, &hits
}

type ensureBody struct {
	Items []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		Action string `json:"action"`
		Error  string `json:"error"`
	} `json:"items"`
	Removed     []string `json:"removed"`
	RemoveError string   `json:"remove_error"`
}

func ensure(t *testing.T, base string, client *http.Client, body string) (int, ensureBody) {
	t.Helper()
	response := request(t, client, http.MethodPost, base+"/v1/skills/ensure", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var parsed ensureBody
	_ = json.Unmarshal(raw, &parsed)
	return response.StatusCode, parsed
}

func item(name, url, digest string) string {
	return fmt.Sprintf(`{"name":%q,"url":%q,"digest":%q}`, name, url, digest)
}

func items(entries ...string) string {
	return `{"items":[` + strings.Join(entries, ",") + `]}`
}

func skillServer(t *testing.T) (string, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	ts := httptest.NewServer(server.New(server.Config{FilesystemRoot: root}))
	t.Cleanup(ts.Close)
	return filepath.Join(root, "workspace", ".agents", "skills"), ts
}

func TestEnsureSkillsPullsOnceAndThenLeavesItAlone(t *testing.T) {
	digest, artifact := skillArtifact(map[string]string{"SKILL.md": "# Weather\n"})
	remote, hits := skillHost(t, &artifact)
	skills, ts := skillServer(t)
	body := items(item("weather-query", remote.URL, digest))

	if _, first := ensure(t, ts.URL, ts.Client(), body); first.Items[0].Action != "pulled" {
		t.Fatalf("first ensure = %+v, want pulled", first.Items[0])
	}
	if _, err := os.Stat(filepath.Join(skills, "weather-query", "SKILL.md")); err != nil {
		t.Fatalf("skill was not installed: %v", err)
	}

	// A skill already at its digest costs one local read, not a download.
	code, second := ensure(t, ts.URL, ts.Client(), body)
	if code != http.StatusOK || second.Items[0].Action != "kept" {
		t.Fatalf("second ensure = %d %+v, want 200 kept", code, second.Items[0])
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("artifact was fetched %d times, want 1", got)
	}
}

func TestEnsureSkillsRejectsAnArtifactThatIsNotWhatWasAskedFor(t *testing.T) {
	_, artifact := skillArtifact(map[string]string{"SKILL.md": "# Other\n"})
	remote, _ := skillHost(t, &artifact)
	skills, ts := skillServer(t)

	_, result := ensure(t, ts.URL, ts.Client(), items(item("weather-query", remote.URL, strings.Repeat("d", 64))))
	if result.Items[0].Action != "failed" || !strings.Contains(result.Items[0].Error, "does not match") {
		t.Fatalf("want a digest mismatch, got %+v", result.Items[0])
	}
	if _, err := os.Stat(filepath.Join(skills, "weather-query")); !os.IsNotExist(err) {
		t.Fatal("a rejected artifact left a directory behind")
	}
}

// The manifest's digest is not taken on trust: a file whose bytes differ from
// what the manifest lists is refused, even when the manifest claims the right
// digest and the sizes match.
func TestEnsureSkillsRefusesAFileThatIsNotWhatItsManifestSays(t *testing.T) {
	files := map[string]string{"SKILL.md": "# Weather\n", "scripts/run.sh": "echo hi\n"}
	described := describe(files)
	digest := consoleDigest(described)
	tampered := map[string]string{"SKILL.md": "# Weather\n", "scripts/run.sh": "rm -rf\n"}
	artifact := packTar(map[string]any{"digest": digest, "files": described}, tampered)
	remote, _ := skillHost(t, &artifact)
	_, ts := skillServer(t)

	_, result := ensure(t, ts.URL, ts.Client(), items(item("weather-query", remote.URL, digest)))
	if result.Items[0].Action != "failed" || !strings.Contains(result.Items[0].Error, "scripts/run.sh") {
		t.Fatalf("want the tampered file named, got %+v", result.Items[0])
	}
}

// A manifest that names a digest its own file list does not hash to is refused:
// otherwise any manifest could claim any identity.
func TestEnsureSkillsRefusesAManifestWhoseDigestIsNotItsFiles(t *testing.T) {
	files := map[string]string{"SKILL.md": "# Weather\n"}
	claimed := strings.Repeat("e", 64)
	artifact := packTar(map[string]any{"digest": claimed, "files": describe(files)}, files)
	remote, _ := skillHost(t, &artifact)
	_, ts := skillServer(t)

	_, result := ensure(t, ts.URL, ts.Client(), items(item("weather-query", remote.URL, claimed)))
	if result.Items[0].Action != "failed" || !strings.Contains(result.Items[0].Error, "hashing to") {
		t.Fatalf("want a manifest digest failure, got %+v", result.Items[0])
	}
}

func TestEnsureSkillsRefusesAFileTheManifestDoesNotList(t *testing.T) {
	listed := map[string]string{"SKILL.md": "# Weather\n"}
	described := describe(listed)
	digest := consoleDigest(described)
	artifact := packTar(map[string]any{"digest": digest, "files": described}, map[string]string{"SKILL.md": "# Weather\n", "extra.sh": "x"})
	remote, _ := skillHost(t, &artifact)
	_, ts := skillServer(t)

	_, result := ensure(t, ts.URL, ts.Client(), items(item("weather-query", remote.URL, digest)))
	if result.Items[0].Action != "failed" {
		t.Fatalf("an unlisted file was installed: %+v", result.Items[0])
	}
}

// The directory is the selection: whatever the request does not name is removed,
// so an uninstalled skill leaves the sandbox on the next sync.
func TestEnsureSkillsRemovesWhatTheSelectionNoLongerNames(t *testing.T) {
	digest, artifact := skillArtifact(map[string]string{"SKILL.md": "# Weather\n"})
	remote, _ := skillHost(t, &artifact)
	skills, ts := skillServer(t)
	stale := filepath.Join(skills, "removed-skill")
	leftover := filepath.Join(skills, ".staging-weather-query-123")
	for _, dir := range []string{stale, leftover} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, result := ensure(t, ts.URL, ts.Client(), items(item("weather-query", remote.URL, digest)))

	if len(result.Removed) != 1 || result.Removed[0] != "removed-skill" {
		t.Fatalf("removed = %v, want [removed-skill]", result.Removed)
	}
	for _, dir := range []string{stale, leftover} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s survived the sync", filepath.Base(dir))
		}
	}
	if _, err := os.Stat(filepath.Join(skills, "weather-query", "SKILL.md")); err != nil {
		t.Fatalf("the selected skill was removed: %v", err)
	}

	if _, empty := ensure(t, ts.URL, ts.Client(), items()); len(empty.Removed) != 1 {
		t.Fatalf("an empty selection removed %v, want [weather-query]", empty.Removed)
	}
}

// A skill whose new version cannot be installed keeps its current copy: it is
// still selected, so it is neither replaced by nothing nor removed.
func TestEnsureSkillsKeepsTheCurrentCopyWhenAnUpdateFails(t *testing.T) {
	digest, artifact := skillArtifact(map[string]string{"SKILL.md": "v1"})
	remote, _ := skillHost(t, &artifact)
	skills, ts := skillServer(t)
	if _, first := ensure(t, ts.URL, ts.Client(), items(item("demo", remote.URL, digest))); first.Items[0].Action != "pulled" {
		t.Fatalf("first ensure = %+v", first.Items[0])
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not a tar"))
	}))
	defer broken.Close()
	_, result := ensure(t, ts.URL, ts.Client(), items(item("demo", broken.URL, strings.Repeat("f", 64))))

	if result.Items[0].Action != "failed" || len(result.Removed) != 0 {
		t.Fatalf("update = %+v removed=%v, want failed and nothing removed", result.Items[0], result.Removed)
	}
	content, err := os.ReadFile(filepath.Join(skills, "demo", "SKILL.md"))
	if err != nil || string(content) != "v1" {
		t.Fatalf("current copy = %q %v, want v1 untouched", content, err)
	}
	entries, _ := os.ReadDir(skills)
	if len(entries) != 1 {
		t.Fatalf("the failed update left %d entries, want only demo", len(entries))
	}
}

func TestEnsureSkillsAnswersEveryItemEvenWhenOneFails(t *testing.T) {
	digest, artifact := skillArtifact(map[string]string{"SKILL.md": "# Weather\n"})
	remote, _ := skillHost(t, &artifact)
	skills, ts := skillServer(t)

	// A name that is not one path segment must not become a path, and must not
	// stop the skill beside it from installing.
	_, result := ensure(t, ts.URL, ts.Client(), items(item("../escape", remote.URL, digest), item("weather-query", remote.URL, digest)))
	if len(result.Items) != 2 || result.Items[0].Action != "failed" || result.Items[1].Action != "pulled" {
		t.Fatalf("items = %+v, want failed then pulled", result.Items)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(skills), "escape")); !os.IsNotExist(err) {
		t.Fatal("a bad name escaped the skills root")
	}
}

func TestEnsureSkillsReplacesTheDirectoryWhenTheDigestChanges(t *testing.T) {
	// Publishing v2 must replace v1 in a sandbox that already holds it.
	first, artifact := skillArtifact(map[string]string{"SKILL.md": "v1", "old.txt": "gone in v2"})
	remote, _ := skillHost(t, &artifact)
	skills, ts := skillServer(t)
	if _, got := ensure(t, ts.URL, ts.Client(), items(item("demo", remote.URL, first))); got.Items[0].Action != "pulled" {
		t.Fatalf("first ensure = %+v", got.Items[0])
	}

	second, next := skillArtifact(map[string]string{"SKILL.md": "v2"})
	artifact = next
	if _, got := ensure(t, ts.URL, ts.Client(), items(item("demo", remote.URL, second))); got.Items[0].Action != "pulled" {
		t.Fatalf("ensure after a publish = %+v, want pulled", got.Items[0])
	}

	content, err := os.ReadFile(filepath.Join(skills, "demo", "SKILL.md"))
	if err != nil || string(content) != "v2" {
		t.Fatalf("SKILL.md = %q %v, want v2", content, err)
	}
	// A file the new version no longer ships does not linger beside it.
	if _, err := os.Stat(filepath.Join(skills, "demo", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old.txt survived the update (err = %v)", err)
	}
}

// The digest is computed in Python by the console and checked here in Go. These
// digests were produced by `console/app/artifacts.py`'s `package`, so a drift in
// either canonical encoding fails this test rather than every install.
func TestEnsureSkillsAgreesWithTheConsolesDigest(t *testing.T) {
	cases := map[string]struct {
		digest string
		files  map[string]string
	}{
		"ascii": {
			digest: "aadeeac7aeea25027451bd595b387f69fe68ac957dfdfd048a7e3483248ff1e4",
			files:  map[string]string{"SKILL.md": "# Demo\n"},
		},
		"escaped": {
			digest: "4417fa954d496415a113f2a300fd73d5b78a40976cf4a25e0506c003c7e1c950",
			files: map[string]string{
				"SKILL.md":          "# Demo\n",
				"scripts/名前.py":     "print(1)\n",
				`notes/a"b\c.txt`:   "quoted\n",
				"emoji/😀.md":        "x",
				"ctl/tab\there.txt": "t",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			artifact := packTar(map[string]any{"digest": tc.digest, "files": describe(tc.files)}, tc.files)
			remote, _ := skillHost(t, &artifact)
			_, ts := skillServer(t)

			_, result := ensure(t, ts.URL, ts.Client(), items(item("demo", remote.URL, tc.digest)))
			if result.Items[0].Action != "pulled" {
				t.Fatalf("a console-made artifact was refused: %+v", result.Items[0])
			}
		})
	}
}
