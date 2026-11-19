package envd

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwrs712/litterbox-agent/internal/fsapi"
)

func TestFilesHTTPSupportsDownloadAndOctetStreamUpload(t *testing.T) {
	root := t.TempDir()
	filesystem, err := fsapi.NewWithPublicRoot(root, "/")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewFilesHandler(filesystem))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/files?path=%2Fdata%2Fhello.txt", bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected upload status: %s", response.Status)
	}
	var uploaded []fileEntryResponse
	if err := json.NewDecoder(response.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(uploaded) != 1 || uploaded[0].Path != "/data/hello.txt" {
		t.Fatalf("unexpected upload response: %#v", uploaded)
	}

	response, err = http.Get(server.URL + "/files?path=%2Fdata%2Fhello.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "hello" {
		t.Fatalf("unexpected downloaded contents: %q", contents)
	}
}

func TestFilesHTTPSupportsGzipMultipartUpload(t *testing.T) {
	root := t.TempDir()
	filesystem, err := fsapi.NewWithPublicRoot(root, "/")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewFilesHandler(filesystem))
	defer server.Close()

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="ignored.txt"`)
	header.Set("Content-Type", "application/octet-stream")
	part, err := multipartWriter.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("compressed")); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	zipWriter := gzip.NewWriter(&compressed)
	if _, err := zipWriter.Write(body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/files?path=%2Fdata%2Fcompressed.txt", &compressed)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("Content-Encoding", "gzip")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected multipart status: %s", response.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "compressed.txt")); err != nil {
		t.Fatal(err)
	}
}
