# Sprint 001 — Foundations

Fix bugs and clean up code before going public.

## Tasks

- [x] Fix readLastLine partial JSON — find last complete newline before 4KB boundary
- [x] Unify menu definition — extract shared menu data structure from tmux.go menuCmd() and commands.go runMenu()
- [x] Make usage cache per-session — key cache file by session name like state files already do
- [x] loadState: log JSON parse errors instead of silently swallowing them
- [x] Confirmation prompt before Kill session (menu action)
- [x] Create GitHub repo and push — https://github.com/LabLeaks/claudebar

## Out of scope

- Distribution tooling (sprint 002)
- Feature additions beyond kill confirmation
