package service

import (
	"bufio"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"litterbox-agent/internal/model"
)

const maxHistorySize = 10

type FileService struct {
	editHistory map[string][]string //
}

func NewFileService() *FileService {
	return &FileService{
		editHistory: make(map[string][]string),
	}
}

// addHistory adds a history entry for a file, maintaining max size
func (s *FileService) addHistory(path, content string) {
	history := s.editHistory[path]
	history = append(history, content)

	if len(history) > maxHistorySize {
		history = history[len(history)-maxHistorySize:]
	}

	s.editHistory[path] = history
}

// UploadFile uploads a file to the specified directory
func (s *FileService) UploadFile(file multipart.File, header *multipart.FileHeader, uploadDir string) (string, error) {
	if uploadDir == "" {
		uploadDir = "/tmp"
	}

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", err
	}

	dst := filepath.Join(uploadDir, header.Filename)
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		return "", err
	}

	return dst, nil
}

// DownloadFile returns a file from the specified path
func (s *FileService) DownloadFile(filePath string) (*os.File, os.FileInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}

	return file, stat, nil
}

// FileOperation performs unified file operations
func (s *FileService) FileOperation(req *model.FileOperationRequest) (*model.FileOperationResponse, error) {
	switch req.Command {
	case "view":
		return s.viewFile(req)
	case "create":
		return s.createFile(req)
	case "str_replace":
		return s.strReplace(req)
	case "insert":
		return s.insertLine(req)
	case "undo_edit":
		return s.undoEdit(req)
	default:
		return &model.FileOperationResponse{
			Success: false,
			Message: fmt.Sprintf("Unknown command: %s", req.Command),
		}, nil
	}
}

// viewFile reads and returns file content with optional line range
func (s *FileService) viewFile(req *model.FileOperationRequest) (*model.FileOperationResponse, error) {
	file, err := os.Open(req.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	totalLines := len(lines)

	start, end := 0, totalLines
	if len(req.ViewRange) >= 2 {
		start = req.ViewRange[0] - 1
		end = req.ViewRange[1]
		if start < 0 {
			start = 0
		}
		if end > totalLines {
			end = totalLines
		}
		if start > end {
			start, end = end, start
		}
	}

	content := strings.Join(lines[start:end], "\n")

	return &model.FileOperationResponse{
		Success: true,
		Content: content,
		Lines:   totalLines,
		Message: fmt.Sprintf("Showing lines %d-%d of %d", start+1, end, totalLines),
	}, nil
}

// createFile creates a new file with content
func (s *FileService) createFile(req *model.FileOperationRequest) (*model.FileOperationResponse, error) {
	if _, err := os.Stat(req.Path); err == nil {
		return &model.FileOperationResponse{
			Success: false,
			Message: "File already exists",
		}, nil
	}

	dir := filepath.Dir(req.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	if err := os.WriteFile(req.Path, []byte(req.FileText), 0644); err != nil {
		return nil, err
	}

	return &model.FileOperationResponse{
		Success: true,
		Message: fmt.Sprintf("File created: %s", req.Path),
	}, nil
}

// strReplace performs string replacement in file
func (s *FileService) strReplace(req *model.FileOperationRequest) (*model.FileOperationResponse, error) {
	data, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, err
	}

	content := string(data)

	// 保存历史用于undo
	s.addHistory(req.Path, content)

	if !strings.Contains(content, req.OldStr) {
		return &model.FileOperationResponse{
			Success: false,
			Message: "String not found in file",
		}, nil
	}

	newContent := strings.Replace(content, req.OldStr, req.NewStr, -1)

	if err := os.WriteFile(req.Path, []byte(newContent), 0644); err != nil {
		return nil, err
	}

	count := strings.Count(content, req.OldStr)
	return &model.FileOperationResponse{
		Success: true,
		Message: fmt.Sprintf("Replaced %d occurrence(s)", count),
	}, nil
}

// insertLine inserts content after specified line
func (s *FileService) insertLine(req *model.FileOperationRequest) (*model.FileOperationResponse, error) {
	// 读取文件
	file, err := os.Open(req.Path)
	if err != nil {
		return nil, err
	}

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	file.Close()

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	s.addHistory(req.Path, strings.Join(lines, "\n"))

	if req.InsertLine < 0 || req.InsertLine > len(lines) {
		return &model.FileOperationResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid line number: %d (file has %d lines)", req.InsertLine, len(lines)),
		}, nil
	}

	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:req.InsertLine]...)
	newLines = append(newLines, req.NewStr)
	newLines = append(newLines, lines[req.InsertLine:]...)

	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(req.Path, []byte(newContent), 0644); err != nil {
		return nil, err
	}

	return &model.FileOperationResponse{
		Success: true,
		Message: fmt.Sprintf("Inserted line after line %d", req.InsertLine),
	}, nil
}

// undoEdit undoes the last edit operation
func (s *FileService) undoEdit(req *model.FileOperationRequest) (*model.FileOperationResponse, error) {
	history, exists := s.editHistory[req.Path]
	if !exists || len(history) == 0 {
		return &model.FileOperationResponse{
			Success: false,
			Message: "No edit history to undo",
		}, nil
	}

	lastVersion := history[len(history)-1]
	s.editHistory[req.Path] = history[:len(history)-1]

	if err := os.WriteFile(req.Path, []byte(lastVersion), 0644); err != nil {
		return nil, err
	}

	return &model.FileOperationResponse{
		Success: true,
		Message: "Edit undone successfully",
	}, nil
}
