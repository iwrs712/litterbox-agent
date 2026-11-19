package server_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	filesystemv1 "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem"
	filesystemv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem/filesystemv1connect"
	"github.com/iwrs712/litterbox-agent/internal/server"
	"golang.org/x/net/http2"
)

func TestServerAcceptsStandardGRPCOverH2C(t *testing.T) {
	handler := server.New(server.Config{FilesystemRoot: t.TempDir(), FilesystemPublicRoot: "/"})
	testServer := restoreTestWorkspace(t, httptest.NewServer(handler))
	defer testServer.Close()
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
	client := filesystemv1connect.NewFilesystemClient(&http.Client{Transport: transport}, testServer.URL, connect.WithGRPC())
	response, err := client.MakeDir(context.Background(), connect.NewRequest(&filesystemv1.MakeDirRequest{Path: "/grpc"}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Entry.GetPath() != "/grpc" {
		t.Fatalf("unexpected grpc response: %#v", response.Msg.Entry)
	}
}
