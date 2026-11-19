# Running envd on Kata workloads

Kata does not provide a runtime setting that copies an arbitrary binary into an
application container and starts it in that container's namespaces. Mount the
static envd binary into the main container and start it with a Kubernetes
`postStart` hook:

```yaml
volumes:
  - name: envd
    hostPath:
      path: /opt/litterbox/bin/envd
      type: File
containers:
  - name: main
    volumeMounts:
      - name: envd
        mountPath: /opt/litterbox/bin/envd
        readOnly: true
    lifecycle:
      postStart:
        exec:
          command:
            - /opt/litterbox/bin/envd
            - daemon
            - --host
            - 0.0.0.0
            - --port
            - "49983"
            - --filesystem-root
            - /
```

Build and install the static binary on every eligible amd64 node:

```bash
make build-linux-amd64
sudo install -D -m 0755 bin/envd-linux-amd64 /opt/litterbox/bin/envd
```

Enable injection in the Orchestrator configuration:

```toml
[envd]
enabled = true
host_binary_path = "/opt/litterbox/bin/envd"
container_binary_path = "/opt/litterbox/bin/envd"
port = 49983
connect_timeout_seconds = 3.0
```

The binary must exist on every schedulable Kata node before injection is
enabled. The `daemon` command starts a detached server, so the `postStart` hook
does not block container startup. The daemon remains in the main container's
PID, mount, network namespace, and cgroup.

The hook runs with the main container's configured user. Images using a
non-root `USER` only expose the files and processes available to that user.

## Image-only alternative

Use an init container to copy the binary into an `emptyDir`, mount that
directory into the main container, and keep the same `postStart` command. The
init container exits after copying the binary and does not remain running.
