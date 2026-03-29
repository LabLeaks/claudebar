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

## Principles

**One package, flat structure.** claudebar is a single binary with no internal packages. All files are `package main`. This is intentional — the codebase is small enough that packages would add ceremony without benefit.

**Shared helpers go in tmux.go.** `currentSession()`, `editorCmd()`, `shellQuote()`, `selfPath()`, `tmuxExec()`, `tmuxOutput()`. If it's used by multiple files and isn't specific to one domain, it lives here.

**Types are defined where they're created.** `claudeSessionState` in claude.go (created by loadState), `sessionInfo` in commands.go (created by listSessions), `pickerModel` in picker.go. Go packages don't have file boundaries — don't move types just for "organizational purity."

**Don't split commands.go yet.** At ~640 lines it's large but navigable. If it grows past ~800 lines or gains a clearly separable domain (e.g., a plugin system), split then. Premature splitting creates more files to navigate without reducing complexity.
