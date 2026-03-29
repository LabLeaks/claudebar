# File Organization

## Current layout

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point, command dispatch |
| `claude.go` | Session state, config, Claude binary interaction, session ID resolution |
| `commands.go` | All `run*` command handlers, session listing/creation, feature toggling |
| `picker.go` | Session picker TUI (standalone, runs before tmux session exists) |
| `taskview.go` | Task side pane TUI (Bubbletea, runs inside tmux) |
| `agentview.go` | Agent teams side pane TUI (Bubbletea, runs inside tmux) |
| `status.go` | Status bar rendering (peak hours, usage display) |
| `statusline.go` | Statusline handler (receives data from Claude, caches to disk) |
| `tmux.go` | tmux helpers, config generation, shared utilities |

## Storage

All claudebar data lives under `~/.config/claudebar/` (XDG convention). Both state and config share the same directory — `stateDir()` delegates to `configDir()`. No separate locations.

| File pattern | Contents |
|---|---|
| `config.json` | Global defaults (permission mode, features, model) |
| `<session>.state.json` | Per-session state (session ID, features, work dir) |
| `<session>.usage-cache.json` | Cached statusline data (usage %, model) |
| `<session>.statusline-settings.json` | Settings overlay for Claude's `--settings` flag |
| `<session>.main-pane` | tmux pane ID for side pane health checks |
| `claudebar.tmux.conf` | Generated tmux config |

## Migrations

When changing storage locations or file formats, use the one-time migration pattern in `configDir()` (claude.go). Guard with a package-level `bool` so it runs at most once per process. Move files individually, skip if destination exists, clean up old dir if empty. This ran in v0.2.0 to move state from `~/Library/Application Support/claudebar/` to `~/.config/claudebar/`.

## Principles

**One package, flat structure.** claudebar is a single binary with no internal packages. All files are `package main`. This is intentional — the codebase is small enough that packages would add ceremony without benefit.

**Shared helpers go in tmux.go.** `currentSession()`, `editorCmd()`, `selfPath()`, `tmuxExec()`, `tmuxOutput()`. If it's used by multiple files and isn't specific to one domain, it lives here. Exceptions: `shellQuote()` lives in claude.go (co-located with the command building it serves), `truncate()` lives in taskview.go (originated there, also used by agentview). Don't move them just for consistency — proximity to primary usage matters more.

**Types are defined where they're created.** `claudeSessionState` in claude.go (created by loadState), `sessionInfo` in commands.go (created by listSessions), `pickerModel` in picker.go. Go packages don't have file boundaries — don't move types just for "organizational purity."

**Don't split commands.go yet.** At ~640 lines it's large but navigable. If it grows past ~800 lines or gains a clearly separable domain (e.g., a plugin system), split then. Premature splitting creates more files to navigate without reducing complexity.
