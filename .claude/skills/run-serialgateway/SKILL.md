---
name: run-serialgateway
description: Build, run, smoke-test, and drive the SerialGateway HTTP API server. Use when asked to start the gateway, run the app, test the API, or verify the server is working.
---

# Run SerialGateway

Go-based serial port gateway that exposes COM ports over SSH with an HTTP
management API. Windows-only (COM port discovery, toast notifications).

Paths below are relative to the repo root.

## Prerequisites

- **Go 1.26+**
- **Windows** (COM port enumeration, toast notifications require WinRT)

## Build

```powershell
go build -o serial-gateway.exe ./cmd/serial-gateway/
```

## Run (agent path — driver)

The driver `.claude/skills/run-serialgateway/smoke.ps1` handles the full
lifecycle: build → start → smoke-test → stop.

```powershell
# Full cycle: build, start, test, stop
.claude/skills/run-serialgateway/smoke.ps1

# Skip rebuild (already built), keep running after tests
.claude/skills/run-serialgateway/smoke.ps1 -SkipBuild -KeepRunning

# Custom port
.claude/skills/run-serialgateway/smoke.ps1 -Port 18080
```

Individual functions (dot-source then call):

```powershell
. .claude/skills/run-serialgateway/smoke.ps1
Build-Gateway
Start-Gateway
Test-GetPorts
Test-GetMappings
Test-PostNotify
Test-HealthCheck
Stop-Gateway
```

### Smoke test coverage

| Endpoint | Method | What it checks |
|---|---|---|
| `/api/ports` | GET | Serial port enumeration, active status |
| `/api/mappings` | GET | Port-to-SSH mappings |
| `/api/config` | GET | Current configuration |
| `/api/notify` | POST | Windows toast notification dispatch |

### Quick API calls (server already running)

```powershell
# List ports
Invoke-RestMethod http://localhost:18080/api/ports | ConvertTo-Json

# List mappings
Invoke-RestMethod http://localhost:18080/api/mappings | ConvertTo-Json

# Get config
Invoke-RestMethod http://localhost:18080/api/config | ConvertTo-Json

# Send toast notification
Invoke-RestMethod -Uri http://localhost:18080/api/notify `
  -Method Post `
  -Body '{"title":"Hello","content":"Test message"}' `
  -ContentType "application/json"

# Write data to a serial port
Invoke-RestMethod -Uri http://localhost:18080/api/mappings/COM3/write `
  -Method Post `
  -Body '{"data":"AT\r\n"}' `
  -ContentType "application/json"

# Stream command output (SSE)
Invoke-WebRequest -Uri "http://localhost:18080/api/mappings/COM3/exec?cmd=AT%0D" `
  -Method Get
```

## Run (human path)

```cmd
:: Start (hidden window)
cscript //nologo run.vbs

:: Stop
stop.bat

:: Restart — full flow: stop → clean stale server tunnel sessions → rebuild → start → verify
restart.bat

:: Check status
status.bat
```

The executable reads `serial-gateway.yaml` from the working directory by
default. Override with `--config <path>` and `--http-port <port>`.

`restart.bat` wraps `restart-gateway.ps1`, which:
1. Recovers and stops the live process.
2. Runs `clean-tunnel-ports.ps1` — kills the server-side sshd sessions left
   half-open by the old process (they hold the remote ports and make every
   tunnel rebind fail with `ssh: tcpip-forward request denied by peer`), plus
   stray non-session port holders (e.g. an auto-started `adb` server on 15037).
   `-SkipClean` skips this when the server is unreachable.
3. Rebuilds `serial-gateway.exe` (`-SkipBuild` to skip) and restarts it.
4. Verifies HTTP, every remote forward in `gateway.log`, and the per-COM SSH
   listeners; exits non-zero with the failing ports on any miss.

Both scripts derive the tunnel host/ports from the config via
`cmd/tunnel-ports` (built on demand into `tunnel-ports.exe`, gitignored) — the
same `internal/config` parser the gateway itself uses. `clean-tunnel-ports.ps1`
also runs standalone (`-DryRun` to preview kills). It only ever kills the SSH
user's own `sshd: <user>@notty` sessions (tunnel-style; interactive `@pts`
shells are never touched) and the probe connection shields itself by parent
chain. Without root there is no way to attribute a session to a client IP (the
session socket belongs to privileged sshd; `/proc` fd/environ are blocked), so
with the gateway running it may kill live tunnel sessions — they re-establish
within one retry interval (≤30s). In the restart flow the gateway is stopped
first, so every `@notty` session killed is stale by definition.

## Test

```powershell
go test ./...
```

## Direct invocation (bypass server)

For PRs that only touch internal logic without changing HTTP surface, import
and call packages directly without launching the full server:

```go
// Serial port enumeration
ports, _ := serial.ListPorts()
for _, p := range ports {
    fmt.Println(p.Device, p.Description)
}

// Config loading
cfg, _ := config.Load("serial-gateway.yaml")

// Ring buffer
rb := serial.NewRingBuffer(1000, 1<<20)
rb.Append("line 1")
fmt.Println(rb.Tail(10))
```

Run with `go run`:

```powershell
go run ./cmd/serial-gateway/ --config serial-gateway.yaml --http-port 18080
```

## Gotchas

- **Windows-only**: COM port discovery uses the Windows registry. Toast
  notifications use WinRT APIs. Will not work on Linux/macOS.
- **Integration test is stale**: `tests/integration_test.go` calls
  `core.NewPortManager(cfg)` and `sgwhttp.NewGatewayWithManager(cfg, pm)`
  but both now require a `*slog.Logger` third argument. The internal
  package tests all compile and pass.
- **Stop-before-start**: The server binds a fixed port. If it's already
  running, a second instance will fail to bind. The smoke driver handles
  this by checking for an existing process first.
- **Server uses VBS launcher**: The human path (`run.vbs`) starts the
  exe hidden (window style 0). For agent use, the driver starts it
  directly with `WScript.Shell.Run` instead so stdout/stderr are
  accessible.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `go build` fails with "no Go files" | Build from repo root: `go build -o serial-gateway.exe ./cmd/serial-gateway/` |
| `bind: address already in use` | Kill existing: `taskkill /f /im serial-gateway.exe` |
| `/api/ports` returns empty list | Expected on machines without physical/virtual COM ports. Not an error. |
| Toast notification not showing | Toast notifications require a Windows interactive session. Works in RDP/console, not in headless service accounts. |
