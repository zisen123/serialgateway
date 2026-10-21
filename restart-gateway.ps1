# restart-gateway.ps1 — full restart flow for the live SerialGateway:
#
#   stop → clean stale server sessions → rebuild → start → verify
#
# The clean step (clean-tunnel-ports.ps1) kills the server-side sshd sessions
# left half-open by the old gateway process; without it the new gateway's
# reverse tunnels fail with "tcpip-forward request denied by peer" until the
# stale sessions time out (which may be never).
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File restart-gateway.ps1
#   ... -SkipBuild    # don't rebuild serial-gateway.exe (binary is current)
#   ... -SkipClean    # don't touch the server (e.g. no network to it)
#   ... -Config serial-gateway.yaml
#
# Exit codes: 0 = gateway up and verified, 1 = failure (see output).

param(
    [string]$Config = "serial-gateway.yaml",
    [switch]$SkipBuild,
    [switch]$SkipClean
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $RepoRoot

function Write-Step { param([string]$m) Write-Host "`n=== $m" -ForegroundColor Cyan }
function Write-Ok   { param([string]$m) Write-Host "  OK: $m" -ForegroundColor Green }
function Write-Bad  { param([string]$m) Write-Host "  FAIL: $m" -ForegroundColor Red }

# ── 1. recover how the live process was started (before killing it) ──
Write-Step "Stopping old gateway"
$live = Get-Process -Name "serial-gateway" -ErrorAction SilentlyContinue
if ($live) {
    foreach ($p in $live) {
        $cli = (Get-CimInstance Win32_Process -Filter "ProcessId = $($p.Id)").CommandLine
        Write-Host "  stopping PID $($p.Id): $cli"
    }
    $live | Stop-Process -Force
    foreach ($p in $live) {
        for ($i = 0; $i -lt 20; $i++) {
            if (-not (Get-Process -Id $p.Id -ErrorAction SilentlyContinue)) { break }
            Start-Sleep -Milliseconds 250
        }
    }
    Write-Ok "old gateway stopped"
} else {
    Write-Host "  not running"
}

# ── 2. clear stale server-side tunnel sessions ───────────────────────
if (-not $SkipClean) {
    Write-Step "Cleaning stale tunnel sessions on the server"
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $RepoRoot "clean-tunnel-ports.ps1") -Config $Config
    if ($LASTEXITCODE -ne 0) {
        Write-Bad "port cleanup reported held ports — tunnels may fail to rebind; continuing (gateway retry will keep trying)"
    } else {
        Write-Ok "server ports clear"
    }
} else {
    Write-Step "Cleaning skipped (-SkipClean)"
}

# ── 3. rebuild (exe is unlocked now, if it was running) ──────────────
if (-not $SkipBuild) {
    Write-Step "Rebuilding serial-gateway.exe"
    $out = go build -o serial-gateway.exe ./cmd/serial-gateway/ 2>&1
    if ($LASTEXITCODE -ne 0) { Write-Bad "build failed: $out"; exit 1 }
    Write-Ok "build succeeded"
} else {
    Write-Step "Build skipped (-SkipBuild)"
}

# ── 4. start ─────────────────────────────────────────────────────────
Write-Step "Starting gateway"
Start-Process -FilePath (Join-Path $RepoRoot "serial-gateway.exe") `
    -ArgumentList @("--config", $Config) `
    -WorkingDirectory $RepoRoot -PassThru -WindowStyle Hidden | ForEach-Object {
    Write-Ok "started PID $($_.Id)"
}

# ── 5. verify ────────────────────────────────────────────────────────
$tunnelPorts = & (Join-Path $RepoRoot "tunnel-ports.exe") -config (Join-Path $RepoRoot $Config) | ConvertFrom-Json
$httpPort = $tunnelPorts.http_port
$logFile = if ($tunnelPorts.log_file) { $tunnelPorts.log_file } else { "gateway.log" }
$logPath = if ([IO.Path]::IsPathRooted($logFile)) { $logFile } else { Join-Path $RepoRoot $logFile }

Write-Step "Waiting for HTTP :$httpPort"
$ok = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $null = Invoke-RestMethod "http://localhost:$httpPort/api/config" -TimeoutSec 2
        $ok = $true; break
    } catch { Start-Sleep -Milliseconds 500 }
}
if (-not $ok) { Write-Bad "HTTP API did not come up on :$httpPort"; exit 1 }
Write-Ok "HTTP API up on :$httpPort"

# tunnel reconnects happen on their own schedule (retry <=30s); give them time
Write-Step "Waiting for reverse tunnels to establish (up to 75s)"
$expected = @{}
foreach ($t in $tunnelPorts.tunnels) {
    if ($t.remote_ports) { foreach ($p in $t.remote_ports) { $expected["$p"] = $t.name } }
}
$deadline = (Get-Date).AddSeconds(75)
$pending = @($expected.Keys)
while ($pending.Count -gt 0 -and (Get-Date) -lt $deadline) {
    Start-Sleep -Seconds 5
    if (-not (Test-Path $logPath)) { continue }
    $tail = Get-Content $logPath -Tail 120
    # regex, NOT -SimpleMatch: match "remote=<addr>:<port>" log lines
    $pending = @($pending | Where-Object { $p = $_; -not ($tail -match "remote=\S+:$p(\s|$)") })
}
if ($pending.Count -eq 0) {
    Write-Ok "all $($expected.Count) remote forwards established"
} else {
    Write-Bad "tunnels not confirmed for ports: $($pending -join ', ')"
    Write-Host "  (check $logPath for 'forwarding' / 'reconnect failed' lines)"
    exit 1
}

# per-COM SSH servers
Write-Step "Checking serial-SSH servers"
$comPorts = @(& (Join-Path $RepoRoot "tunnel-ports.exe") -config (Join-Path $RepoRoot $Config) | ConvertFrom-Json `
    | ForEach-Object { $_.tunnels } | Where-Object { $_.name -eq 'serial_ssh_tunnel' } | ForEach-Object { $_.remote_ports })
if ($comPorts.Count -gt 0) {
    $listening = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -in $comPorts }
    $missing = @($comPorts | Where-Object { $p = $_; -not ($listening | Where-Object { $_.LocalPort -eq $p }) })
    if ($missing.Count -eq 0) { Write-Ok "serial-SSH listening on $($comPorts -join ', ')" }
    else { Write-Bad "serial-SSH missing listeners: $($missing -join ', ')"; exit 1 }
} else {
    Write-Host "  no serial_ssh_tunnel configured"
}

Write-Step "Restart complete"
exit 0
