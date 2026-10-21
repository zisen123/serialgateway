# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

SerialGateway maps physical COM serial ports to SSH listening ports and exposes an HTTP REST API for management and history queries. Written in Go, Windows-primary (uses Windows toast notifications and COM ports).

## Commands

```powershell
# Build
go build -o serial-gateway.exe ./cmd/serial-gateway/

# Run (config defaults to serial-gateway.yaml)
.\serial-gateway.exe --config serial-gateway.yaml
.\serial-gateway.exe --http-port 8080   # override HTTP port

# Tests
go test ./...                            # all unit tests
go test ./internal/serial/...            # one package
go test ./internal/serial/ -run TestRingBuffer   # single test
go test ./tests/ -run TestHTTPAPIPortsEndpoint    # integration tests (spin up in-process gateway)

# Real-port / live-gateway tests — skip unless the gateway is running against a real COM port
go test ./tests/ -run TestRealPortSSHConnection   # needs SG_TEST_PORT / SG_TEST_SSH_PORT or defaults COM3/2203
go test ./tests/ -run TestSSHEchoImmediate       # needs gateway running on 2203
```

Unit/integration tests in `tests/` use an in-process virtual serial device (`tests/vserial`); the `TestRealPort*` and `TestSSHEcho*` tests skip gracefully if no live gateway/port is available.

## Architecture

Entry point: `cmd/serial-gateway/main.go` — loads config, initializes structured logger, creates a `core.PortManager`, optionally starts reverse SSH tunnels (`tunnel.ReverseTunnel`), then starts the HTTP gateway and waits on SIGINT.

### Port mapping rule
SSH port = `cfg.SSH.BasePort` + COM number (COM3 → 2203, COM25 → 2225). Computed in `internal/ssh.PortMapping`. This is the central invariant connecting config, SSH servers, and tests.

### Layered packages (all under `internal/`)

- **`config`** — YAML schema + `Load`/`applyDefaults`. Durations are parsed from strings (e.g. `5s`). Missing config file is not an error: defaults are applied and an empty config is returned.
- **`serial`** — the core I/O engine:
  - `SerialSession` (`session.go`) owns the open COM port. Concurrency model: a single `writeCh chan WriteRequest` serializes writes; a broadcast fan-out via `subscribers` (each subscriber is `chan string`) delivers read lines to all listeners (SSH clients, HTTP `/exec` SSE, recorder). A `RingBuffer` (`ringbuffer.go`) keeps recent history (line + timestamp + seq) bounded by `max_lines`/`max_bytes`. Reconnect logic with exponential backoff lives here, governed by `cfg.Reconnect`.
  - `port.go` enumerates COM ports via `go.bug.st/serial/enumerator`.
- **`ssh`** — per-device `SSHServer` (gliderlabs/ssh) wrapping a `SerialSession`. `handler.go` bridges the SSH session to the serial session: stdout from serial → SSH client, stdin from SSH client → serial `writeCh`. Host key is generated/loaded from `host_key` (ed25519).
- **`recorder`** — `FileRecorder` asynchronously persists raw serial bytes to `serial_logs/<device>`, with size cap + header-trim when exceeded. Fed as a subscriber to the session.
- **`core`** — `PortManager` coordinates the map of device → `SSHServer` (+ `FileRecorder`). `AutoStart` reads `cfg.Ports` and starts a server+recorder per device. This is what the HTTP handlers mutate for dynamic mapping create/delete.
- **`http`** — `Gateway` wires `net/http` routes (stdlib mux, no framework). `handlers.go` does routing by path-splitting under `/api/mappings/`; sub-paths `tail`, `log`, `write`, `exec` dispatch to dedicated handlers. `exec.go` implements SSE-style streaming command execution (subscribe → write → stream output until silence/timeout). `notify_handler.go` fronts the toast system.
- **`notify`** — Windows toast notifications via PowerShell + Win32. `Show` is a function variable so tests can mock it. Non-ASCII is XML-escaped to ASCII numeric references to avoid encoding issues across Go/PowerShell/XML.
- **`tunnel`** — `ReverseTunnel` opens an SSH reverse forward (`remote_port` → `target_host:local_port`) so a remote host can reach the gateway, ADB, or another LAN host. `target_host` defaults to `127.0.0.1`. In `mode: "ssh_proxy"`, the gateway terminates the inbound SSH connection with its own stable `host_key` and then dials the target as an SSH client, so remote known_hosts pins the gateway rather than the reflashing board. Three named instances can run: `reverse_tunnel` (gateway), `adb_reverse_tunnel` (ADB `5037`→`15037`), and `serial_ssh_tunnel` (all COM ports' SSH, forwards auto-derived from `ports`). For anything beyond those, use the `reverse_tunnels` list: an arbitrary number of extra tunnels, each a single forward by default, `kind: "serial_ssh"` to auto-derive per-COM-port forwards, or `kind: "local_forward"` (`LocalForward`, the SSH `-L` direction): listen on `bind_address:local_port` on this machine and forward each connection to `target_host:remote_port` dialed from the server side — used to reach server-only services (e.g. the opencli daemon on `19825`) from localhost without holding a manual SSH window. `main.go` collects the three named fields plus every list entry into one `[]tunnel.Tunnel` slice via `startReverseTunnel` (dispatches on `Kind`) and stops them all on shutdown.
- **`log`** — `slog` logger with lumberjack rotation; text/json format; stdout/file output.

### Data flow for a command
SSH client (or HTTP `/write`) → `SerialSession.writeCh` → serial port → read loop parses lines → `RingBuffer` + every `subscriber` channel → SSH handler / `/exec` SSE / `FileRecorder`.

## Config

`serial-gateway.yaml` is the live config. Key sections: `gateway.http_port`, `serial_defaults` (baudrate/timeout/`exec_silence_timeout`/`exec_max_duration`), `ssh.base_port` + `ssh.auth`, `ring_buffer`, `reconnect`, `reverse_tunnel` / `adb_reverse_tunnel`, `file_recording` (output dir, size/trim MB), `logging`, `ports` (list of `{device, baudrate}` to auto-start). `PUT /api/config` updates config at runtime.

## Notes

- `.gitignore` excludes `*.exe`, `host_key*`, `gateway.log`, `serial_logs/`, `.claude/worktrees/`. The committed `host_key` is an exception (note `host_key` is tracked but `host_key*` is ignored — only the exact `host_key` file is kept).
- Windows-only features: `internal/notify` (toast) and COM port access. Tests assume a Windows host for real-port tests.
- The `run-serialgateway` and `serialgateway` skills (in `.claude/skills/`) build/start the server and drive the HTTP API — prefer them over manual `go build` + `curl` when iterating.
