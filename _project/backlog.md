# claudebar backlog

## Distribution

- [ ] Set up GoReleaser (.goreleaser.yaml + GitHub Actions)
- [ ] Create Homebrew tap (lableaks/homebrew-tap)
- [ ] Write install.sh (curl | sh installer)
- [ ] Create GitHub repo and push
- [ ] First tagged release (v0.1.0)

## Code quality

- [ ] Unify menu definition — menuCmd() in tmux.go and runMenu() in commands.go define the same menu items twice; extract shared data structure
- [ ] Make usage cache per-session — two simultaneous claudebar sessions overwrite each other's usage-cache.json
- [ ] Fix readLastLine partial JSON — 4KB boundary can cut mid-line, need to find the last complete newline
- [ ] Unify timeAgo and timeAgoShort into one function with a format param
- [ ] viewOverview calls loadTeamConfig on every 1s tick — cache it in the model
- [ ] loadState silently swallows JSON parse errors — at least log corruption
- [ ] wrapText doesn't preserve intentional newlines in descriptions

## Features

- [ ] Asciinema demo recording for README
- [ ] Show plan name in status bar (not available from statusline API yet — monitor for changes)
- [ ] Scrollable inbox view in agents panel (currently shows all messages, overflows on long histories)
- [ ] Search/filter in task viewer
- [ ] Confirmation prompt before Kill session (menu action)
- [ ] Show active feature toggles in status bar (e.g. "TEAMS" indicator)
- [ ] Context window % in status bar (data already cached from statusline)

## Research

- [ ] Can we detect plan name from any Claude Code file/API? (not in statusline JSON currently)
- [ ] Explore MCP integration — could claudebar expose tools to claude via MCP?
- [ ] Can we use claude's built-in Ctrl+T task list alongside our side pane viewer?
