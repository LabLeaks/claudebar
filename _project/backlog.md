# claudebar backlog

Unclassified items. Promote to a sprint when ready to work on them.

## Code quality

- [ ] Unify timeAgo and timeAgoShort into one function with a format param
- [ ] viewOverview calls loadTeamConfig on every 1s tick — cache it in the model
- [ ] wrapText doesn't preserve intentional newlines in descriptions

## Features

- [ ] Agent session switching — select an agent in the side panel to swap the main pane to that agent's live session (respawn-pane with --resume). Auto-return to lead when agent terminates. This is the key UX for managing agent teams.
- [ ] Show plan name in status bar (blocked — not available from statusline API yet, monitor for changes)
- [ ] Search/filter in task viewer
- [ ] Show active feature toggles in status bar (e.g. "TEAMS" indicator)
- [ ] Task list ID collision — `taskListIDForSession` uses `filepath.Base(cwd)` so two directories named `claudebar` share tasks. Should use full path encoding like session transcripts do. Needs migration of existing task lists (claudebar-commercebox, claudebar-lableaks, etc have live tasks). Do this when no sessions are running.
- [ ] `claudebar gc` command to clean up stale state files from dead sessions
- [ ] Show unclaimed sessions alongside live tmux sessions in the multi-session picker (currently separate paths)
- [ ] Test whether `claude --resume <id>` works with extra flags (--model, etc.) — if so, allow extra args on resume path

## Research

- [ ] Can we detect plan name from any Claude Code file/API? (not in statusline JSON currently)
- [ ] Explore MCP integration — could claudebar expose tools to claude via MCP?
- [ ] Can we use claude's built-in Ctrl+T task list alongside our side pane viewer?
