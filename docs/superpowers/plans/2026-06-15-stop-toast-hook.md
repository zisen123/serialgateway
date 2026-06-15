# Stop-toast Hook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install a `Stop` event hook that pops a Windows toast notification (via SerialGateway API) when Claude finishes a reply that took >= 2 minutes.

**Architecture:** Hook fires on every Stop event → PowerShell script reads transcript JSONL to find last user prompt timestamp (skipping fast replies) → extracts last assistant text + session title → POSTs to `http://localhost:18080/api/notify`.

**Tech Stack:** PowerShell 5.1, SerialGateway toast API, Claude Code hooks

---

## Transcript JSONL format (discovered)

Key line types in the transcript:

| `type` value | What it contains | How to identify |
|---|---|---|
| `"user"` + `"message":{"role":"user","content":"<string>"}` | Actual user prompt | `message.content` is a plain string |
| `"user"` + `"message":{"role":"user","content":[{"tool_use_id":...}]}` | Tool result (skip) | `message.content` is an array with `tool_use_id` |
| `"assistant"` + `"message":{"role":"assistant","content":[{"type":"text","text":"..."}]}` | Assistant text | Look for `"type":"text"` in the content array |
| `"ai-title"` | Session title | Has `"aiTitle":"..."` field |

All lines have a `"timestamp"` field in ISO 8601.

---

### Task 1: Create the notify-stop.ps1 script

**Files:**
- Create: `~/.claude/hooks/notify-stop.ps1`

- [ ] **Step 1: Create the script file**

```powershell
New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\.claude\hooks"
```

Then create `$env:USERPROFILE\.claude\hooks\notify-stop.ps1`:

```powershell
# notify-stop.ps1 — Stop-event hook: toast when reply takes >= 2 min
# Reads hook JSON from stdin, parses transcript, POSTs to SerialGateway notify API.

$ErrorActionPreference = "Stop"
$inputJson = $input | Out-String
if (-not $inputJson) { exit 0 }

try {
    $hook = $inputJson | ConvertFrom-Json
    $transcriptPath = $hook.transcript_path
    if (-not $transcriptPath -or -not (Test-Path $transcriptPath)) { exit 0 }
} catch {
    exit 0
}

# ── Parse transcript ─────────────────────────────────────────────
$lastUserTime = $null
$lastReply    = ""
$sessionTitle = "Claude 已回复"

try {
    $lines = Get-Content -Path $transcriptPath -Encoding UTF8
    foreach ($line in $lines) {
        if (-not $line.Trim()) { continue }
        try { $entry = $line | ConvertFrom-Json } catch { continue }

        # Session title
        if ($entry.type -eq "ai-title" -and $entry.aiTitle) {
            $sessionTitle = $entry.aiTitle
        }

        # User prompt (skip tool results: content is an array with tool_use_id)
        if ($entry.type -eq "user" -and $entry.message.role -eq "user") {
            $content = $entry.message.content
            if ($content -is [string] -and $content.Length -gt 0) {
                $lastUserTime = $entry.timestamp
            }
        }

        # Assistant text
        if ($entry.type -eq "assistant" -and $entry.message.role -eq "assistant") {
            $blocks = $entry.message.content
            foreach ($block in $blocks) {
                if ($block.type -eq "text") {
                    $lastReply += $block.text
                }
            }
        }
    }
} catch {
    # If transcript parsing fails, skip silently
    exit 0
}

# ── Time threshold check ─────────────────────────────────────────
if ($lastUserTime) {
    $elapsed = [DateTime]::UtcNow - [DateTime]::Parse($lastUserTime)
    if ($elapsed.TotalSeconds -lt 120) {
        exit 0  # Fast reply, don't notify
    }
}

# ── Truncate reply ───────────────────────────────────────────────
if ($lastReply.Length -gt 150) {
    $lastReply = $lastReply.Substring(0, 150) + "..."
}
if (-not $lastReply.Trim()) {
    exit 0  # No text in reply, don't notify
}

# ── POST to SerialGateway ────────────────────────────────────────
try {
    $body = @{
        title    = $sessionTitle
        content  = $lastReply
        duration = "short"
    } | ConvertTo-Json

    $null = Invoke-RestMethod -Uri "http://localhost:18080/api/notify" `
        -Method Post `
        -Body $body `
        -ContentType "application/json" `
        -TimeoutSec 5
} catch {
    # SerialGateway not running — fail silent
    exit 0
}
```

- [ ] **Step 2: Verify the script file exists and is syntactically valid**

```powershell
Test-Path "$env:USERPROFILE\.claude\hooks\notify-stop.ps1"
```

Expected: `True`

```powershell
$errors = $null
[System.Management.Automation.PSParser]::Tokenize(
    (Get-Content "$env:USERPROFILE\.claude\hooks\notify-stop.ps1" -Raw),
    [ref]$errors
)
$errors.Count
```

Expected: `0`

- [ ] **Step 3: Commit**

```bash
git add ~/.claude/hooks/notify-stop.ps1
git commit -m "feat: add stop-event toast notification hook script

Co-Authored-By: Claude <noreply@anthropic.com>"
```

> Note: `~/.claude/` is a user-level directory (not in the project repo). The commit step here is for documentation — the file lives outside the project repo and doesn't need version control unless the user chooses to manage `~/.claude/` separately.

---

### Task 2: Add hooks.Stop entry to settings.json

**Files:**
- Modify: `~/.claude/settings.json`

- [ ] **Step 1: Read current settings**

```powershell
Get-Content "$env:USERPROFILE\.claude\settings.json" | ConvertFrom-Json | ConvertTo-Json -Depth 10
```

- [ ] **Step 2: Add hooks.Stop configuration**

Current settings.json content:

```json
{
  "model": "opus",
  "enabledPlugins": {
    "superpowers@claude-plugins-official": true
  },
  "tui": "fullscreen",
  "skipDangerousModePermissionPrompt": true,
  "theme": "dark",
  "mcpServers": {
    "wps-office": {
      "args": [
        "C:\\Users\\yicong.wu\\Documents\\wps-control\\wps-skills\\wps-office-mcp\\dist\\index.js"
      ],
      "command": "node"
    }
  }
}
```

Replace with (add `hooks` key):

```json
{
  "model": "opus",
  "enabledPlugins": {
    "superpowers@claude-plugins-official": true
  },
  "tui": "fullscreen",
  "skipDangerousModePermissionPrompt": true,
  "theme": "dark",
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "powershell",
            "args": [
              "-NoProfile",
              "-NonInteractive",
              "-File",
              "C:\\Users\\yicong.wu\\.claude\\hooks\\notify-stop.ps1"
            ],
            "timeout": 15
          }
        ]
      }
    ]
  },
  "mcpServers": {
    "wps-office": {
      "args": [
        "C:\\Users\\yicong.wu\\Documents\\wps-control\\wps-skills\\wps-office-mcp\\dist\\index.js"
      ],
      "command": "node"
    }
  }
}
```

- [ ] **Step 3: Verify settings.json is valid JSON**

```powershell
Get-Content "$env:USERPROFILE\.claude\settings.json" | ConvertFrom-Json
```

Expected: Parses without error, returns a PSObject.

- [ ] **Step 4: Verify hook appears in /hooks menu**

Start a new Claude Code session and run `/hooks`. Confirm `Stop` event is listed with `type: command` and source `User`.

---

### Task 3: Integration smoke test

- [ ] **Step 1: Ensure SerialGateway is running**

```powershell
Invoke-RestMethod http://localhost:18080/api/ports
```

Expected: returns JSON with ports list.

- [ ] **Step 2: Manually test the script with a sample stdin**

Create a test transcript that simulates a slow reply:

```powershell
$testJson = @{
    session_id     = "test-123"
    transcript_path = "$env:USERPROFILE\.claude\projects\C--Users-yicong-wu-Documents-SerialGateway\f5ba9a6c-21bc-4a20-a0f5-b70354d303b8.jsonl"
    cwd            = (Get-Location).Path
    hook_event_name = "Stop"
} | ConvertTo-Json

$testJson | powershell -NoProfile -NonInteractive -File "$env:USERPROFILE\.claude\hooks\notify-stop.ps1"
```

Expected: A toast notification pops up on the desktop with the session title and last reply content (if the last user message was >= 2 min ago).

- [ ] **Step 3: Wait for a real Stop event to fire**

Use Claude Code normally. After a long reply finishes, verify the toast notification appears.

- [ ] **Step 4: Commit the plan**

```bash
git add docs/superpowers/plans/2026-06-15-stop-toast-hook.md
git commit -m "docs: add stop-toast-hook implementation plan

Co-Authored-By: Claude <noreply@anthropic.com>"
```
