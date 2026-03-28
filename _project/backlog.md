# claudebar backlog

Unclassified items. Promote to a sprint when ready to work on them.

## Code quality

- [ ] Unify timeAgo and timeAgoShort into one function with a format param
- [ ] viewOverview calls loadTeamConfig on every 1s tick — cache it in the model
- [ ] wrapText doesn't preserve intentional newlines in descriptions

## Features

- [ ] Show plan name in status bar (blocked — not available from statusline API yet, monitor for changes)
- [ ] Scrollable inbox view in agents panel (currently shows all messages, overflows on long histories)
- [ ] Search/filter in task viewer
- [ ] Show active feature toggles in status bar (e.g. "TEAMS" indicator)

## Research

- [ ] Can we detect plan name from any Claude Code file/API? (not in statusline JSON currently)
- [ ] Explore MCP integration — could claudebar expose tools to claude via MCP?
- [ ] Can we use claude's built-in Ctrl+T task list alongside our side pane viewer?
