package fsapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemSupportsRootPublicPathAndDepth(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested", "deep"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file.txt"), []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	filesystem, err := NewWithPublicRoot(root, "/")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := filesystem.ListDepth("/", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "/nested" || entries[0].EntryType != "directory" {
		t.Fatalf("unexpected depth-one entries: %#v", entries)
	}
	if entries[0].Mode == 0 || entries[0].Permissions == "" {
		t.Fatalf("expected rich entry metadata: %#v", entries[0])
	}
	if entries[0].Mode&0o170000 != 0o040000 {
		t.Fatalf("expected POSIX directory mode, got %#o", entries[0].Mode)
	}

	stat, err := filesystem.Stat("/nested/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if stat.Path != "/nested/file.txt" || stat.Size != 5 || stat.Mode == 0 || stat.Permissions == "" {
		t.Fatalf("unexpected stat: %#v", stat)
	}
}

func TestFilesystemReportsSymlinkTargetWithoutFollowingIt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("value"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	filesystem, err := NewWithPublicRoot(root, "/")
	if err != nil {
		t.Fatal(err)
	}
	stat, err := filesystem.Stat("/link")
	if err != nil {
		t.Fatal(err)
	}
	if stat.EntryType != "symlink" || stat.SymlinkTarget != "target" {
		t.Fatalf("unexpected symlink stat: %#v", stat)
	}
}
