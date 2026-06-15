# Smoke test driver for serial-gateway
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File smoke.ps1 [-Port 18080]
# Or dot-source to use functions: . .\smoke.ps1; Start-Gateway; Test-Gateway; Stop-Gateway

param(
    [int]$Port = 18080,
    [string]$Config = "serial-gateway.yaml",
    [switch]$SkipBuild,
    [switch]$KeepRunning
)

$ErrorActionPreference = "Stop"
$BaseUrl = "http://localhost:$Port"
$ExePath = Join-Path $PSScriptRoot "..\..\..\serial-gateway.exe"
$ConfigPath = Join-Path $PSScriptRoot "..\..\..\$Config"

function Write-Step {
    param([string]$Message)
    Write-Host "=== $Message" -ForegroundColor Cyan
}

function Write-Pass {
    param([string]$Message)
    Write-Host "  PASS: $Message" -ForegroundColor Green
}

function Write-Fail {
    param([string]$Message)
    Write-Host "  FAIL: $Message" -ForegroundColor Red
}

# ── Build ────────────────────────────────────────────────────────────
function Build-Gateway {
    Write-Step "Building serial-gateway.exe"
    Push-Location (Join-Path $PSScriptRoot "..\..\..")
    try {
        $output = go build -o serial-gateway.exe ./cmd/serial-gateway/ 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Fail "Build failed: $output"
            throw "Build failed"
        }
        Write-Pass "Build succeeded"
    } finally {
        Pop-Location
    }
}

# ── Start / Stop ─────────────────────────────────────────────────────
function Start-Gateway {
    Write-Step "Starting serial-gateway"
    $proc = Get-Process -Name "serial-gateway" -ErrorAction SilentlyContinue
    if ($proc) {
        Write-Host "  Already running (PID $($proc.Id))"
        return
    }
    $ws = New-Object -ComObject WScript.Shell
    $ws.CurrentDirectory = Split-Path $ExePath -Parent
    $ws.Run("$ExePath --config $ConfigPath --http-port $Port", 0, $false)
    Write-Host "  Launched (waiting for HTTP readiness..."

    # Wait for server to be ready
    $timeout = 15
    $start = Get-Date
    while ($true) {
        try {
            $null = Invoke-WebRequest -Uri "$BaseUrl/api/ports" -Method Get -TimeoutSec 2 -UseBasicParsing
            Write-Pass "Server ready on port $Port"
            break
        } catch {
            if (((Get-Date) - $start).TotalSeconds -gt $timeout) {
                Write-Fail "Server did not start within ${timeout}s"
                throw "Startup timeout"
            }
            Start-Sleep -Milliseconds 500
        }
    }
}

function Stop-Gateway {
    Write-Step "Stopping serial-gateway"
    $proc = Get-Process -Name "serial-gateway" -ErrorAction SilentlyContinue
    if (-not $proc) {
        Write-Host "  Not running"
        return
    }
    $proc | Stop-Process -Force
    Write-Pass "Stopped"
}

# ── API tests ────────────────────────────────────────────────────────
function Test-GetPorts {
    Write-Step "GET /api/ports"
    try {
        $r = Invoke-RestMethod -Uri "$BaseUrl/api/ports" -Method Get
        if ($r.ports) {
            Write-Pass "Got $($r.ports.Count) port(s)"
            foreach ($p in $r.ports) {
                Write-Host "    $($p.device) ($($p.description)) status=$($p.status) ssh=$($p.ssh_port)"
            }
        } else {
            Write-Pass "No ports available (expected on headless machine)"
        }
    } catch {
        Write-Fail "GET /api/ports failed: $_"
        throw
    }
}

function Test-GetMappings {
    Write-Step "GET /api/mappings"
    try {
        $r = Invoke-RestMethod -Uri "$BaseUrl/api/mappings" -Method Get
        Write-Pass "Got $($r.mappings.Count) mapping(s)"
        foreach ($m in $r.mappings) {
            Write-Host "    $($m.serial_port) -> SSH:$($m.ssh_port) (baud=$($m.baudrate))"
        }
    } catch {
        Write-Fail "GET /api/mappings failed: $_"
        throw
    }
}

function Test-GetConfig {
    Write-Step "GET /api/config"
    try {
        $r = Invoke-RestMethod -Uri "$BaseUrl/api/config" -Method Get
        Write-Pass "HTTP port: $($r.gateway.http_port), SSH base: $($r.ssh.base_port)"
    } catch {
        Write-Fail "GET /api/config failed: $_"
        throw
    }
}

function Test-PostNotify {
    Write-Step "POST /api/notify"
    try {
        $body = @{title="SerialGateway Smoke Test"; content="All systems operational."; duration="short"} | ConvertTo-Json
        $r = Invoke-RestMethod -Uri "$BaseUrl/api/notify" -Method Post -Body $body -ContentType "application/json"
        if ($r.status -eq "dispatched") {
            Write-Pass "Toast notification dispatched (duration=$($r.duration))"
        } else {
            Write-Fail "Unexpected response: $($r | ConvertTo-Json)"
        }
    } catch {
        Write-Fail "POST /api/notify failed: $_"
        throw
    }
}

function Test-HealthCheck {
    Write-Step "Health check summary"
    try {
        $ports = Invoke-RestMethod -Uri "$BaseUrl/api/ports" -Method Get
        $mappings = Invoke-RestMethod -Uri "$BaseUrl/api/mappings" -Method Get
        $activePorts = ($ports.ports | Where-Object { $_.status -eq "active" }).Count
        Write-Pass "Healthy: $activePorts active port(s), $($mappings.mappings.Count) mapping(s)"
    } catch {
        Write-Fail "Health check failed: $_"
    }
}

# ── Main ─────────────────────────────────────────────────────────────
function Invoke-SmokeTest {
    if (-not $SkipBuild) {
        Build-Gateway
    }
    Start-Gateway
    try {
        Test-GetPorts
        Test-GetMappings
        Test-GetConfig
        Test-PostNotify
        Test-HealthCheck
        Write-Step "All smoke tests passed"
    } finally {
        if (-not $KeepRunning) {
            Stop-Gateway
        }
    }
}

# Run if invoked as script (not dot-sourced)
if ($MyInvocation.InvocationName -ne '.') {
    Invoke-SmokeTest
}
