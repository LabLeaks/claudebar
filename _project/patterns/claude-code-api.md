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
| `CLAUDE_CODE_TASK_LIST_ID` | Set to `claudebar-<session-name>`. Gives Claude a deterministic task list ID so the side pane knows which `~/.claude/tasks/` subdirectory to watch. Without this, each conversation gets a random UUID directory | tmux session env |
| `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` | Enable agent teams | feature toggle |
| `MAX_THINKING_TOKENS=32000` | Set thinking token limit | feature toggle |
| `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` | Disable background tasks | feature toggle |

## Router env vars (set when routing through native proxy)

| Var | Purpose | Set by |
|-----|---------|--------|
| `ANTHROPIC_BASE_URL` | Points Claude Code at proxy preset URL (`http://127.0.0.1:3457/preset/<name>/v1/messages?session=<tmux-session>`) | router.go |
| `ANTHROPIC_AUTH_TOKEN` | Auth token for proxy (hardcoded `claudebar`) | router.go |
| `ANTHROPIC_API_KEY=` | Empty string — prevents Anthropic login fallback | router.go |
| `DISABLE_PROMPT_CACHING=1` | Non-Anthropic endpoints reject cache_control fields | router.go |
| `DISABLE_COST_WARNINGS=1` | Costs aren't real Anthropic charges | router.go |
| `NO_PROXY=127.0.0.1` | Prevent system proxy from intercepting local proxy traffic | router.go |
| `ENABLE_TOOL_SEARCH=true` | Disabled by default on custom base URLs; re-enable for proxy | router.go |

Native proxy: runs as `claudebar _proxy_server` subprocess on port 3457. PID tracked at `~/.config/claudebar/openrouter-proxy.pid`. Per-session usage logged to `~/.claudebar/openrouter-usage/<session>.jsonl`.

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
