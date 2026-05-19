# SerialGateway Design

**Date:** 2026-05-19
**Status:** Draft

## Overview

SerialGateway is a Go-based serial port gateway that maps physical serial ports (COM3, COM4, etc.) to SSH listening ports (2203, 2204, etc.). Humans and AI agents connect via standard SSH clients for transparent serial interaction, while a lightweight HTTP REST API provides structured management and query capabilities — including historical log retrieval for agents who read data in discrete segments rather than streams.

## Core Requirements

1. **SSH port mapping:** Each serial port maps to an SSH port following the rule `COMn → 2200+n` (COM3→2203, COM4→2204, COM8→2208).
2. **Transparent interaction:** SSH sessions directly bridge to serial port I/O — keyboard input goes to serial write, serial output appears on the terminal.
3. **Shared read + queued write:** Multiple SSH connections to the same serial port all receive read output (broadcast), while writes are serialized through a queue to prevent data corruption.
4. **Serial disconnect resilience:** When a serial port disconnects (e.g., cable bumped), SSH sessions stay open, display a status message, and auto-reconnect when the port becomes available again. Input during disconnection is discarded.
5. **HTTP management API + history query:** Lightweight REST API for listing ports, viewing mappings, configuring parameters, and querying per-port ring buffer history (tail/log endpoints) for AI agents.
6. **N serial ports:** Supports dynamic management of multiple serial ports with different baudrates and settings.

## Architecture

```
┌─────────────────────────────────────────────┐
│           SerialGateway (Go binary)          │
│                                              │
│  ┌──────────────┐    ┌────────────────────┐  │
│  │ PortManager   │───▶│ HTTP API :8080     │  │
│  │ (core)        │    │                    │  │
│  │               │    │ GET /api/ports      │  │
│  │ - discover    │    │ GET /api/mappings   │  │
│  │ - lifecycle   │    │ POST /api/mappings  │  │
│  │ - mappings    │    │ DEL /api/mappings   │  │
│  └───────┬───────┘    │ GET .../tail        │  │
│          │             │ GET .../log         │  │
│     ┌────┴────┐       │ GET/PUT /api/config │  │
│     │ Serial  │       └────────────────────┘  │
│     │ Sessions│                                │
│     │ + Ring  │                                │
│     │ Buffers │                                │
│     │ COM3    │                                │
│     │ COM4    │                                │
│     └────┬────┘                                │
│          │                                     │
│  ┌───────┴───────┐                             │
│  │ SSH Servers    │                             │
│  │ COM3→:2203     │                             │
│  │ COM4→:2204     │                             │
│  └───────────────┘                             │
└─────────────────────────────────────────────┘

External access:
  Human:   ssh -p 2203 user@localhost → COM3 transparent interaction
  Agent:   ssh (same as human)        → programmatic serial interaction
  Agent:   HTTP GET /api/ports        → list ports and mappings
  Agent:   HTTP GET /api/mappings/COM3/tail?lines=500 → read history
```

## Components

### PortManager

Central coordinator responsible for:
- Discovering available serial ports on the system
- Managing serial port sessions (open, close, reconnect)
- Mapping serial ports to SSH server instances
- Tracking connection counts and session state

### SerialSession

Per-port session handling:
- Maintains the physical `serial.Port` connection
- Maintains an in-memory ring buffer of recent serial output (for history queries)
- Broadcasts read data to all attached SSH connections
- Queues write requests through a Go channel, processing one at a time
- Handles disconnect/reconnect: detects serial errors, enters reconnect loop, resumes when port reappears
- During disconnection: discards incoming write requests, sends status messages to SSH clients

### SSHServer

Per-port SSH listener:
- One `gliderlabs/ssh` server instance per active serial port
- Port assignment: `2200 + COM number` (COM3→2203)
- Authentication: configurable password or public key (default: simple password)
- On connection: creates a handler that bridges stdin→serial write, serial read→stdout
- Multiple concurrent SSH connections share the same SerialSession

### HTTP API

Lightweight management interface using standard `net/http`:
- No external web framework needed (few endpoints, simple logic)
- JSON request/response format
- Middleware for structured logging and error handling

## SSH Port Mapping & Concurrency Model

### Connection Flow

1. User connects: `ssh -p 2203 user@localhost`
2. SSH server authenticates (password/public key per config)
3. Session established in transparent mode: keystrokes → serial write, serial output → terminal
4. Disconnect: SSH session closes gracefully; serial port stays open if other connections exist

### Port Mapping Rules

- Mapping rule: `COMn → port 2200+n`
- Configured ports auto-start their SSH servers on gateway launch
- Unconfigured/discovered ports remain inactive until manually activated via API

### Shared Read + Queued Write

- **Read broadcasting:** Serial output is sent to all SSH connections attached to that port
- **Write serialization:** Write requests enter a buffered channel (queue), processed one at a time
- **Write timeout:** If a serial write blocks beyond a configurable threshold, the operation is canceled and the sender receives an error notification
- **Disconnect discard:** During serial disconnection, incoming write requests are dropped silently

## Serial Disconnect Resilience

When a serial port disconnects (cable unplugged, device reboot):

1. SSH sessions remain open — not closed
2. SSH terminal displays: `[serial disconnected - waiting for reconnect...]`
3. Gateway enters reconnect loop with exponential backoff (1s → 2s → 4s → ... → 30s max)
4. Keyboard input during disconnection is discarded
5. When serial port reappears: gateway auto-reconnects, SSH terminal displays `[serial reconnected]`
6. Read broadcasting resumes to all SSH sessions

## HTTP Management API

The HTTP API serves two roles: port/mapping management for both humans and agents, and structured query access (especially historical log retrieval) for AI agents. Agents read serial data in discrete segments, not streams, so they rely on the HTTP API for querying past serial output rather than consuming a live stream.

The gateway maintains a per-port ring buffer of recent serial output (configurable size, default 64KB or ~500 lines). Agents query this buffer via the `tail` and `log` endpoints.

### Endpoints

```
GET    /api/ports                    List available serial ports, mapping status, baudrate
GET    /api/mappings                 List active SSH port mappings with connection counts
POST   /api/mappings                 Create mapping for a serial port (start SSH server)
DELETE /api/mappings/:device         Remove mapping and stop SSH server for a device (e.g. COM3)
GET    /api/mappings/:device/tail    Read last N lines from serial port history buffer (for agents)
GET    /api/mappings/:device/log     Read full serial port history buffer in text or JSONL format
GET    /api/config                   View gateway configuration
PUT    /api/config                   Update gateway configuration
```

### GET /api/ports Response

```json
{
  "ports": [
    {
      "device": "COM3",
      "description": "USB Serial Device",
      "hwid": "USB VID:PID...",
      "baudrate": 115200,
      "ssh_port": 2203,
      "status": "active"
    },
    {
      "device": "COM5",
      "description": "...",
      "hwid": "...",
      "baudrate": 115200,
      "ssh_port": null,
      "status": "inactive"
    }
  ]
}
```

### GET /api/mappings Response

```json
{
  "mappings": [
    {
      "serial_port": "COM3",
      "ssh_port": 2203,
      "connections": 2,
      "baudrate": 115200,
      "connected": true
    },
    {
      "serial_port": "COM8",
      "ssh_port": 2208,
      "connections": 0,
      "baudrate": 115200,
      "connected": false
    }
  ]
}
```

### GET /api/mappings/:device/tail Response

```
GET /api/mappings/COM3/tail?lines=200
```

```json
{
  "device": "COM3",
  "lines": 200,
  "count": 150,
  "items": [
    {"ts": "2026-05-19T10:00:01Z", "seq": 1, "line": "root@cvitek:~#"},
    {"ts": "2026-05-19T10:00:03Z", "seq": 2, "line": "reboot"}
  ]
}
```

### GET /api/mappings/:device/log Response

```
GET /api/mappings/COM3/log?format=jsonl
GET /api/mappings/COM3/log?format=text
```

```json
{
  "device": "COM3",
  "format": "jsonl",
  "bytes": 2048,
  "content": "{\"ts\":\"...\",\"seq\":1,\"line\":\"...\"}\n{\"ts\":\"...\",\"seq\":2,\"line\":\"...\"}\n"
}
```

## Ring Buffer (History Buffer)

Each SerialSession maintains an in-memory ring buffer of recent serial output:

- Default size: configurable (64KB or ~500 lines, whichever limit is hit first)
- Entries are structured: `{ts, seq, line, bytes}`
- Written to on every serial read line (same data broadcast to SSH connections)
- Queried via HTTP API `tail` and `log` endpoints
- On serial disconnect/reconnect: buffer persists, new data appends after reconnect marker
- Buffer does not persist across gateway restarts (in-memory only)

## Protocol Summary

| Interface | Target | Mode | Use Case |
|-----------|--------|------|----------|
| SSH port mapping | Humans + Agents | Transparent stream | Real-time terminal interaction, live monitoring |
| HTTP REST API | Agents (primary) | Structured query | List ports, check mappings, read history logs, configure settings |
| No WebSocket | — | — | Agents read in segments, not streams; SSH covers streaming needs |

### Config File (YAML)

```yaml
# serial-gateway.yaml
gateway:
  http_port: 8080

serial_defaults:
  baudrate: 115200
  timeout: 5s
  bytesize: 8
  parity: "N"
  stopbits: 1
  write_timeout: 10s

ring_buffer:
  max_lines: 500
  max_bytes: 65536

ssh:
  base_port: 2200
  auth:
    type: "password"
    password: "serial"
    # public_keys: ["~/.ssh/id_rsa.pub"]

reconnect:
  initial_interval: 1s
  max_interval: 30s
  discard_input_on_disconnect: true

ports:
  - device: "COM3"
    baudrate: 115200
  - device: "COM4"
    baudrate: 9600
  # Ports not listed use serial_defaults
```

### CLI Flags

```
serial-gateway serve [--config path] [--http-port 8080] [--log-level info]
```

### Startup Behavior

- Scan configured ports, create SSH servers for each available one
- Unavailable configured ports: SSH server not started, status "inactive"
- Unconfigured ports: not mapped, can be activated via POST /api/mappings
- Port discovery via API reflects current system state

## Error Handling & Logging

- **Serial read/write errors:** Display `[serial error: <message>]` on SSH terminal, keep session open
- **SSH disconnect:** Clean up that connection's write queue slot, broadcast continues for others
- **HTTP errors:** Standard HTTP status codes + JSON error body
- **Logging:** Structured JSON to stdout, configurable via `--log-level` (debug/info/warn/error)

## Go Project Structure

```
SerialGateway/
  cmd/
    serial-gateway/
      main.go              # Entry point, CLI commands
  internal/
    config/
      config.go            # Config loading, YAML parsing
    serial/
      port.go              # Port discovery, enumeration
      session.go           # SerialSession (shared read, queued write)
      ringbuffer.go        # Per-port ring buffer for history log
      reconnect.go         # Disconnect/reconnect logic
    ssh/
      server.go            # SSH server lifecycle (per port)
      handler.go           # SSH session handler (stdin→serial, serial→stdout)
    http/
      server.go            # HTTP server setup
      handlers.go          # API route handlers
      middleware.go         # Logging & error middleware
  go.mod
  go.sum
  serial-gateway.yaml      # Default config
```

### Key Dependencies

- `github.com/gliderlabs/ssh` — SSH server implementation
- `go.bug.st/serial` — Cross-platform serial port library (good Windows support)
- Go standard library `net/http` — HTTP API (no external framework needed)

## Testing Strategy

- Unit tests for SerialSession (shared read, queued write, reconnect) with mocked serial ports
- Unit tests for SSH handler logic
- HTTP API endpoint tests using `net/http/httptest`
- Integration test: start gateway, connect via SSH, verify bidirectional data flow
- Mock serial ports for all tests (no hardware dependency)

## Out of Scope (YAGNI)

- Protocol-level parsing (AT commands, Modbus, etc.) — transparent byte forwarding only
- Web UI — SSH + HTTP API only
- WebSocket streaming interface — not needed; SSH covers streaming for humans, HTTP covers structured queries for agents
- MCP protocol integration — can be added later if needed
- Windows service installer — future enhancement
- TLS/mTLS for HTTP API — local gateway, not needed initially
- Ring buffer persistence across gateway restarts — in-memory only for now