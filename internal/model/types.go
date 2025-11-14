package model

// CommandRequest represents a command execution request
type CommandRequest struct {
	Command string `json:"command"`
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
