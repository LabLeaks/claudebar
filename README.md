# claudebar

An enhanced wrapper for [Claude Code](https://claude.ai/code) that adds session persistence, live usage monitoring, interactive side panels for tasks and agents, and a menu system for common operations — all without leaving your terminal.

Close the window, come back later, pick up where you left off. Toggle features, switch permissions, upgrade Claude — all with automatic session resume so you never lose conversation history.

## Why

Claude Code is powerful but ephemeral. Close the terminal and your session is gone. Want to check your tasks or agent teams? You need slash commands. Want to toggle bypass permissions? Restart and lose your place. Want to know if you're in peak hours or burning through your usage limit? No persistent indicator.

claudebar solves all of this by wrapping Claude Code in a managed terminal session with persistent state, live status, and interactive panels.

## Installation

```bash
# Homebrew
brew install lableaks/tap/claudebar

# Or one-line install
curl -sSfL https://raw.githubusercontent.com/lableaks/claudebar/main/install.sh | sh

# Or build from source (requires Go 1.26+)
git clone https://github.com/lableaks/claudebar
cd claudebar
make install
```

Requires [tmux](https://github.com/tmux/tmux) — install with `brew install tmux` if you don't have it.

## Quick start

```bash
# Launch Claude Code inside claudebar
claudebar

# Pass any Claude Code flags
claudebar --dangerously-skip-permissions
claudebar --model sonnet --verbose
```

That's it. You now have a persistent session with a status bar and side panels.

## Session persistence

The killer feature. When you're done for now, press `⌥W` (Option+W) to background the session. Claude keeps running. Close the terminal, go get coffee, open a new terminal, type `claudebar` — you're right back where you were.

If you have multiple backgrounded sessions, an interactive picker lets you choose which one to reattach:

```
 claudebar

  Sessions for my-project
  ▸ my-project (5m ago)

  Other sessions
    other-thing (2h ago)

  + New session

  ↑↓ navigate  ⏎ select  n new  esc quit
```

## Status bar

The bottom of the screen shows two lines of persistent information:

```
 claudebar  my-project          ⚡PEAK (til 2:00pm EDT)  ⏱ USAGE 23% (resets 5:00pm)
 ⌥W background  ⌥S shell  ⌥T tasks  ⌥A agents  ⌥H help  ⌥M menu
```

**Peak hours**: Anthropic rate limits hit faster during weekday business hours (5-11am PT). The indicator shows this in your local timezone so you know when to expect slower responses.

**Usage**: Your 5-hour rolling usage percentage, color-coded green (<50%), yellow (50-80%), or red (>80%). Shows when the window resets.

## Keyboard shortcuts

| Shortcut | Action |
|----------|--------|
| `⌥W` | Background session (keeps running) |
| `⌥S` | Toggle shell panel |
| `⌥T` | Toggle tasks panel |
| `⌥A` | Toggle agents panel |
| `⌥H` | Show help |
| `⌥M` | Open action menu |

All shortcuts use the Option key (⌥) plus a letter.

## Action menu

Click anywhere on the status bar, or press `⌥M`, to open the action menu:

| Action | What it does |
|--------|-------------|
| Background | Send session to background (same as ⌥W) |
| Kill session | End the session entirely |
| Toggle bypass permissions | Switch between normal and bypass mode, auto-resumes |
| Update Claude Code | Runs npm update, restarts with session resume |
| Features | Toggle env-var features like Agent Teams, restart to apply |
| Toggle shell/tasks/agents pane | Same as the ⌥ shortcuts |
| Compact context | Sends `/compact` to Claude |
| Clear / new chat | Sends `/clear` to Claude |
| Toggle verbose | Sends `/verbose` to Claude |
| Show usage | Sends `/usage` to Claude |

Operations that change how Claude runs (permissions, features, upgrades) automatically restart Claude with `--resume` so your conversation history is preserved.

## Side panels

### Tasks panel (`⌥T`)

An interactive viewer for Claude's task list. Navigate, edit, and manage tasks without typing slash commands.

- `↑↓` or `j/k` — navigate tasks
- `⏎` — view task detail (full description, status, dependencies)
- `s` — cycle status (pending → in progress → completed)
- `e` — edit task in your `$EDITOR`
- `x` — delete task
- `d` — close panel

### Agents panel (`⌥A`)

Shows agent teams active in your current project. Drill into team members to see their prompts, inbox messages, and status.

- `⏎` — view team member detail
- `i` — view member's inbox messages
- `e` — edit team config in `$EDITOR`
- `o` — open teammate in a new panel
- `d` — close panel

### Shell panel (`⌥S`)

A shell for running git commands, tests, or anything else without leaving claudebar. Opens your default login shell.

## Feature toggles

Some Claude Code features require environment variables to be set before launch. claudebar lets you toggle these from the menu and automatically restarts with resume:

- **Agent Teams** — enables multi-agent team coordination
- **Max Thinking** — increases extended thinking budget to 32k tokens
- **Disable Background Tasks**

## How it works

claudebar runs Claude Code inside a managed terminal session that persists in the background. It uses a dedicated namespace so it never interferes with any other terminal sessions.

Key design choices:
- **Separate namespace**: claudebar sessions are fully isolated from your other terminal work
- **Statusline integration**: Claude Code sends usage data to claudebar via its statusline API, which is cached and displayed in the status bar
- **Atomic restarts**: When toggling features or permissions, the Claude process is atomically replaced — side panels stay open, no disruption
- **Project-scoped tasks**: Each project gets its own named task list that persists across sessions
- **Project-scoped agents**: The agents panel only shows teams relevant to your current project directory

## License

MIT
