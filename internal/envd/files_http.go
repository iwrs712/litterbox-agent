package envd

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"

	"github.com/iwrs712/litterbox-agent/internal/fsapi"
)

type fileEntryResponse struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func NewFilesHandler(filesystem *fsapi.Filesystem) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /files", func(w http.ResponseWriter, r *http.Request) {
		contents, err := filesystem.Read(r.URL.Query().Get("path"))
		if err != nil {
			writeFilesError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(contents)
	})
	mux.HandleFunc("POST /files", func(w http.ResponseWriter, r *http.Request) {
		path, contents, err := readFileUpload(r)
		if err != nil {
			writeFilesError(w, err)
			return
		}
		if queryPath := r.URL.Query().Get("path"); queryPath != "" {
			path = queryPath
		}
		if path == "" {
			writeFilesError(w, errors.New("path is required"))
			return
		}
		written, err := filesystem.Write(path, contents)
		if err != nil {
			writeFilesError(w, err)
			return
		}
		stat, err := filesystem.Stat(written)
		if err != nil {
			writeFilesError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, []fileEntryResponse{{
			Path: stat.Path,
			Name: filepath.Base(stat.Path),
			Type: stat.EntryType,
		}})
	})
	return mux
}

func readFileUpload(request *http.Request) (string, []byte, error) {
	reader := io.Reader(request.Body)
	var closer io.Closer
	if request.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(request.Body)
		if err != nil {
			return "", nil, err
		}
		reader = gzipReader
		closer = gzipReader
	}
	if closer != nil {
		defer closer.Close()
	}

	contentType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return "", nil, err
	}
	if contentType == "multipart/form-data" {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil, errors.New("multipart boundary is required")
		}
		multipartReader := multipart.NewReader(reader, boundary)
		part, err := multipartReader.NextPart()
		if err != nil {
			return "", nil, err
		}
		contents, err := io.ReadAll(io.LimitReader(part, 100<<20))
		return part.FileName(), contents, err
	}
	if contentType != "application/octet-stream" {
		return "", nil, errors.New("content-type must be application/octet-stream or multipart/form-data")
	}
	contents, err := io.ReadAll(io.LimitReader(reader, 100<<20))
	return "", contents, err
}

func writeFilesError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, fsapi.ErrInvalidPath) || errors.Is(err, io.ErrUnexpectedEOF) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"message": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
