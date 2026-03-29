# Claude Code Undocumented API Surface

Everything here is undocumented and may change without notice. This is the regression test surface for v1.0.

## File system layout

| Path | What we use it for | Where in code |
|------|--------------------|---------------|
| `~/.claude/projects/{encoded-dir}/{session-id}.jsonl` | Session transcripts — ID extraction, claimed/unclaimed detection | claude.go |
| `~/.claude/tasks/{list-id}/{task-id}.json` | Task list data for side pane | taskview.go |
| `~/.claude/teams/{team-name}/config.json` | Agent team configuration | agentview.go |
| `~/.claude/teams/{team-name}/inboxes/{agent-name}.json` | Agent inbox messages | agentview.go |

### Path encoding

`/Users/gk/foo` → `-Users-gk-foo` (replace `/` with `-`, keep leading dash).

## CLI flags we depend on

| Flag | Purpose | Where |
|------|---------|-------|
| `--resume <session-id>` | Resume a conversation | claude.go:156 |
| `--dangerously-skip-permissions` | Bypass permission prompts | claude.go:162 |
| `--permission-mode plan` | Plan-only mode | claude.go:164 |
| `--remote-control` | Enable remote control | claude.go:168 |
| `--model <model>` | Select model | claude.go:172 |
| `--teammate-mode in-process` | Force in-process teammates | claude.go:176 |
| `--settings <file>` | Settings overlay file | claude.go:180 |

## Statusline JSON schema (stdin to statusline command)

```json
{
  "model": {"id": "...", "display_name": "..."},
  "rate_limits": {
    "five_hour": {"used_percentage": N, "resets_at": N},
    "seven_day": {"used_percentage": N, "resets_at": N}
  },
  "context_window": {"used_percentage": N},
  "workspace": {"current_dir": "..."}
}
```

All fields are optional — we handle missing/null gracefully with zero values.

## Settings file format

```json
{"statusLine": {"type": "command", "command": "<path> _statusline <session>"}}
```

## Environment variables

| Var | Purpose | Set by |
|-----|---------|--------|
| `CLAUDEBAR=1` | Detect nesting (prevent claudebar-in-claudebar) | tmux session env |
| `CLAUDE_CODE_TASK_LIST_ID` | Tell Claude which task list to use | tmux session env |
| `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` | Enable agent teams | feature toggle |
| `MAX_THINKING_TOKENS=32000` | Set thinking token limit | feature toggle |
| `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` | Disable background tasks | feature toggle |

## Slash commands we send

`/compact`, `/clear`, `/verbose`, `/usage` — sent via `send-keys` to the Claude pane.

## How we'd detect breakage

| Surface | Symptom |
|---------|---------|
| File layout change | Sessions don't resume, tasks/agents panes show empty |
| CLI flag removed | Claude errors on startup |
| Statusline schema change | Usage shows 0% permanently, model name blank |
| Settings format change | Statusline data never arrives |
| Env var change | Features silently stop working |
| Slash command change | Claude shows "unknown command" |
