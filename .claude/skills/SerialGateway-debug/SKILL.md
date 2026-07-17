---
name: SerialGateway-debug
description: Safely debug and validate SerialGateway behavior alongside an already running gateway by using isolated configs, separate binaries, non-conflicting ports, and remote SSH-based verification.
---

# SerialGateway-debug

Use this skill when working on SerialGateway features and you need to **debug, validate, or probe behavior without disturbing the currently running gateway**. Here, “debug” means safe investigation and verification, not necessarily bug fixing.

## Goals

- Keep the existing gateway process running untouched.
- Avoid conflicts on HTTP, SSH mapping, ADB, serial ports, and logs.
- Validate reverse tunnels end-to-end from the remote server.
- Distinguish **network path success** from **SSH authentication failure**.

## Core rules

1. **Do not stop the existing gateway unless the user explicitly asks.**
2. **Build a separate executable** for test runs, e.g. `serial-gateway-test.exe`.
3. **Use a dedicated YAML config** with non-conflicting ports.
4. **If the current gateway may already own the COM ports, disable serial entirely**:
   ```yaml
   ports: []
   ```
5. **Disable ADB test forwarding unless you are explicitly testing ADB**:
   ```yaml
   adb_reverse_tunnel:
     host: ""
   ```
6. **Write logs to a separate file** such as `gateway-test-ssh.log`.

## Proven debug patterns

### Pattern A — localhost SSH target

Use this first to verify the reverse tunnel path without involving another LAN host.

```yaml
gateway:
  http_port: 18081

ssh:
  base_port: 32000
  auth:
    type: "password"

reverse_tunnel:
  host: "172.25.4.7"
  port: 22
  user: "yicong.wu"
  private_key_file: "C:/Users/yicong.wu/.ssh/id_ed25519"
  remote_port: 10022
  target_host: "127.0.0.1"
  local_port: 22
  bind_address: "127.0.0.1"

adb_reverse_tunnel:
  host: ""

logging:
  output: "file"
  file_path: "gateway-test-ssh.log"

ports: []
```

### Pattern B — LAN IP SSH target

Use this to prove `target_host` is really honored and not hardcoded to localhost.

```yaml
gateway:
  http_port: 18082

ssh:
  base_port: 32100
  auth:
    type: "password"

reverse_tunnel:
  host: "172.25.4.7"
  port: 22
  user: "yicong.wu"
  private_key_file: "C:/Users/yicong.wu/.ssh/id_ed25519"
  remote_port: 10023
  target_host: "<local-lan-ip>"
  local_port: 22
  bind_address: "127.0.0.1"

adb_reverse_tunnel:
  host: ""

logging:
  output: "file"
  file_path: "gateway-test-lan-ssh.log"

ports: []
```

In this repo, ready-made examples live at:

- `serial-gateway-test-ssh.yaml`
- `serial-gateway-test-lan-ssh.yaml`

## Helper script

`debug.ps1` (next to this SKILL.md) automates the whole safe side-by-side workflow: it checks for port/process conflicts with the live gateway, builds `serial-gateway-test.exe`, starts isolated instances from the test YAMLs, smoke-tests their HTTP ports, and stops them on exit. Run it from the repo root:

```powershell
# Full workflow: conflict check, build, start both configs, smoke-test, then stop
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/SerialGateway-debug/debug.ps1 -Action all

# Leave the test instances running for remote probing
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/SerialGateway-debug/debug.ps1 -Action all -KeepRunning

# Just one config (ssh or lan), skip rebuild if the test binary is current
... -Action start -Config ssh -SkipBuild

# Inspect state / stop only the test instances (never the live gateway)
... -Action status
... -Action stop
```

Other `-Action` values: `conflict`, `build`, `smoke`. `-Config` accepts `ssh`, `lan`, or `both` (default). The script records launched PIDs in `.debug-pids.json` and only ever stops processes named `serial-gateway-test`, so the live `serial-gateway` process is never touched.

## Safe debug workflow

### 1. Inspect the currently running gateway

Check whether the normal gateway is already running and which ports it owns.

```powershell
Get-Process serial-gateway -ErrorAction SilentlyContinue
Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
  Where-Object { $_.LocalPort -in 18080,2203,2221,2225,5037 }
```

Pick test ports that do **not** overlap.

### 2. Confirm the SSH target is reachable locally

For localhost target:

```powershell
Get-Service sshd
Get-NetTCPConnection -State Listen -LocalPort 22 -ErrorAction SilentlyContinue
```

For LAN IP target:

```powershell
Test-NetConnection -ComputerName <local-lan-ip> -Port 22
```

### 3. Build a separate binary

```powershell
go build -o serial-gateway-test.exe ./cmd/serial-gateway/
```

Never reuse the live process for debug runs when isolation matters.

### 4. Start the isolated instance

```powershell
Start-Process -FilePath ".\serial-gateway-test.exe" `
  -ArgumentList @("--config", "serial-gateway-test-ssh.yaml") `
  -PassThru -WindowStyle Hidden
```

Then smoke-test the dedicated HTTP port:

```powershell
Invoke-RestMethod http://localhost:18081/api/config | ConvertTo-Json -Depth 5
```

### 5. Verify from the remote server

SSH to the remote host first:

```bash
ssh -i /c/Users/yicong.wu/.ssh/id_ed25519 yicong.wu@172.25.4.7
```

Then from the remote server, probe the reverse-tunneled SSH port:

```bash
ssh -o BatchMode=yes \
    -o PreferredAuthentications=none \
    -o StrictHostKeyChecking=no \
    -o ConnectTimeout=5 \
    -p 10022 yicong.wu@127.0.0.1 exit
```

Swap `10022` for `10023` when testing the LAN-IP config.

## How to interpret results

### Success patterns

If the remote probe reaches SSH and then fails with:

- `Permission denied (publickey,password,keyboard-interactive)`

that means:

- the reverse tunnel is up,
- the remote side can reach the forwarded port,
- the connection reached the target SSH server,
- **only authentication failed**.

This is a **network-path success**.

### Failure patterns

These indicate the tunnel path itself is broken:

- `Connection refused`
- `Connection timed out`
- `No route to host`
- remote listen/bind errors in the test gateway log

## Logs to inspect

Typical useful logs:

- `gateway-test-ssh.log`
- `gateway-test-lan-ssh.log`

Look for lines like:

- `reverse tunnel started ... target_host=... target_port=...`
- `connected component=tunnel addr=...`
- `forwarding component=tunnel remote=127.0.0.1:<port> target_host=... target_port=...`

## Cleanup

Stop only the test processes, not the main gateway:

```powershell
Stop-Process -Id <test-pid>
```

Common temporary artifacts to clean up when done:

- `serial-gateway-test.exe`
- `gateway-test-ssh.log`
- `gateway-test-lan-ssh.log`

## Repo-specific lessons captured from this debug session

- A clean way to test `target_host` support is to run **two isolated configs**: one with `127.0.0.1`, one with the machine's LAN IP.
- `ports: []` is the safest option when the main gateway may already hold the COM ports.
- A reverse-tunneled SSH probe that reaches `Permission denied` is enough to prove the tunnel path worked.
- Use separate HTTP ports (`18081`, `18082`) and separate SSH base ports (`32000`, `32100`) to avoid colliding with the normal gateway.
