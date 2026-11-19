# envd

`envd` is the sandbox data-plane daemon used by Litterbox. It provides an
E2B-compatible filesystem and process API inside a running sandbox container.

The daemon is intentionally small: it owns the sandbox data plane, while
authentication, sandbox lifecycle, and service discovery remain the
responsibility of the Litterbox Orchestrator.

## Features

- HTTP file read/write endpoints under `/files`
- ConnectRPC, gRPC, and gRPC-Web filesystem services
- ConnectRPC, gRPC, and gRPC-Web process services, including PTY streams
- Workspace restore and synchronization hooks
- Skills provisioning support
- A detached `daemon` mode for container lifecycle hooks

## Requirements

- Go 1.25.1 or newer
- `buf`, `protoc-gen-go`, and `protoc-gen-connect-go` for regenerating protobuf
  clients
- Docker with Buildx for container and node-installer builds

## Quick start

```bash
make test
make build
./bin/envd server \
  --host 127.0.0.1 \
  --port 49983 \
  --filesystem-root /
```

Verify the service:

```bash
curl -fsS http://127.0.0.1:49983/health
curl -fsS -X POST \
  -H 'Content-Type: application/json' \
  --data '{}' \
  http://127.0.0.1:49983/v1/workspace/restore
curl -fsS -X POST \
  -H 'Content-Type: application/octet-stream' \
  --data-binary 'hello' \
  'http://127.0.0.1:49983/files?path=/tmp/envd-hello.txt'
curl -fsS 'http://127.0.0.1:49983/files?path=/tmp/envd-hello.txt'
```

To run the server detached, so a Kubernetes `postStart` hook can return
immediately:

```bash
./bin/envd daemon --host 0.0.0.0 --port 49983 --filesystem-root /
```

Daemon output is written to `/tmp/envd.log` by default.

## Configuration

| CLI flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--host` | - | `0.0.0.0` | Listen address |
| `--port` | - | `49983` | Listen port |
| `--filesystem-root` | `ENVD_FILESYSTEM_ROOT` | `/` | Filesystem exposed to clients |
| `--workspace-root` | `ENVD_WORKSPACE_ROOT` | - | Workspace-only compatibility mode |
| - | `ENVD_DAEMON_LOG` | `/tmp/envd.log` | Detached daemon log path |

When `--filesystem-root` is set, it takes precedence over `--workspace-root`.
The default configuration exposes the container root filesystem, so run envd
only inside the intended sandbox boundary.

## APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/` | Service information |
| `GET` | `/health` | Health and workspace readiness |
| `GET` | `/files?path=...` | Read a file |
| `POST` | `/files?path=...` | Write a file |
| Connect/gRPC | `/filesystem.Filesystem/*` | Filesystem operations |
| Connect/gRPC | `/process.Process/*` | Process and PTY operations |

The protobuf contracts are in [`proto/`](./proto). Generated Go bindings are
committed under `internal/envd/proto/` so consumers can build without running
protobuf generation.

Regenerate the bindings after changing a `.proto` file:

```bash
make generate
```

## Build and test

```bash
make fmt
make vet
make test
make build
make build-linux-amd64
```

`make build` writes a host binary to `bin/envd`.
`make build-linux-amd64` writes a static Linux amd64 binary to
`bin/envd-linux-amd64`.

Build the local runtime image:

```bash
docker build -t envd .
docker run --rm -p 49983:49983 -v "$PWD:/workspace" envd
```

Build the node-installer image used to place the binary on Kata nodes:

```bash
make build-node-installer
```

See [`deploy/kata/README.md`](./deploy/kata/README.md) for the Kubernetes
`hostPath` plus `postStart` deployment pattern.

## Project layout

```text
cmd/envd/                CLI entry point
internal/envd/           filesystem and process protocol handlers
internal/fsapi/          filesystem boundary and path validation
internal/processes/      process and PTY lifecycle management
internal/server/         HTTP routing and compatibility endpoints
proto/                   protobuf source contracts
deploy/                  container and Kata deployment examples
scripts/                 operational smoke tests
```

## Security

`envd` does not implement end-user authentication. Bind it to a private
network interface and keep the Litterbox Orchestrator or equivalent gateway as
the authentication boundary. Do not expose port `49983` directly to untrusted
networks.

## License

The project is licensed under the repository-level [MIT License](../LICENSE).
