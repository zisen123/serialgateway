# clean-tunnel-ports.ps1 — detect and kill stale server-side SSH sessions that
# hold the gateway's reverse-tunnel remote ports.
#
# Why: when the gateway host loses connectivity abruptly (process killed,
# network blip, sleep), its SSH connections end half-open. The server-side
# sshd sessions never notice and keep holding the remote listen ports, so the
# gateway's reconnect loop fails forever with
#   "ssh: tcpip-forward request denied by peer".
# Stray processes can also grab a freed port meanwhile (e.g. an `adb` client
# auto-starting a server on 15037).
#
# How (unprivileged — per-session peer attribution is NOT possible without
# root: the session's TCP socket belongs to the privileged sshd, /proc fd and
# environ are blocked, so we cannot ask "which client IP opened this session"):
#   1. One ssh probe connection per tunnel server (same user/key as the
#      gateway, from the config). Kills are scoped to MY user's
#      "sshd: <user>@notty" sessions only — @notty means no TTY, i.e. tunnel /
#      automated sessions; interactive shells (@pts/*), other users, and ssh
#      clients running on the server (ps shows "ssh ...", not "sshd:") are
#      never touched. The probe itself is excluded by walking its parent chain
#      to the owning sshd process.
#   2. Remote: kill my stray non-session processes holding a configured port
#      (the adb case — own processes ARE visible in ss -tlnp). Root-held
#      listeners can't be killed — reported as STILL-HELD.
#   3. Report per-port status; exit 0 only when every configured port is free.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File clean-tunnel-ports.ps1
#   ... -DryRun    # show what would be killed, kill nothing
#   ... -Config serial-gateway.yaml
#
# Best run with the gateway STOPPED (the restart flow does exactly that): then
# every @notty session of mine on that server is stale by definition. Running
# it while the gateway is up may kill live tunnel sessions — each tunnel then
# reconnects within one retry interval (<=30s) and the end state is identical,
# but expect a brief blip on every forwarded port.
#
# Exit codes: 0 = all configured remote ports free, 1 = error or still held.

param(
    [string]$Config = "serial-gateway.yaml",
    [switch]$DryRun,
    [int]$SshTimeoutSec = 10
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path

function Write-Info { param([string]$m) Write-Host "[clean] $m" -ForegroundColor Cyan }
function Write-Ok   { param([string]$m) Write-Host "[clean]   OK: $m" -ForegroundColor Green }
function Write-Warn2{ param([string]$m) Write-Host "[clean]   WARN: $m" -ForegroundColor Yellow }
function Write-Err2 { param([string]$m) Write-Host "[clean]   FAIL: $m" -ForegroundColor Red }

# ── 0. tunnel targets from the authoritative parser ─────────────────
$tunnelPortsExe = Join-Path $RepoRoot "tunnel-ports.exe"
if (-not (Test-Path $tunnelPortsExe)) {
    Write-Info "building tunnel-ports.exe ..."
    Push-Location $RepoRoot
    try {
        go build -o tunnel-ports.exe ./cmd/tunnel-ports/
        if ($LASTEXITCODE -ne 0) { throw "go build tunnel-ports failed" }
    } finally { Pop-Location }
}
$tp = & $tunnelPortsExe -config (Join-Path $RepoRoot $Config) | ConvertFrom-Json
if (-not $tp.tunnels) { Write-Info "no tunnels configured — nothing to do"; exit 0 }

# group remote ports by ssh host (each host needs its own cleanup pass)
$byHost = @{}
foreach ($t in $tp.tunnels) {
    if (-not $t.remote_ports) { continue }   # local_forward: nothing server-side
    if (-not $byHost[$t.host]) { $byHost[$t.host] = @{ user = $t.user; port = $t.port; key = $t.private_key_file; ports = @() } }
    foreach ($p in $t.remote_ports) { $byHost[$t.host].ports += $p }
}
if ($byHost.Count -eq 0) { Write-Info "no remote-listening tunnels — nothing to do"; exit 0 }

# ── remote cleanup script (bash) ─────────────────────────────────────
# Transported as base64 to survive PowerShell 5.1's BOM/encoding on stdin.
$remoteScript = @'
PORTS='__PORTS__'
DRYRUN=__DRYRUN__
me=$(id -un)
# this probe's sshd ancestors: the session running this script + its privileged
# parents ("sshd: me@notty" child and root "sshd: me [priv]" holder)
shield=" $$"
p=$$
while [ "$p" -gt 1 ]; do
  p=$(ps -o ppid= -p "$p" 2>/dev/null | tr -d ' ')
  [ -z "$p" ] && break
  shield="$shield $p"
done
say() { echo "$@"; }
say "INFO user=$me shielded_pids=$(echo $shield)"

# --- pass 1: my @notty sshd sessions (tunnel-style; probe lineage shielded) ---
for pid in $(ps -u "$me" -o pid=,args= | awk -v me="$me" 'index($0, "sshd: "me"@notty") && !/awk/ {print $1}'); do
  if echo "$shield" | grep -q " $pid "; then
    say "LIVE pid=$pid (this probe's lineage)"
    continue
  fi
  et=$(ps -o etimes= -p "$pid" | tr -d ' ')
  if [ "$DRYRUN" = "1" ]; then
    say "DRYRUN would kill stale session pid=$pid (age=${et}s)"
  else
    say "KILL stale session pid=$pid (age=${et}s)"
    kill "$pid" 2>/dev/null
    sleep 1
    kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null
  fi
done

# --- pass 2: stray non-session holders of target ports ----------------
sleep 1
for p in $PORTS; do
  holders=$(ss -tlnp 2>/dev/null | grep ":$p " | grep -oP 'pid=\K[0-9]+' | sort -u)
  [ -z "$holders" ] && continue
  for pid in $holders; do
    args=$(ps -o args= -p "$pid" 2>/dev/null)
    case "$args" in
      *"sshd: "*) say "PORT $p still held by sshd session pid=$pid ($args)" ;;  # root-held or not ours; pass 1 kills ours
      *)
        if [ "$DRYRUN" = "1" ]; then
          say "DRYRUN would kill stray holder pid=$pid of port $p ($args)"
        else
          say "KILL stray holder pid=$pid of port $p ($args)"
          kill "$pid" 2>/dev/null
          sleep 1
          kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null
        fi ;;
    esac
  done
done

# --- pass 3: final per-port status -------------------------------------
sleep 1
rc=0
for p in $PORTS; do
  if ss -tln 2>/dev/null | grep -q ":$p "; then
    say "STILL-HELD $p"
    rc=1
  else
    say "FREE $p"
  fi
done
exit $rc
'@

# ── per host: run remote cleanup over one ssh probe ──────────────────
$overallOk = $true
foreach ($hostName in $byHost.Keys) {
    $info = $byHost[$hostName]
    $ports = $info.ports | Sort-Object -Unique
    Write-Info "server ${hostName}: remote ports $($ports -join ', ')"

    $script = $remoteScript.Replace("__PORTS__", ($ports -join ' ')).Replace("__DRYRUN__", $(if ($DryRun) { "1" } else { "0" }))
    $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($script))

    $sshArgs = @("-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=$SshTimeoutSec")
    if ($info.key) { $sshArgs += @("-i", $info.key) }
    $sshArgs += @("$($info.user)@$hostName", "echo $b64 | base64 -d | bash")

    $out = & ssh @sshArgs 2>&1
    $sshRc = $LASTEXITCODE
    $out | ForEach-Object {
        $line = "$_"
        if ($line -notmatch '\S') { return }
        if     ($line -match '^(KILL|STILL-HELD|FAIL)')      { Write-Warn2 $line }
        elseif ($line -match '^(FREE|LIVE|INFO)')            { Write-Ok $line }
        elseif ($line -match '^DRYRUN')                      { Write-Warn2 $line }
        elseif ($line -match '^SKIP|^PORT')                  { Write-Host "[clean]   $line" }
        elseif ($line -match 'WARNING|store now|openssh.com'){}  # OpenSSH pq noise
        else                                                  { Write-Host "[clean]   $line" }
    }
    if ($sshRc -eq 0) {
        Write-Ok "all remote ports on $hostName are free"
    } elseif ($sshRc -eq 255) {
        Write-Err2 "ssh to $hostName failed (rc=255)"
        $overallOk = $false
    } else {
        Write-Warn2 "some ports on $hostName still held (rc=$sshRc)"
        $overallOk = $false
    }
}

if ($overallOk) { exit 0 } else { exit 1 }
