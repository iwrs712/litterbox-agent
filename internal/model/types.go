package model

// CommandRequest represents a command execution request
type CommandRequest struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"` // 工作目录，可选
}

// CommandResponse represents a command execution response
type CommandResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// Metrics represents system metrics
type Metrics struct {
	Uptime           string  `json:"uptime"`
	RequestCount     uint64  `json:"request_count"`
	CommandCount     uint64  `json:"command_count"`
	UploadCount      uint64  `json:"upload_count"`
	DownloadCount    uint64  `json:"download_count"`
	Goroutines       int     `json:"goroutines"`
	MemoryMB         uint64  `json:"memory_mb"`           // 进程使用内存（MB）
	CPUPercent       float64 `json:"cpu_percent"`         // CPU使用率（%）
	SystemMemoryMB   uint64  `json:"system_memory_mb"`    // 系统已使用内存（MB）
	SystemTotalMemMB uint64  `json:"system_total_mem_mb"` // 系统总内存（MB）
}

// FileOperationRequest represents a unified file operation request
type FileOperationRequest struct {
	Command    string `json:"command"`               // view, create, str_replace, insert, undo_edit
	Path       string `json:"path"`                  // 文件路径
	FileText   string `json:"file_text,omitempty"`   // create: 文件内容
	ViewRange  []int  `json:"view_range,omitempty"`  // view: [start_line, end_line]
	OldStr     string `json:"old_str,omitempty"`     // str_replace: 要替换的字符串
	NewStr     string `json:"new_str,omitempty"`     // str_replace/insert: 新字符串
	InsertLine int    `json:"insert_line,omitempty"` // insert: 插入位置
}

// FileOperationResponse represents a unified file operation response
type FileOperationResponse struct {
	Success bool   `json:"success"`
	Content string `json:"content,omitempty"` // view: 文件内容
	Message string `json:"message,omitempty"` // 操作结果消息
	Lines   int    `json:"lines,omitempty"`   // view: 总行数
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// FileTreeNode represents a node in the file tree
type FileTreeNode struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	IsDir    bool            `json:"is_dir"`
	Size     int64           `json:"size,omitempty"`
	Children []*FileTreeNode `json:"children,omitempty"`
}

// FileContentRequest represents a request to read file content
type FileContentRequest struct {
	Path string `json:"path"`
}

// FileContentResponse represents a file content response
type FileContentResponse struct {
	Content  string `json:"content"`
	Path     string `json:"path"`
	Language string `json:"language,omitempty"` // 文件语言类型，用于编辑器高亮
	Size     int64  `json:"size,omitempty"`     // 文件大小（字节）
	ModTime  string `json:"mod_time,omitempty"` // 修改时间
}

// FileSaveRequest represents a request to save file content
type FileSaveRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileSaveResponse represents a file save response
type FileSaveResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// FileInfoRequest represents a request to get file metadata
type FileInfoRequest struct {
	Path string `json:"path"`
}

// FileInfoResponse represents file metadata response
type FileInfoResponse struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mod_time"`
	Language string `json:"language,omitempty"`
}

// FileCreateRequest represents a request to create a file or directory
type FileCreateRequest struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// FileDeleteRequest represents a request to delete a file or directory
type FileDeleteRequest struct {
	Path string `json:"path"`
}

// FileOperationResult represents a generic file operation result
type FileOperationResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
