# Session Management Patterns

## State vs config

- **State** (`~/.config/claudebar/<session>.state.json`) — per-session, tracks current session ID, permission mode, features, working directory. Ephemeral — lost state is recoverable by toggling features again. Previously stored in `os.UserConfigDir()` (`~/Library/Application Support/claudebar/` on macOS); migrated to `~/.config/claudebar/` in v0.2.0.
- **Config** (`~/.config/claudebar/config.json`) — global defaults applied to all new sessions. Persists user preferences across sessions.

## Session ID tracking

Claude Code creates a `.jsonl` transcript per session in `~/.claude/projects/<encoded-dir>/`. The filename (minus `.jsonl`) is the session ID used with `--resume`.

**Claimed vs unclaimed:** A session is "claimed" if its ID appears in any claudebar state file AND that state file's tmux session is still alive. Unclaimed sessions are raw `claude` CLI sessions available for adoption.

```
resolveSessionID(state) flow:
  1. state.SessionID set + .jsonl exists → use it (don't re-scan)
  2. state.SessionID empty or .jsonl gone → findLatestClaudeSession(skip=claimed)
```

This prevents claudebar from accidentally resuming another instance's session or a raw Claude session during feature toggles/restarts.

## Startup flow

```
claudebar (no args)
  ├─ live tmux session for this dir? → reattach
  ├─ multiple live sessions? → picker (attach or new)
  ├─ unclaimed .jsonl sessions? → resume picker (adopt into claudebar or new)
  └─ nothing? → startSession (fresh)
```

Killed claudebar sessions (state file exists, tmux session dead) are NOT offered for resume. The user must explicitly pass `--resume <id>` to resume a dead session's conversation.

## Path encoding

Working directories are encoded as `-Users-gk-foo` (replace `/` with `-`). The leading dash is critical — without it, session resume silently fails because the project directory doesn't match.
