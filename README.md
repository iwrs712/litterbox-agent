# Litterbox Agent

高性能、低占用的沙箱守护进程 通过web api与沙箱交互
## 功能

- 文件上传/下载
- 文件操作（查看、创建、编辑、撤销）
- 命令执行
- 性能指标监控


## 运行

```bash
# 编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64
go build -o litterbox-agent cmd/server/main.go
# 运行
./litterbox-agent

# 或直接运行
go run cmd/server/main.go
```

默认端口: 8080 (可通过环境变量 PORT 修改)

## API

### 1. 上传文件

```bash
POST /upload
Content-Type: multipart/form-data

# 示例
curl -X POST http://localhost:8080/upload \
  -F "file=@/path/to/file.txt" \
  -F "path=/tmp/uploads"
```

响应:
```json
{
  "status": "success",
  "path": "/tmp/uploads/file.txt"
}
```

### 2. 下载文件

```bash
GET /download?path={filepath}

# 示例
curl -OJ "http://localhost:8080/download?path=/tmp/uploads/file.txt"
```

### 3. 文件操作（统一接口）

支持多种文件操作命令

```bash
POST /file
Content-Type: application/json
```

#### 3.1 查看文件内容 (view)

```bash
# 查看整个文件
curl -X POST http://localhost:8080/file \
  -H "Content-Type: application/json" \
  -d '{"command":"view","path":"/tmp/test.txt"}'

# 查看指定行范围 (第1-10行)
curl -X POST http://localhost:8080/file \
  -H "Content-Type: application/json" \
  -d '{"command":"view","path":"/tmp/test.txt","view_range":[1,10]}'
```

响应:
```json
{
  "success": true,
  "content": "file content...",
  "lines": 100,
  "message": "Showing lines 1-10 of 100"
}
```

#### 3.2 创建文件 (create)

```bash
curl -X POST http://localhost:8080/file \
  -H "Content-Type: application/json" \
  -d '{"command":"create","path":"/tmp/new.txt","file_text":"Hello World"}'
```

响应:
```json
{
  "success": true,
  "message": "File created: /tmp/new.txt"
}
```

#### 3.3 字符串替换 (str_replace)

```bash
curl -X POST http://localhost:8080/file \
  -H "Content-Type: application/json" \
  -d '{"command":"str_replace","path":"/tmp/test.txt","old_str":"old text","new_str":"new text"}'
```

响应:
```json
{
  "success": true,
  "message": "Replaced 2 occurrence(s)"
}
```

#### 3.4 插入行 (insert)

```bash
# 在第5行后插入内容
curl -X POST http://localhost:8080/file \
  -H "Content-Type: application/json" \
  -d '{"command":"insert","path":"/tmp/test.txt","insert_line":5,"new_str":"new line content"}'
```

响应:
```json
{
  "success": true,
  "message": "Inserted line after line 5"
}
```

#### 3.5 撤销编辑 (undo_edit)

撤销上一次的编辑操作。每个文件最多保留10次编辑历史。

```bash
curl -X POST http://localhost:8080/file \
  -H "Content-Type: application/json" \
  -d '{"command":"undo_edit","path":"/tmp/test.txt"}'
```

响应:
```json
{
  "success": true,
  "message": "Edit undone successfully"
}
```

**注意**:
- 每个文件最多保留10次编辑历史
- 可以连续撤销多次（最多10次）
- 超过10次的旧历史会被自动删除

### 4. 执行命令

通过shell执行命令，支持管道、重定向、变量等所有shell特性

```bash
POST /exec
Content-Type: application/json

# 示例
curl -X POST http://localhost:8080/exec \
  -H "Content-Type: application/json" \
  -d '{"command":"ls -la | grep test"}'

curl -X POST http://localhost:8080/exec \
  -H "Content-Type: application/json" \
  -d '{"command":"touch file && echo created"}'
```

响应:
```json
{
  "stdout": "...",
  "stderr": "...",
  "exit_code": 0
}
```

### 5. 监控指标

```bash
GET /metrics

# 示例
curl http://localhost:8080/metrics
```

响应:
```json
{
  "uptime": "1h30m20s",
  "request_count": 1234,
  "command_count": 56,
  "upload_count": 12,
  "download_count": 34,
  "goroutines": 5,
  "memory_mb": 8
}
```
