# Litterbox Agent

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18-61DAFB?style=flat&logo=react)](https://react.dev/)
[![Vite](https://img.shields.io/badge/Vite-5-646CFF?style=flat&logo=vite)](https://vitejs.dev/)
[![Monaco Editor](https://img.shields.io/badge/Monaco_Editor-VS_Code-007ACC?style=flat&logo=visual-studio-code)](https://microsoft.github.io/monaco-editor/)
[![xterm.js](https://img.shields.io/badge/xterm.js-Terminal-000000?style=flat&logo=windowsterminal)](https://xtermjs.org/)
[![License](https://img.shields.io/badge/License-Apache2.0-green.svg?style=flat)](LICENSE)

**[中文](README.md) | EN**

Lightweight sandbox tool with Web IDE and RESTful API. Edit code, execute commands, and manage files in your browser, while providing complete automation interfaces for AI Agents. Designed for remote development and AI collaboration.

![Litterbox Agent](./docs/imgs/screenshot.png)

## Quick Start

### Build

```bash
# Build backend
go build -o litterbox-agent cmd/server/main.go

# Build frontend
cd front && npm install && npm run build
```

### Run

```bash
# Without authentication
./litterbox-agent

# With token authentication
export AGENT_TOKEN="your-secure-token"
./litterbox-agent
```

### Access

Browser: `http://localhost:22531`

URL parameters supported:
```
http://localhost:22531?dir=/path/to/directory
http://localhost:22531?dir=/home&file=/home/config.json
http://localhost:22531?token=xxxxxx&dir=/home&file=/home/config.json
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `22531` |
| `AGENT_TOKEN` | Access token (no auth if not set) | None |
| `DEFAULT_DIR` | Default directory to open | Current working directory |

## API Reference

> Include `X-Token` in request headers if authentication is enabled (for WebSocket, pass as URL query parameter `?token=xxx`)

### WebSocket Terminal Endpoint
```
WS /ws/terminal?rows=24&cols=80&token=xxxxx
```

### File Operations

```bash
# Get file tree
GET /api/tree?path=/path/to/directory

# Read file
GET /api/files?path=/path/to/file

# Create file/directory
POST /api/files
{"path": "/path/to/file", "is_dir": false}

# Save file
PUT /api/files
{"path": "/path/to/file", "content": "file content"}

# Delete file/directory
DELETE /api/files
{"path": "/path/to/file"}

# Upload file
POST /api/upload
Content-Type: multipart/form-data

# Download file
GET /api/download?path=/path/to/file
```

### Command Execution

```bash
POST /api/exec
{"command": "ls -la", "cwd": "/optional/working/directory"}
```

### Environment Variable Management

Dynamically manage environment variables, including `AGENT_TOKEN`, `DEFAULT_DIR`, etc.

```bash
# List all environment variables
GET /api/env

# Get specific environment variable
GET /api/env?key=AGENT_TOKEN

# Set environment variable
POST /api/env
{"key": "AGENT_TOKEN", "value": "new-token"}

# Set default directory
POST /api/env
{"key": "DEFAULT_DIR", "value": "/home/projects"}

# Delete environment variable
DELETE /api/env?key=AGENT_TOKEN
```

**Notes**:
- Changes to `AGENT_TOKEN` take effect immediately; subsequent requests must use the new token
- Changes to `DEFAULT_DIR` apply to newly opened pages
- Changes to `PORT` require service restart

### Agent File Operations

file operations for AI Agents, supporting precise editing and history rollback.

#### 1. View File Content (view)

```bash
# View entire file
POST /api/agent/file
{"command": "view", "path": "/path/to/file"}

# View specific line range (e.g., lines 1-10)
POST /api/agent/file
{"command": "view", "path": "/path/to/file", "view_range": [1, 10]}
```

#### 2. Create File (create)

```bash
POST /api/agent/file
{"command": "create", "path": "/path/to/file", "file_text": "file content"}
```

#### 3. String Replace (str_replace)

```bash
# Replace all occurrences in file
POST /api/agent/file
{"command": "str_replace", "path": "/path/to/file", "old_str": "old text", "new_str": "new text"}
```

#### 4. Insert Line (insert)

```bash
# Insert content after line 5
POST /api/agent/file
{"command": "insert", "path": "/path/to/file", "insert_line": 5, "new_str": "new line content"}
```

#### 5. Undo Edit (undo_edit)

```bash
# Undo last edit operation (up to 10 history records per file)
POST /api/agent/file
{"command": "undo_edit", "path": "/path/to/file"}
```

## License

Apache-2.0 license
