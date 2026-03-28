# claudebar

Go binary wrapping Claude Code in a tmux session. Session persistence, status bar, interactive task/agent side panels, clickable menu, feature toggles with transparent restart+resume.

## Agent Team

Sprint execution uses parallel Claude subagents with these roles:

- **Architect** — Designs the approach before code is written. Reviews task scope, identifies affected files, flags risks, proposes the implementation plan. Runs first on each task.
- **Researcher** — Investigates unknowns: reads docs, searches for APIs, checks how similar tools solve the problem. Feeds findings to the architect or developer.
- **UX** — Reviews user-facing changes: menu layout, status bar formatting, panel behavior, keybindings. Ensures consistency and usability.
- **Developer** — Writes the code. Follows the architect's plan and existing patterns.
- **Code Reviewer** — Reviews diffs for correctness, style, edge cases, and adherence to the architect's plan. Runs after the developer.
- **Test Engineer** — Writes tests for new and changed code. Covers happy path, edge cases, and regressions.
- **QA Tester** — Runs the build and tests, verifies the change works end-to-end. Reports pass/fail with exact output.
- **Docs Maintainer** — Updates README, CLAUDE.md, sprint docs, and backlog after changes land. Ensures docs match reality.

Typical flow: Architect → Developer (+ Researcher/UX as needed) → Code Reviewer + Test Engineer in parallel → QA Tester → Docs Maintainer.

## Architecture

- Dedicated tmux socket (`-L claudebar`). All keybinds/menu items call `claudebar _<command>` via `run-shell`.
- Side panes are Bubbletea TUIs that poll every 1s to auto-close when main pane dies.
- Restarts use `respawn-pane -k` (atomic, side panes survive).
- Usage data comes from Claude's statusline API via `--settings` overlay → cached to disk → read by status bar.
- Tasks/agents read directly from `~/.claude/tasks/` and `~/.claude/teams/`.

## Known Traps

- **Do NOT add tmux hooks for side pane cleanup.** We tried `pane-exited`, `pane-died`, `after-pane-died`, `claudebar _cleanup` chains — ALL cause `zsh: killed` on startup because they also fire during `respawn-pane` and failed starts. The polling approach works.
- **`make install` must `rm -f` before `cp`.** macOS kills newly-executed binaries when you overwrite a running binary in-place (`cp` reuses the inode). Running tmux sessions hold the old binary open via `run-shell` keybinds. You MUST `rm -f` first to unlink the old inode, then `cp` creates a fresh one. Without this you get `zsh: killed` on every invocation. This cost us hours to debug.
- **Path encoding must keep the leading dash.** `/Users/gk/foo` → `-Users-gk-foo`, NOT `Users-gk-foo`. Session resume silently fails otherwise.
- **Stale tmux servers.** If you get mysterious crashes after rebuilding, run `tmux -L claudebar kill-server` first.
- **Don't double-escape `%` in status bar.** It works as-is in tmux 3.5a.
