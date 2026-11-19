package envd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	filesystemv1 "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem"
	filesystemv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem/filesystemv1connect"
	"github.com/iwrs712/litterbox-agent/internal/fsapi"
)

func TestFilesystemServiceMatchesE2BProtocol(t *testing.T) {
	root := t.TempDir()
	filesystem, err := fsapi.NewWithPublicRoot(root, "/")
	if err != nil {
		t.Fatal(err)
	}
	path, handler := filesystemv1connect.NewFilesystemHandler(NewFilesystemService(filesystem))
	server := httptest.NewServer(httpPathHandler(path, handler))
	defer server.Close()
	client := filesystemv1connect.NewFilesystemClient(server.Client(), server.URL)

	ctx := context.Background()
	if _, err := client.MakeDir(ctx, connect.NewRequest(&filesystemv1.MakeDirRequest{Path: "/data"})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "hello.txt"), []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	stat, err := client.Stat(ctx, connect.NewRequest(&filesystemv1.StatRequest{Path: "/data/hello.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	if stat.Msg.Entry.GetPath() != "/data/hello.txt" || stat.Msg.Entry.GetSize() != 5 || stat.Msg.Entry.GetMode() == 0 {
		t.Fatalf("unexpected stat response: %#v", stat.Msg.Entry)
	}
	list, err := client.ListDir(ctx, connect.NewRequest(&filesystemv1.ListDirRequest{Path: "/", Depth: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Msg.Entries) != 2 {
		t.Fatalf("expected directory and file at depth 2, got %#v", list.Msg.Entries)
	}
	if _, err := client.Move(ctx, connect.NewRequest(&filesystemv1.MoveRequest{Source: "/data/hello.txt", Destination: "/data/moved.txt"})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Remove(ctx, connect.NewRequest(&filesystemv1.RemoveRequest{Path: "/data"})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stat(ctx, connect.NewRequest(&filesystemv1.StatRequest{Path: "/data"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected not found after remove, got %v", err)
	}
}

func httpPathHandler(path string, handler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	return mux
}
