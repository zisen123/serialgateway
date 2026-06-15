# Stop Hook: Toast Notification on Reply Complete

**Date:** 2026-06-15
**Status:** approved

## Motivation

User wants a Windows toast notification to pop up whenever Claude finishes
responding (Stop event), so they can switch to other windows while waiting
and be alerted when the reply is ready.

### Scope

This is a **single-machine** change: it lives in `~/.claude/settings.json`
and `~/.claude/hooks/` — it is NOT a project-level feature of SerialGateway
itself. It happens to POST to the local SerialGateway API for toast display.

## Design

### Architecture

```
Stop event fires
    → PowerShell script (~/.claude/hooks/notify-stop.ps1)
        → reads stdin JSON (transcript_path, session_id)
        → reads transcript JSONL, finds last user message timestamp
        → skips if elapsed < 2 minutes
        → extracts last assistant message (as notification body)
        → extracts session title (as notification title)
        → POST http://localhost:18080/api/notify
```

### Components

#### 1. Hook configuration (`~/.claude/settings.json`)

Add a `hooks.Stop` entry with a single matcher group (match-all) containing
one `type: "command"` hook that calls PowerShell.

- `timeout: 15` — script should complete within 15 seconds
- No `if` filter — fires on every Stop event

#### 2. PowerShell script (`~/.claude/hooks/notify-stop.ps1`)

```
Input (stdin):
    { session_id, transcript_path, cwd, hook_event_name, ... }

Logic:
    1. Parse stdin JSON → $transcriptPath
    2. Read $transcriptPath as JSONL (Get-Content)
    3. Parse each line, collect user/assistant messages with timestamps
    4. Get last user message timestamp → $lastUserTs
    5. If (now - $lastUserTs) < 120s → exit 0 (don't notify for fast replies)
    6. Get last assistant message → $reply (truncate to 150 chars)
    7. Get session title → $title (from session_title field or fallback "Claude 已回复")
    8. POST http://localhost:18080/api/notify
         body: { title: $title, content: $reply, duration: "short" }
    9. If POST fails (no connection, server down) → exit 0 silently
```

### Error handling

| Scenario | Behavior |
|---|---|
| transcript not found / unreadable | exit 0, no notification |
| SerialGateway not running (port 18080) | exit 0, no notification |
| Transcript has no user messages yet | exit 0, no notification |
| Script crashes unexpectedly | exit 1, Claude Code shows stderr notice |
| Reply < 2 minutes | exit 0, skip notification |

The guiding principle: **fail silent** — a broken notification hook should
never interrupt the user's workflow.

### Truncation strategy

Toast notifications display ~2 lines of body text on Windows. We truncate
the assistant reply to 150 characters, appending "..." if cut. The full reply
remains in the Claude Code transcript.

### Dependencies

- **SerialGateway** running on `localhost:18080` (the project server)
- **Windows** (toast API, PowerShell)
- No Anthropic API key needed (uses raw assistant reply, not AI-generated summary)

## Files changed

| File | Action | Purpose |
|---|---|---|
| `~/.claude/settings.json` | Modify | Add `hooks.Stop` entry |
| `~/.claude/hooks/notify-stop.ps1` | Create | PowerShell notification script |
