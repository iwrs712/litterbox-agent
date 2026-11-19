package envd

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	filesystemv1 "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem"
	filesystemv1connect "github.com/iwrs712/litterbox-agent/internal/envd/proto/filesystem/filesystemv1connect"
	"github.com/iwrs712/litterbox-agent/internal/fsapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type FilesystemService struct {
	filesystemv1connect.UnimplementedFilesystemHandler
	fs *fsapi.Filesystem
}

func NewFilesystemService(filesystem *fsapi.Filesystem) *FilesystemService {
	return &FilesystemService{fs: filesystem}
}

func (s *FilesystemService) Stat(_ context.Context, request *connect.Request[filesystemv1.StatRequest]) (*connect.Response[filesystemv1.StatResponse], error) {
	entry, err := s.fs.Stat(request.Msg.Path)
	if err != nil {
		return nil, filesystemError(err)
	}
	return connect.NewResponse(&filesystemv1.StatResponse{Entry: statEntry(entry)}), nil
}

func (s *FilesystemService) MakeDir(_ context.Context, request *connect.Request[filesystemv1.MakeDirRequest]) (*connect.Response[filesystemv1.MakeDirResponse], error) {
	path, err := s.fs.Mkdir(request.Msg.Path)
	if err != nil {
		return nil, filesystemError(err)
	}
	entry, err := s.fs.Stat(path)
	if err != nil {
		return nil, filesystemError(err)
	}
	return connect.NewResponse(&filesystemv1.MakeDirResponse{Entry: statEntry(entry)}), nil
}

func (s *FilesystemService) Move(_ context.Context, request *connect.Request[filesystemv1.MoveRequest]) (*connect.Response[filesystemv1.MoveResponse], error) {
	_, destination, err := s.fs.Move(request.Msg.Source, request.Msg.Destination, false)
	if err != nil {
		return nil, filesystemError(err)
	}
	entry, err := s.fs.Stat(destination)
	if err != nil {
		return nil, filesystemError(err)
	}
	return connect.NewResponse(&filesystemv1.MoveResponse{Entry: statEntry(entry)}), nil
}

func (s *FilesystemService) ListDir(_ context.Context, request *connect.Request[filesystemv1.ListDirRequest]) (*connect.Response[filesystemv1.ListDirResponse], error) {
	entries, err := s.fs.ListDepth(request.Msg.Path, request.Msg.Depth)
	if err != nil {
		return nil, filesystemError(err)
	}
	result := make([]*filesystemv1.EntryInfo, 0, len(entries))
	for _, entry := range entries {
		result = append(result, fsEntry(entry))
	}
	return connect.NewResponse(&filesystemv1.ListDirResponse{Entries: result}), nil
}

func (s *FilesystemService) Remove(_ context.Context, request *connect.Request[filesystemv1.RemoveRequest]) (*connect.Response[filesystemv1.RemoveResponse], error) {
	if _, err := s.fs.Delete(request.Msg.Path, true); err != nil {
		return nil, filesystemError(err)
	}
	return connect.NewResponse(&filesystemv1.RemoveResponse{}), nil
}

func statEntry(entry fsapi.Stat) *filesystemv1.EntryInfo {
	symlinkTarget := entry.SymlinkTarget
	result := &filesystemv1.EntryInfo{
		Name:         entry.Name,
		Type:         fileType(entry.EntryType),
		Path:         entry.Path,
		Size:         entry.Size,
		Mode:         entry.Mode,
		Permissions:  entry.Permissions,
		Owner:        entry.Owner,
		Group:        entry.Group,
		ModifiedTime: parseTimestamp(entry.Modified),
	}
	if symlinkTarget != "" {
		result.SymlinkTarget = &symlinkTarget
	}
	return result
}

func fsEntry(entry fsapi.Entry) *filesystemv1.EntryInfo {
	return statEntry(fsapi.Stat(entry))
}

func fileType(value string) filesystemv1.FileType {
	switch value {
	case "file":
		return filesystemv1.FileType_FILE_TYPE_FILE
	case "directory":
		return filesystemv1.FileType_FILE_TYPE_DIRECTORY
	case "symlink":
		return filesystemv1.FileType_FILE_TYPE_SYMLINK
	default:
		return filesystemv1.FileType_FILE_TYPE_UNSPECIFIED
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}

func filesystemError(err error) error {
	code := connect.CodeInternal
	switch {
	case errors.Is(err, fsapi.ErrInvalidPath):
		code = connect.CodeInvalidArgument
	case errors.Is(err, fs.ErrNotExist):
		code = connect.CodeNotFound
	case errors.Is(err, fs.ErrPermission):
		code = connect.CodePermissionDenied
	case errors.Is(err, fs.ErrExist):
		code = connect.CodeAlreadyExists
	}
	return connect.NewError(code, err)
}

func cleanFilename(path string) string {
	return filepath.Base(filepath.Clean(path))
}
