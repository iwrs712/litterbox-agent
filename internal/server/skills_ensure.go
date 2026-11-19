// Package server — the skills directory as a projection of the catalogue.
//
// The backend sends the complete list of skills a tenant has selected, each with
// the digest the catalogue currently serves. This handler makes
// `/workspace/.agents/skills` hold exactly that list: a skill already at its
// digest is kept, one that is missing or at another digest is replaced, and a
// directory the list does not name is removed. The directory is a cache of
// immutable, content-addressed artifacts, so removing too much only costs a
// download on the next turn.
//
// An artifact is installed by extracting it into a staging directory beside the
// root, checking every file against the manifest it carries, and renaming the
// staged directory into place. A skill directory therefore only ever holds a
// whole, verified artifact, which is why "already installed" is just the digest
// in its manifest.
package server

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// skillsRoot is owned here rather than taken from the request. Reconciling
// deletes directories, so the one path it may ever touch is the one this
// process decides.
const skillsRoot = "/workspace/.agents/skills"

// skillManifestName is the entry every artifact carries, written by the console
// when it packed the tar.
const skillManifestName = "manifest.json"

// stagingPrefix names the directories an install extracts into. They sit inside
// the root so the final rename never crosses a filesystem, and the leading dot
// keeps them out of the skill-name pattern.
const stagingPrefix = ".staging-"

// maxArtifactBytes bounds one download. The console refuses packages over 8 MiB
// and extracted trees over 32 MiB.
const maxArtifactBytes = 64 << 20

// A skill key is also its directory name, so it has to be one path segment. The
// console refuses anything else at publish; refusing it here too means a
// malformed key can never become an escape attempt.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type skillsEnsureRequest struct {
	Items []skillsEnsureItem `json:"items"`
}

type skillsEnsureItem struct {
	URL    string `json:"url"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

type skillsEnsureResult struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Action string `json:"action"` // kept | pulled | failed
	Error  string `json:"error,omitempty"`
}

// skillManifest is the manifest.json the console writes: the digest, and every
// file with its size and sha256. The digest is the sha256 of the canonical JSON
// of `files`, so it commits to every file's content.
type skillManifest struct {
	Digest string         `json:"digest"`
	Files  []manifestFile `json:"files"`
}

type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (s *Server) ensureSkills(w http.ResponseWriter, r *http.Request) {
	var request skillsEnsureRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ensure request"})
		return
	}
	if len(request.Items) > 256 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many skills in one request"})
		return
	}
	s.skills.Lock()
	defer s.skills.Unlock()
	root, err := s.fs.HostPath(skillsRoot, true)
	if err == nil {
		err = os.MkdirAll(root, 0o755)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("skills root: %v", err)})
		return
	}

	results := make([]skillsEnsureResult, 0, len(request.Items))
	wanted := make(map[string]bool, len(request.Items))
	for _, item := range request.Items {
		name := strings.TrimSpace(item.Name)
		if !skillNamePattern.MatchString(name) {
			results = append(results, skillsEnsureResult{
				Name: name, Digest: item.Digest, Action: "failed",
				Error: "name must be one path segment of [a-z0-9._-]",
			})
			continue
		}
		// Named, so it survives the reconcile below even if this install fails:
		// a skill the user still has keeps its previous copy rather than none.
		wanted[name] = true
		results = append(results, ensureSkill(root, name, item))
	}

	removed, err := reconcileSkills(root, wanted)
	payload := map[string]any{"root": skillsRoot, "items": results, "removed": removed}
	if err != nil {
		payload["remove_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

// ensureSkill installs one skill, or reports that it is already installed.
//
// A failure here is one item's failure: the caller gets a verdict for every
// skill it asked about, so one unreachable artifact cannot leave the rest of a
// user's skills un-installed and unexplained.
func ensureSkill(root, name string, item skillsEnsureItem) skillsEnsureResult {
	result := skillsEnsureResult{Name: name, Digest: item.Digest, Action: "kept"}
	if !digestPattern.MatchString(item.Digest) {
		result.Action, result.Error = "failed", "digest must be 64 lowercase hex characters"
		return result
	}
	destination := filepath.Join(root, name)
	if installedDigest(destination) == item.Digest {
		return result
	}
	if err := installSkill(root, destination, item); err != nil {
		result.Action, result.Error = "failed", err.Error()
		return result
	}
	result.Action = "pulled"
	return result
}

// installedDigest is the digest a skill directory holds, or "" when it holds
// none. Every directory under the root was renamed in whole after verification,
// so its manifest's digest is the answer.
func installedDigest(destination string) string {
	raw, err := os.ReadFile(filepath.Join(destination, skillManifestName))
	if err != nil {
		return ""
	}
	var manifest skillManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return ""
	}
	return manifest.Digest
}

// installSkill downloads one artifact, verifies it in a staging directory and
// renames it over the destination.
func installSkill(root, destination string, item skillsEnsureItem) error {
	if err := validatePullURL(item.URL); err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 10 * time.Minute}).Get(item.URL)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}

	staging, err := os.MkdirTemp(root, stagingPrefix+filepath.Base(destination)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	manifest, err := extractArtifact(staging, io.LimitReader(response.Body, maxArtifactBytes+1))
	if err != nil {
		return err
	}
	if manifest.Digest != item.Digest {
		return fmt.Errorf("artifact digest %s does not match the requested %s", manifest.Digest, item.Digest)
	}

	// The previous copy is moved aside rather than deleted first, so a failed
	// rename leaves it where it was.
	previous := ""
	if _, err := os.Lstat(destination); err == nil {
		previous = staging + ".previous"
		if err := os.Rename(destination, previous); err != nil {
			return err
		}
		defer os.RemoveAll(previous)
	}
	if err := os.Rename(staging, destination); err != nil {
		if previous != "" {
			_ = os.Rename(previous, destination)
		}
		return err
	}
	return nil
}

// extractArtifact writes the tar's files into staging and returns its manifest
// once every file matches it: the same set of paths, each at its declared size
// and sha256, and a digest that is the hash of that file list.
func extractArtifact(staging string, body io.Reader) (skillManifest, error) {
	var manifest skillManifest
	var rawManifest []byte
	written := map[string]string{}
	var total int64
	archive := tar.NewReader(body)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return manifest, fmt.Errorf("artifact is not a readable tar: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return manifest, fmt.Errorf("artifact entry %s is not a regular file", header.Name)
		}
		rel, err := artifactPath(header.Name)
		if err != nil {
			return manifest, err
		}
		if _, seen := written[rel]; seen || (rel == skillManifestName && rawManifest != nil) {
			return manifest, fmt.Errorf("artifact contains %s twice", rel)
		}
		total += header.Size
		if total > maxArtifactBytes {
			return manifest, errors.New("artifact is larger than a skill may be")
		}
		if rel == skillManifestName {
			if rawManifest, err = io.ReadAll(archive); err != nil {
				return manifest, fmt.Errorf("artifact manifest is unreadable: %v", err)
			}
			continue
		}
		sum, err := writeArtifactFile(filepath.Join(staging, filepath.FromSlash(rel)), archive)
		if err != nil {
			return manifest, err
		}
		written[rel] = sum
	}

	if rawManifest == nil {
		return manifest, errors.New("artifact has no manifest.json")
	}
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return manifest, fmt.Errorf("artifact manifest is not JSON: %v", err)
	}
	if len(manifest.Files) != len(written) {
		return manifest, fmt.Errorf("artifact holds %d files, its manifest lists %d", len(written), len(manifest.Files))
	}
	for _, file := range manifest.Files {
		sum, ok := written[file.Path]
		if !ok {
			return manifest, fmt.Errorf("artifact is missing %s", file.Path)
		}
		if sum != file.SHA256 {
			return manifest, fmt.Errorf("%s does not match its manifest", file.Path)
		}
		info, err := os.Stat(filepath.Join(staging, filepath.FromSlash(file.Path)))
		if err != nil || info.Size() != file.Size {
			return manifest, fmt.Errorf("%s is not the size its manifest says", file.Path)
		}
	}
	if digest := manifestDigest(manifest.Files); digest != manifest.Digest {
		return manifest, fmt.Errorf("artifact manifest claims digest %s but lists files hashing to %s", manifest.Digest, digest)
	}
	// The manifest is what records the installed digest.
	if err := os.WriteFile(filepath.Join(staging, skillManifestName), rawManifest, 0o644); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func writeArtifactFile(target string, contents io.Reader) (string, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), contents)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// artifactPath is a tar entry's path relative to the skill directory, refused
// if it could land anywhere else.
func artifactPath(name string) (string, error) {
	cleaned := path.Clean(strings.TrimPrefix(name, "./"))
	if name == "" || path.IsAbs(name) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("artifact entry %q leaves the skill directory", name)
	}
	return cleaned, nil
}

// manifestDigest is the console's digest of a file list: sha256 over the
// canonical JSON `{"files":[{"path","sha256","size"},...]}` with sorted keys,
// no whitespace and non-ASCII escaped (`console/app/artifacts.py`).
func manifestDigest(files []manifestFile) string {
	sorted := append([]manifestFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	var b strings.Builder
	b.WriteString(`{"files":[`)
	for i, file := range sorted {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"path":%s,"sha256":%s,"size":%d}`, asciiJSON(file.Path), asciiJSON(file.SHA256), file.Size)
	}
	b.WriteString(`]}`)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// asciiJSON encodes a string the way Python's json.dumps(ensure_ascii=True)
// does, which is what the console hashes.
func asciiJSON(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r < 0x20 || (r >= 0x7f && r <= 0xffff):
				fmt.Fprintf(&b, `\u%04x`, r)
			case r > 0xffff:
				r -= 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// reconcileSkills removes every entry under the root the request did not name,
// including staging directories an interrupted install left behind.
func reconcileSkills(root string, wanted map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []string{}, err
	}
	removed := []string{}
	var failure error
	for _, entry := range entries {
		name := entry.Name()
		if wanted[name] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			failure = errors.Join(failure, fmt.Errorf("could not remove %s: %v", name, err))
			continue
		}
		if !strings.HasPrefix(name, stagingPrefix) {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	return removed, failure
}

func validatePullURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("url must be an absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url must use http or https")
	}
	return nil
}
