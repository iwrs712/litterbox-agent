package fsapi

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const PublicRoot = "/workspace"

var ErrInvalidPath = errors.New("path must stay within /workspace")

type Filesystem struct {
	root       string
	publicRoot string
}

type Entry struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	EntryType     string `json:"entryType"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode"`
	Permissions   string `json:"permissions"`
	Owner         string `json:"owner"`
	Group         string `json:"group"`
	Modified      string `json:"modified,omitempty"`
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
}

type Stat struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	EntryType     string `json:"entryType"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode"`
	Permissions   string `json:"permissions"`
	Owner         string `json:"owner"`
	Group         string `json:"group"`
	Modified      string `json:"modified,omitempty"`
	SymlinkTarget string `json:"symlinkTarget,omitempty"`
}

func New(root string) (*Filesystem, error) {
	return NewWithPublicRoot(root, PublicRoot)
}

func NewWithPublicRoot(root, publicRoot string) (*Filesystem, error) {
	if root == "" {
		return nil, errors.New("workspace root is required")
	}
	publicRoot, err := normalizePublicRoot(publicRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &Filesystem{root: filepath.Clean(resolved), publicRoot: publicRoot}, nil
}

func (f *Filesystem) List(path string) ([]Entry, error) {
	return f.ListDepth(path, 1)
}

func (f *Filesystem) ListDepth(path string, depth uint32) ([]Entry, error) {
	target, virtual, err := f.resolve(path, false)
	if err != nil {
		return nil, err
	}
	if depth == 0 {
		depth = 1
	}
	return f.listDepth(target, virtual, depth)
}

func (f *Filesystem) listDepth(target, virtual string, depth uint32) ([]Entry, error) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		entryPath := joinVirtual(virtual, entry.Name())
		item, err := entryFromInfo(entryPath, info, filepath.Join(target, entry.Name()))
		if err != nil {
			return nil, err
		}
		result = append(result, item)
		if depth > 1 && info.IsDir() {
			children, err := f.listDepth(filepath.Join(target, entry.Name()), entryPath, depth-1)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		}
	}
	return result, nil
}

func (f *Filesystem) Stat(path string) (Stat, error) {
	target, virtual, err := f.resolveNoFollow(path)
	if err != nil {
		return Stat{}, err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return Stat{}, err
	}
	entry, err := entryFromInfo(virtual, info, target)
	if err != nil {
		return Stat{}, err
	}
	return Stat(entry), nil
}

func (f *Filesystem) Read(path string) ([]byte, error) {
	target, _, err := f.resolve(path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a file: %s", path)
	}
	return os.ReadFile(target)
}

func (f *Filesystem) Write(path string, contents []byte) (string, error) {
	target, virtual, err := f.resolve(path, true)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		return "", err
	}
	return virtual, nil
}

func (f *Filesystem) Mkdir(path string) (string, error) {
	target, virtual, err := f.resolve(path, true)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	return virtual, nil
}

func (f *Filesystem) Move(fromPath, toPath string, overwrite bool) (string, string, error) {
	from, fromVirtual, err := f.resolve(fromPath, false)
	if err != nil {
		return "", "", err
	}
	to, toVirtual, err := f.resolve(toPath, true)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(to); err == nil {
		if !overwrite {
			return "", "", fmt.Errorf("destination already exists: %s", toVirtual)
		}
		info, err := os.Lstat(to)
		if err != nil {
			return "", "", err
		}
		if info.IsDir() {
			err = os.RemoveAll(to)
		} else {
			err = os.Remove(to)
		}
		if err != nil {
			return "", "", err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return "", "", err
	}
	if err := os.Rename(from, to); err != nil {
		return "", "", err
	}
	return fromVirtual, toVirtual, nil
}

func (f *Filesystem) Delete(path string, recursive bool) (string, error) {
	target, virtual, err := f.resolve(path, false)
	if err != nil {
		return "", err
	}
	if target == f.root {
		return "", ErrInvalidPath
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		if recursive {
			err = os.RemoveAll(target)
		} else {
			err = os.Remove(target)
		}
	} else {
		err = os.Remove(target)
	}
	if err != nil {
		return "", err
	}
	return virtual, nil
}

func (f *Filesystem) ExtractTar(path string, reader io.Reader) ([]string, bool, error) {
	base, baseVirtual, err := f.resolve(path, true)
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, false, err
	}

	archive := tar.NewReader(reader)
	paths := make([]string, 0)
	truncated := false
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
		rel, err := cleanArchivePath(header.Name)
		if err != nil {
			return nil, false, err
		}
		destination := filepath.Join(base, rel)
		if !isWithin(base, destination) {
			return nil, false, ErrInvalidPath
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return nil, false, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return nil, false, err
			}
			file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fs.FileMode(header.Mode))
			if err != nil {
				return nil, false, err
			}
			_, copyErr := io.Copy(file, archive)
			closeErr := file.Close()
			if copyErr != nil {
				return nil, false, copyErr
			}
			if closeErr != nil {
				return nil, false, closeErr
			}
		default:
			return nil, false, fmt.Errorf("unsupported tar entry type for %s", header.Name)
		}
		if len(paths) < 1024 {
			paths = append(paths, joinVirtual(baseVirtual, filepath.ToSlash(rel)))
		} else {
			truncated = true
		}
	}
	return paths, truncated, nil
}

func (f *Filesystem) ResolveDirectory(path string) (string, error) {
	target, _, err := f.resolve(path, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", path)
	}
	return target, nil
}

// HostPath returns the canonical host path for a public path. When
// allowMissing is true, missing leaf components are accepted while existing
// parent symlinks are still resolved and checked against the sandbox root.
// When false, the final component is not followed, which is required for
// safe rename/delete operations on symlinks.
func (f *Filesystem) HostPath(path string, allowMissing bool) (string, error) {
	virtual, err := f.PublicPath(path)
	if err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(virtual, f.publicRoot)
	rel = strings.TrimPrefix(rel, "/")
	candidate := filepath.Join(f.root, filepath.FromSlash(rel))
	if !isWithin(f.root, candidate) {
		return "", ErrInvalidPath
	}
	if _, statErr := os.Lstat(candidate); statErr == nil {
		target, _, resolveErr := f.resolveNoFollow(virtual)
		return target, resolveErr
	} else if !errors.Is(statErr, fs.ErrNotExist) || !allowMissing {
		return "", statErr
	}
	target, _, resolveErr := f.resolve(virtual, true)
	return target, resolveErr
}

// Root returns the canonical host path that backs the public /workspace root.
func (f *Filesystem) Root() string {
	return f.root
}

func (f *Filesystem) PublicRoot() string {
	return f.publicRoot
}

func PublicPath(path string) (string, error) {
	return cleanPublicPath(path, PublicRoot)
}

func (f *Filesystem) PublicPath(path string) (string, error) {
	return cleanPublicPath(path, f.publicRoot)
}

func (f *Filesystem) resolve(publicPath string, allowMissing bool) (string, string, error) {
	virtual, err := cleanPublicPath(publicPath, f.publicRoot)
	if err != nil {
		return "", "", err
	}
	rel := strings.TrimPrefix(virtual, f.publicRoot)
	rel = strings.TrimPrefix(rel, "/")
	candidate := filepath.Join(f.root, filepath.FromSlash(rel))
	if !isWithin(f.root, candidate) {
		return "", "", ErrInvalidPath
	}

	if !allowMissing {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", "", err
		}
		if !isWithin(f.root, resolved) {
			return "", "", ErrInvalidPath
		}
		return resolved, virtual, nil
	}

	resolved, err := resolveMissingPath(f.root, candidate)
	if err != nil {
		return "", "", err
	}
	return resolved, virtual, nil
}

func (f *Filesystem) resolveNoFollow(publicPath string) (string, string, error) {
	virtual, err := cleanPublicPath(publicPath, f.publicRoot)
	if err != nil {
		return "", "", err
	}
	rel := strings.TrimPrefix(virtual, f.publicRoot)
	rel = strings.TrimPrefix(rel, "/")
	candidate := filepath.Join(f.root, filepath.FromSlash(rel))
	if !isWithin(f.root, candidate) {
		return "", "", ErrInvalidPath
	}
	if candidate == f.root {
		return candidate, virtual, nil
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		return "", "", err
	}
	if !isWithin(f.root, parent) {
		return "", "", ErrInvalidPath
	}
	return filepath.Join(parent, filepath.Base(candidate)), virtual, nil
}

func resolveMissingPath(root, candidate string) (string, error) {
	current := candidate
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current || !isWithin(root, parent) {
			return "", ErrInvalidPath
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	if !isWithin(root, resolved) {
		return "", ErrInvalidPath
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, missing[index])
	}
	if !isWithin(root, resolved) {
		return "", ErrInvalidPath
	}
	return resolved, nil
}

func cleanPublicPath(path, publicRoot string) (string, error) {
	if path == "" || path == "." {
		return publicRoot, nil
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if publicRoot == "/" {
		if !strings.HasPrefix(cleaned, "/") {
			return "", ErrInvalidPath
		}
		return cleaned, nil
	}
	if cleaned != publicRoot && !strings.HasPrefix(cleaned, publicRoot+"/") {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

func normalizePublicRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", errors.New("public root must be an absolute path")
	}
	return filepath.ToSlash(filepath.Clean(root)), nil
}

func cleanArchivePath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", ErrInvalidPath
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

func entryType(info fs.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "directory"
	}
	return "file"
}

func entryFromInfo(path string, info fs.FileInfo, target string) (Entry, error) {
	mode, owner, group := fileMetadata(info)
	entry := Entry{
		Name:        filepath.Base(path),
		Path:        path,
		EntryType:   entryType(info),
		Size:        info.Size(),
		Mode:        mode,
		Permissions: info.Mode().Perm().String(),
		Owner:       owner,
		Group:       group,
		Modified:    rfc3339(info.ModTime()),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(target)
		if err != nil {
			return Entry{}, err
		}
		entry.SymlinkTarget = link
	}
	return entry, nil
}

func fileMetadata(info fs.FileInfo) (uint32, string, string) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return uint32(info.Mode().Perm()), "", ""
	}
	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	gid := strconv.FormatUint(uint64(stat.Gid), 10)
	owner, group := uid, gid
	if value, err := user.LookupId(uid); err == nil {
		owner = value.Username
	}
	if value, err := user.LookupGroupId(gid); err == nil {
		group = value.Name
	}
	return uint32(stat.Mode), owner, group
}

func joinVirtual(base, name string) string {
	return filepath.ToSlash(filepath.Join(base, name))
}

func isWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func rfc3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
