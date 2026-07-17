# Safe side-by-side debug driver for SerialGateway.
#
# Automates the SerialGateway-debug workflow: detect/avoid port and process
# conflicts, build a separate serial-gateway-test.exe, start isolated
# instances from the dedicated test YAMLs, smoke-test their HTTP ports, and
# optionally stop them — all without touching the live gateway.
#
# Usage (run from repo root, or pass -RepoRoot):
#   powershell -NoProfile -ExecutionPolicy Bypass -File debug.ps1 -Action all
#   powershell -NoProfile -ExecutionPolicy Bypass -File debug.ps1 -Action start -Config lan
#   powershell -NoProfile -ExecutionPolicy Bypass -File debug.ps1 -Action stop
#
# Dot-source to use individual functions:
#   . .\debug.ps1; Invoke-ConflictCheck; Start-TestGateway -Config ssh

param(
    [ValidateSet('all','conflict','build','start','smoke','stop','status')]
    [string]$Action = 'all',

    [ValidateSet('ssh','lan','both')]
    [string]$Config = 'both',

    [switch]$SkipBuild,
    [switch]$KeepRunning,

    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
)

$ErrorActionPreference = 'Stop'

# ── Paths and constants ──────────────────────────────────────────────
$TestExeName   = 'serial-gateway-test.exe'
$TestProcName  = 'serial-gateway-test'        # process name (no extension)
$TestExePath   = Join-Path $RepoRoot $TestExeName
$PidFile       = Join-Path $RepoRoot '.debug-pids.json'

# Config name -> (yaml, http_port, ssh_base_port, remote_port, log_file)
$ConfigMap = [ordered]@{
    ssh  = [pscustomobject]@{ Yaml='serial-gateway-test-ssh.yaml';     HttpPort=18081; SshBase=32000; RemotePort=10022; Log='gateway-test-ssh.log' }
    lan  = [pscustomobject]@{ Yaml='serial-gateway-test-lan-ssh.yaml'; HttpPort=18082; SshBase=32100; RemotePort=10023; Log='gateway-test-lan-ssh.log' }
}

# Ports the live gateway typically owns (from serial-gateway.yaml).
$LivePorts = @(18080, 2200, 5037, 15037)
# Ports the live process name uses.
$LiveProcName = 'serial-gateway'

# ── Helpers ──────────────────────────────────────────────────────────
function Write-Step { param([string]$Message) Write-Host "=== $Message" -ForegroundColor Cyan }
function Write-Pass { param([string]$Message) Write-Host "  PASS: $Message" -ForegroundColor Green }
function Write-Warn { param([string]$Message) Write-Host "  WARN: $Message" -ForegroundColor Yellow }
function Write-Fail { param([string]$Message) Write-Host "  FAIL: $Message" -ForegroundColor Red }

function Get-ListeningPorts {
    try {
        Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
            Select-Object -ExpandProperty LocalPort -Unique
    } catch { @() }
}

function Get-ConfigList {
    if ($Config -eq 'both') { @('ssh','lan') } else { @($Config) }
}

function Read-PidFile {
    if (Test-Path $PidFile) {
        try {
            $data = Get-Content $PidFile -Raw | ConvertFrom-Json
            if ($data) { return $data }
        } catch { }
    }
    return [pscustomobject]@{ ssh = $null; lan = $null }
}

function Write-PidFile {
    param($Pids)
    $Pids | ConvertTo-Json -Depth 3 | Set-Content -Path $PidFile -Encoding utf8
}

# ── Actions ──────────────────────────────────────────────────────────

function Invoke-ConflictCheck {
    Write-Step "Checking for conflicts with the live gateway"
    $conflict = $false

    $live = Get-Process -Name $LiveProcName -ErrorAction SilentlyContinue
    if ($live) {
        Write-Warn "Live gateway process '$LiveProcName' is running (PID $($live.Id -join ', '))"
    } else {
        Write-Pass "No live '$LiveProcName' process detected"
    }

    $listening = Get-ListeningPorts
    foreach ($p in $LivePorts) {
        if ($listening -contains $p) {
            Write-Warn "Live port $p is LISTENING (owned by the normal gateway)"
        }
    }

    foreach ($name in (Get-ConfigList)) {
        $cfg = $ConfigMap[$name]
        foreach ($p in @($cfg.HttpPort, $cfg.SshBase, $cfg.RemotePort)) {
            if ($listening -contains $p) {
                Write-Fail "Test port $p for config '$name' is already in use — pick another"
                $conflict = $true
            }
        }
    }

    $testProcs = Get-Process -Name $TestProcName -ErrorAction SilentlyContinue
    if ($testProcs) {
        Write-Warn "Existing test instances running (PID $($testProcs.Id -join ', ')) — stop with -Action stop"
    }

    if ($conflict) {
        throw "Port conflicts detected; aborting to avoid disturbing the live gateway"
    }
    Write-Pass "No test-port conflicts"
}

function Build-TestGateway {
    if ($SkipBuild) { Write-Step "Skipping build (-SkipBuild)"; return }
    Write-Step "Building $TestExeName"
    Push-Location $RepoRoot
    try {
        $output = go build -o $TestExeName ./cmd/serial-gateway/ 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Fail "Build failed: $output"
            throw "Build failed"
        }
        Write-Pass "Build succeeded -> $TestExePath"
    } finally {
        Pop-Location
    }
}

function Start-TestGateway {
    param([string]$Name)
    $cfg = $ConfigMap[$Name]
    $yamlPath = Join-Path $RepoRoot $cfg.Yaml
    if (-not (Test-Path $yamlPath)) {
        throw "Config not found: $yamlPath"
    }

    Write-Step "Starting isolated instance '$Name' from $($cfg.Yaml)"
    $existing = Get-Process -Name $TestProcName -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $TestExePath }
    if ($existing) {
        Write-Warn "Test instance for '$Name' may already be running (PID $($existing.Id))"
        return $existing
    }

    $proc = Start-Process -FilePath $TestExePath `
        -ArgumentList @('--config', $yamlPath) `
        -WorkingDirectory $RepoRoot `
        -PassThru -WindowStyle Hidden
    Write-Pass "Launched PID $($proc.Id) (http=$($cfg.HttpPort), ssh_base=$($cfg.SshBase))"

    $pids = Read-PidFile
    $pids.$Name = $proc.Id
    Write-PidFile $pids

    # Wait for HTTP readiness
    $timeout = 15
    $start = Get-Date
    $url = "http://localhost:$($cfg.HttpPort)/api/config"
    while ($true) {
        try {
            $null = Invoke-WebRequest -Uri $url -Method Get -TimeoutSec 2 -UseBasicParsing
            Write-Pass "Instance '$Name' ready on port $($cfg.HttpPort)"
            return $proc
        } catch {
            if (((Get-Date) - $start).TotalSeconds -gt $timeout) {
                Write-Fail "Instance '$Name' did not become ready within ${timeout}s"
                throw "Startup timeout for '$Name'"
            }
            Start-Sleep -Milliseconds 500
        }
    }
}

function Test-TestGateway {
    param([string]$Name)
    $cfg = $ConfigMap[$Name]
    $url = "http://localhost:$($cfg.HttpPort)/api/config"
    Write-Step "Smoke-test '$Name' HTTP API ($url)"
    try {
        $r = Invoke-RestMethod -Uri $url -Method Get -TimeoutSec 5
        Write-Pass "http_port=$($r.gateway.http_port) ssh.base_port=$($r.ssh.base_port)"
    } catch {
        Write-Fail "GET /api/config failed: $_"
        throw
    }
    try {
        $m = Invoke-RestMethod -Uri "http://localhost:$($cfg.HttpPort)/api/mappings" -Method Get -TimeoutSec 5
        Write-Pass "mappings: $($m.mappings.Count)"
    } catch {
        Write-Warn "GET /api/mappings failed: $_"
    }
}

function Stop-TestGateway {
    Write-Step "Stopping isolated test instances"
    $procs = Get-Process -Name $TestProcName -ErrorAction SilentlyContinue
    if (-not $procs) {
        Write-Host "  No test instances running"
    } else {
        $procs | ForEach-Object {
            try {
                Stop-Process -Id $_.Id -Force -ErrorAction Stop
                Write-Pass "Stopped PID $($_.Id)"
            } catch {
                Write-Warn "Could not stop PID $($_.Id): $_"
            }
        }
    }
    if (Test-Path $PidFile) {
        Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    }
}

function Show-Status {
    Write-Step "Debug status"
    $live = Get-Process -Name $LiveProcName -ErrorAction SilentlyContinue
    if ($live) { Write-Host "  live gateway : PID $($live.Id -join ', ')" }
    else       { Write-Host "  live gateway : not running" }

    $test = Get-Process -Name $TestProcName -ErrorAction SilentlyContinue
    if ($test) {
        foreach ($p in $test) { Write-Host "  test instance: PID $($p.Id)" }
    } else {
        Write-Host "  test instance: not running"
    }

    $listening = Get-ListeningPorts
    foreach ($name in (Get-ConfigList)) {
        $cfg = $ConfigMap[$name]
        $state = if ($listening -contains $cfg.HttpPort) { 'LISTENING' } else { 'free' }
        Write-Host "  config '$name': http $($cfg.HttpPort)=$state log=$($cfg.Log)"
    }
}

# ── Orchestration ────────────────────────────────────────────────────
function Invoke-DebugWorkflow {
    Invoke-ConflictCheck
    Build-TestGateway
    $names = Get-ConfigList
    foreach ($n in $names) { Start-TestGateway -Name $n }
    foreach ($n in $names) { Test-TestGateway -Name $n }
    Write-Step "Debug instances are running"
    Write-Host "  Logs: $($names | ForEach-Object { $ConfigMap[$_].Log })"
    Write-Host "  Remote probe ports: $($names | ForEach-Object { $ConfigMap[$_].RemotePort })"
    if (-not $KeepRunning) {
        Write-Host "  (re-run with -KeepRunning to leave instances up; or -Action stop)"
        Stop-TestGateway
    } else {
        Write-Pass "Keeping instances running (-KeepRunning)"
    }
}

# ── Dispatch ─────────────────────────────────────────────────────────
switch ($Action) {
    'conflict' { Invoke-ConflictCheck }
    'build'    { Build-TestGateway }
    'start'    { foreach ($n in (Get-ConfigList)) { Start-TestGateway -Name $n } }
    'smoke'    { foreach ($n in (Get-ConfigList)) { Test-TestGateway -Name $n } }
    'stop'     { Stop-TestGateway }
    'status'   { Show-Status }
    'all'      { Invoke-DebugWorkflow }
}
