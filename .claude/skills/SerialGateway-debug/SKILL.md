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

### Pattern C — ssh_proxy stable-host-key mode

Use this when the target board reflashes often and its SSH host key changes. Set:

```yaml
reverse_tunnel:
  remote_port: 10022
  target_host: "192.168.1.10"
  local_port: 22
  mode: "ssh_proxy"
  target_user: "root"
  target_password: "<board-password>"   # or target_private_key_file
```

Then the remote user connects to the gateway endpoint as usual, but the host key they pin is the gateway's stable `host_key`, not the board's. Validation target: reconnect after the board's host key changes and confirm the remote side does **not** get a known_hosts mismatch.

### Pattern D — serial_ssh_tunnel (all COM ports, ports kept identical)

`serial_ssh_tunnel` opens **one** SSH connection and forwards **every** COM port in `ports` to the server using the **same** port number as locally (COM3 → 2203, COM25 → 2225; port = `ssh.base_port + COM number`). Unlike Patterns A–C this needs real COM ports, so it usually can't run fully isolated with `ports: []` — validate it against the live gateway (or a test instance that owns spare COM ports), and set `bind_address: "0.0.0.0"` only if the server's LAN must reach it (see GatewayPorts below).

```yaml
serial_ssh_tunnel:
  host: "172.25.4.7"
  port: 22
  user: "yicong.wu"
  private_key_file: "C:/Users/yicong.wu/.ssh/id_ed25519"
  bind_address: "0.0.0.0"   # 127.0.0.1 = server-local only; 0.0.0.0 needs GatewayPorts yes
```

Per-port fields (`remote_port`/`target_host`/`local_port`) are **ignored** — they're auto-derived. Startup log shows one `forwarding ... label=COMxx` line per port:

```
serial SSH reverse tunnel started host=... bind_address=0.0.0.0 forwards=4
forwarding component=tunnel remote=0.0.0.0:2203 target_host=127.0.0.1 target_port=2203 label=COM3
```

An arbitrary number of extra tunnels can also be declared under the `reverse_tunnels:` list (each a normal single forward, or `kind: "serial_ssh"` to behave like the section above).

### Pattern E — one `reverse_tunnels` list entry (arbitrary host IP → server port)

The `reverse_tunnels:` list exposes any reachable `host:port` through the server without touching the three named tunnels. This is the cleanest way to test a single extra forward in isolation: blank out the named tunnels with `host: ""`, keep `ports: []`, and declare just the one entry.

```yaml
gateway:
  http_port: 18089

reverse_tunnel:      { host: "" }
adb_reverse_tunnel:  { host: "" }
serial_ssh_tunnel:   { host: "" }

reverse_tunnels:
  - name: "host-192.168.42.1-ssh"
    host: "172.25.4.7"
    port: 22
    user: "yicong.wu"
    private_key_file: "C:/Users/yicong.wu/.ssh/id_ed25519"
    remote_port: 10001          # convention: 10000 + last octet of target_host
    target_host: "192.168.42.1"
    local_port: 22
    bind_address: "127.0.0.1"

logging: { output: "file", file_path: "gateway-test-42.log" }
ports: []
```

Startup log confirms it: `reverse tunnel started name=host-192.168.42.1-ssh ... remote_port=10001 target_host=192.168.42.1 target_port=22` followed by `forwarding ... remote=127.0.0.1:10001 target_host=192.168.42.1 target_port=22`. Probe from the server exactly like Patterns A–C (swap the port to `10001`). When reversing several host-side IPs, keep `remote_port = 10000 + last octet` so ports stay collision-free and self-documenting (assumes all targets share one /24). `remote_port` is manual — the gateway does **not** derive it from the IP.

### The GatewayPorts pitfall (bind_address: 0.0.0.0)

**The gateway log showing `remote=0.0.0.0:2203` does NOT mean the server bound `0.0.0.0`.** That is only what the gateway *requested*. If the server's sshd lacks `GatewayPorts yes`, sshd **silently rewrites the bind to `127.0.0.1`** — no error anywhere in the gateway log. The only source of truth is the actual bind on the server (next section). To expose to the server's LAN:

```bash
sudo sshd -T | grep -i gatewayports          # want: gatewayports yes  (or clients)
# if no: add "GatewayPorts yes" to /etc/ssh/sshd_config, then: sudo systemctl reload sshd
```

Caveat: under `-o BatchMode=yes` a `sudo` password prompt can't be answered, so `sudo sshd -T` may fail. In that case **infer** GatewayPorts from the actual bind: `127.0.0.1`-only binds ⇒ GatewayPorts is off.

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

For a LAN IP target, **avoid `Test-NetConnection`** — it can hang for a long time and return no output under `-NoProfile`. Use a `TcpClient` with an explicit timeout instead, and list the local interfaces to confirm which side an IP like `192.168.42.1` is on (peer vs. this host):

```powershell
function TryTcp($h,$p){ $c=New-Object Net.Sockets.TcpClient; try{ $a=$c.BeginConnect($h,$p,$null,$null); if($a.AsyncWaitHandle.WaitOne(3000)){ $c.EndConnect($a); $c.Close(); return $true } else { $c.Close(); return $false } } catch { return $false } }
Get-NetIPAddress -AddressFamily IPv4 | Select-Object IPAddress,InterfaceAlias   # is 192.168.42.1 a peer, or me?
TryTcp '192.168.42.1' 22        # target SSH reachable from the gateway host?
TryTcp '172.25.4.7'   22        # relay reachable?
```

Confirm the target is reachable **before** probing: a later `Connection refused` on the tunnel then points at the target, not the tunnel path.

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

The Bash tool can drive the whole check in one shot by wrapping the remote commands in `ssh ... "..."` — no interactive login needed. OpenSSH now prints a post-quantum warning to stderr on every connection; strip it so it doesn't drown the output:

```bash
ssh -i /c/Users/yicong.wu/.ssh/id_ed25519 -o BatchMode=yes -o StrictHostKeyChecking=no \
    -o ConnectTimeout=8 yicong.wu@172.25.4.7 "<remote commands>" 2>&1 \
  | grep -v "WARNING\|store now\|may need to be upgraded\|openssh.com/pq"
```

**First, check the actual bind on the server** — this is the only way to know whether `bind_address: 0.0.0.0` really took (vs. GatewayPorts silently forcing 127.0.0.1):

```bash
ss -tlnp 2>/dev/null | grep -E ':22(03|15|21|25) '
#   0.0.0.0:2203  -> LAN-reachable (GatewayPorts on)
#   127.0.0.1:2203 (+ [::1]) -> server-local only (GatewayPorts off)
```

Then probe each forwarded port. Use `PreferredAuthentications=none` so the probe does an SSH handshake and stops — it never enters an interactive serial shell (which would hang):

```bash
for p in 2203 2215 2221 2225; do
  ssh -o PreferredAuthentications=none -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
      -p $p x@127.0.0.1 exit 2>&1 | head -3
done
```

For a single-forward tunnel (Patterns A–C) swap the port (`10022`/`10023`) and, if it uses real auth, use `-o BatchMode=yes` instead.

**Clean up** the temporary known_hosts entries the probes wrote on the server:

```bash
for p in 2203 2215 2221 2225; do ssh-keygen -R "[127.0.0.1]:$p" >/dev/null 2>&1; done
```

## How to interpret results

### Success patterns

If the remote probe reaches SSH and then fails with:

- `Permission denied (publickey,password,keyboard-interactive)` — or, for a serial SSH endpoint, `Permission denied (password).`

especially alongside `Warning: Permanently added '[127.0.0.1]:2203' (ED25519) to the list of known hosts.`, that means:

- the reverse tunnel is up,
- the remote side can reach the forwarded port,
- the connection reached the target SSH server (and, for `serial_ssh_tunnel`, the gateway's stable ED25519 host key was negotiated),
- **only authentication failed** (which the probe withheld on purpose via `PreferredAuthentications=none`).

This is a **network-path success**. For a multi-port serial tunnel, confirm you get this on **every** port in the loop, not just the first.

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
- `serial SSH reverse tunnel started ... forwards=<N>` (one connection, N forwards)
- `connected component=tunnel addr=... forwards=<N>`
- `forwarding component=tunnel remote=<bind>:<port> target_host=... target_port=... label=COMxx`

Note `remote=0.0.0.0:<port>` in the log is the **requested** bind, not the effective one — always cross-check with `ss` on the server (GatewayPorts pitfall above). A single listener dying logs `accept error` + `remote listener failed`, then the whole tunnel reconnects and re-opens **all** ports.

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
- `serial_ssh_tunnel` forwards **all** COM ports over one connection at identical port numbers (COM3→2203). It needs real COM ports, so it's validated against the live gateway, not an isolated `ports: []` instance — probe every port, not just one.
- **The gateway log's `remote=0.0.0.0:<port>` is the requested bind, not the effective one.** If the server lacks `GatewayPorts yes`, sshd silently rebinds to `127.0.0.1` with no error. Always verify the real bind with `ss -tlnp | grep ':<port> '` on the server.
- Interpreting a serial-port probe: `Permanently added '[127.0.0.1]:<port>' (ED25519)` + `Permission denied (password).` = full path success (host key negotiated, only auth withheld).
- When driving remote checks through the Bash tool via `ssh host "cmd"`, filter OpenSSH's post-quantum warning (`grep -v "WARNING\|store now\|openssh.com/pq"`), and clean up probe-written known_hosts with `ssh-keygen -R "[127.0.0.1]:<port>"`. `sudo sshd -T` won't work under `BatchMode=yes` (password prompt) — infer GatewayPorts from the actual bind instead.
