# tmux Conventions

## Socket

All tmux operations use `-L claudebar` (dedicated socket). This isolates claudebar from the user's own tmux sessions. The socket name is the `tmuxSocket` constant in tmux.go.

## Session names

Derived from `filepath.Base(cwd)` with dots and colons replaced by hyphens. This prevents tmux from misinterpreting `1.0` as "session 1, window 0" or `foo:bar` as "session foo, window bar". Duplicates get `-2`, `-3`, etc. suffixed.

## Keybinds

All keybinds use `⌥` (Meta/Alt) prefix to avoid conflicting with Claude Code's own key handling. Defined in `generateTmuxConf()` as `bind -n M-<key>`. Each keybind calls `claudebar _<command>` via `run-shell`.

## Side panes

Created with `split-window`, tracked via tmux session options (`@claudebar-tasks-pane`, `@claudebar-agents-pane`, `@claudebar-shell-pane`). Toggle pattern: check if tracked pane exists → kill it (off) or create it (on).

Side panes are Bubbletea TUIs that poll every 1s. They auto-close when the main pane dies (checked via `mainPaneAlive()`). Do NOT use tmux hooks for cleanup — see Known Traps in CLAUDE.md.

## Restart pattern

`restartClaudeWithResume` sends C-c + C-d to gracefully stop Claude, then uses `respawn-pane -k` to atomically replace the pane content. Side panes survive because they're separate panes. The `-k` flag kills whatever is running in the target pane.

## Helper: currentSession()

Use `currentSession()` (tmux.go) to get the current tmux session name. Don't inline `tmuxOutput("display-message", "-p", "#{session_name}")`.

## Helper: editorCmd()

Use `editorCmd(file)` (tmux.go) to open a file in the user's editor. Respects `$EDITOR`, falls back to `vi`. Don't inline `exec.Command` with EDITOR lookup.
