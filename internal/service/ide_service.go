package service

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"litterbox-agent/internal/model"
)

// IDEService handles IDE-related operations
type IDEService struct{}

// NewIDEService creates a new IDEService
func NewIDEService() *IDEService {
	return &IDEService{}
}

// GetFileTree returns the directory tree structure
func (s *IDEService) GetFileTree(rootPath string) (*model.FileTreeNode, error) {
	// Clean and validate path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	return s.buildTree(absPath, info)
}

// buildTree recursively builds the file tree
func (s *IDEService) buildTree(path string, info fs.FileInfo) (*model.FileTreeNode, error) {
	node := &model.FileTreeNode{
		Name:  info.Name(),
		Path:  path,
		IsDir: info.IsDir(),
	}

	// Skip hidden files/folders (starting with .)
	if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
		return nil, nil
	}

	if !info.IsDir() {
		node.Size = info.Size()
		return node, nil
	}

	// Read directory contents
	entries, err := os.ReadDir(path)
	if err != nil {
		// Return node without children if we can't read the directory
		return node, nil
	}

	children := make([]*model.FileTreeNode, 0)
	for _, entry := range entries {
		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		childPath := filepath.Join(path, entry.Name())
		childInfo, err := entry.Info()
		if err != nil {
			continue
		}

		childNode, err := s.buildTree(childPath, childInfo)
		if err != nil || childNode == nil {
			continue
		}

		children = append(children, childNode)
	}

	// Sort children: directories first, then files, both alphabetically
	sort.Slice(children, func(i, j int) bool {
		// If one is directory and other is file, directory comes first
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		// Both are same type, sort alphabetically (case-insensitive)
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})

	node.Children = children
	return node, nil
}

// ReadFileContent reads the content of a file
func (s *IDEService) ReadFileContent(filePath string) (string, error) {
	// Clean and validate path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file")
	}

	// Read file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

// IsImageFile checks if a file is an image based on extension
func (s *IDEService) IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	imageExts := []string{".png", ".jpg", ".jpeg", ".gif", ".bmp", ".svg", ".webp", ".ico"}
	for _, imgExt := range imageExts {
		if ext == imgExt {
			return true
		}
	}
	return false
}

// SaveFileContent saves content to a file
func (s *IDEService) SaveFileContent(filePath string, content string) error {
	// Clean and validate path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Write file content
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// CreateFile creates a new file or directory
func (s *IDEService) CreateFile(path string, isDir bool) error {
	// Clean and validate path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check if already exists
	if _, err := os.Stat(absPath); err == nil {
		return fmt.Errorf("path already exists")
	}

	if isDir {
		// Create directory
		if err := os.MkdirAll(absPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	} else {
		// Ensure parent directory exists
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}
		// Create empty file
		if err := os.WriteFile(absPath, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
	}

	return nil
}

// DeleteFile deletes a file or directory
func (s *IDEService) DeleteFile(path string) error {
	// Clean and validate path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check if exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist")
	}

	// Remove file or directory
	if err := os.RemoveAll(absPath); err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	return nil
}

// IsBinaryFile checks if a file is binary and cannot be edited as text
// Rules:
// 1. Files with extensions are considered text (let DetectLanguage handle them)
// 2. Files without extension < 100KB without null bytes can be edited as text
// 3. Files without extension >= 100KB or containing null bytes are binary
func (s *IDEService) IsBinaryFile(filePath string) (bool, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false, fmt.Errorf("invalid path: %w", err)
	}

	// Files with extensions are not binary for our purposes
	if filepath.Ext(filePath) != "" {
		return false, nil
	}

	// Get file info for size check
	info, err := os.Stat(absPath)
	if err != nil {
		return false, fmt.Errorf("failed to stat file: %w", err)
	}

	// If >= 100KB, too large to edit as text
	if info.Size() >= 100*1024 {
		return true, nil
	}

	// Read first 512 bytes to detect null bytes
	file, err := os.Open(absPath)
	if err != nil {
		return false, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return false, fmt.Errorf("failed to read file: %w", err)
	}

	// Check for null bytes (binary indicator)
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true, nil
		}
	}

	// Small file without extension and without null bytes: treat as text
	return false, nil
}

// DetectLanguage detects the programming language based on file extension
func (s *IDEService) DetectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	languageMap := map[string]string{
		".go":         "go",
		".js":         "javascript",
		".ts":         "typescript",
		".jsx":        "javascript",
		".tsx":        "typescript",
		".py":         "python",
		".java":       "java",
		".c":          "c",
		".cpp":        "cpp",
		".cc":         "cpp",
		".cxx":        "cpp",
		".h":          "c",
		".hpp":        "cpp",
		".cs":         "csharp",
		".php":        "php",
		".rb":         "ruby",
		".rs":         "rust",
		".swift":      "swift",
		".kt":         "kotlin",
		".scala":      "scala",
		".sh":         "shell",
		".bash":       "shell",
		".zsh":        "shell",
		".fish":       "shell",
		".json":       "json",
		".xml":        "xml",
		".html":       "html",
		".htm":        "html",
		".css":        "css",
		".scss":       "scss",
		".sass":       "sass",
		".less":       "less",
		".yaml":       "yaml",
		".yml":        "yaml",
		".toml":       "toml",
		".ini":        "ini",
		".md":         "markdown",
		".sql":        "sql",
		".r":          "r",
		".m":          "objective-c",
		".mm":         "objective-cpp",
		".lua":        "lua",
		".vim":        "vim",
		".dockerfile": "dockerfile",
		".proto":      "protobuf",
		".graphql":    "graphql",
		".vue":        "vue",
		".svelte":     "svelte",
	}

	if lang, ok := languageMap[ext]; ok {
		return lang
	}

	// Special cases
	basename := filepath.Base(filePath)
	switch basename {
	case "Dockerfile":
		return "dockerfile"
	case "Makefile":
		return "makefile"
	case "CMakeLists.txt":
		return "cmake"
	}

	return "plaintext"
}
