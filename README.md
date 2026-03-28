# claudebar

The missing interactive menu bar for [Claude Code](https://claude.ai/code).

<!-- TODO: screenshots -->

## The problem

You close your terminal. Your Claude session is gone. You restart, lose your conversation, re-explain your codebase.

You're 80% through your rate limit but don't know it. You're working during peak hours but can't tell. You want to toggle bypass permissions but that means restarting Claude and losing context.

You have agent teams running but no way to see what they're doing without typing slash commands. Same for tasks.

## What claudebar does

claudebar wraps Claude Code in a persistent tmux session. Everything survives closing the terminal — your conversation, your context, your agent teams.

**Never lose a session again.** Background with `⌥W`, close the terminal, come back hours later, type `claudebar` — you're exactly where you left off.

**See what matters at a glance.** The status bar shows peak hours, usage percentage, and reset times. No more guessing if you're about to hit the rate limit.

**Change settings without losing context.** Toggle bypass permissions, remote control, agent teams, or any feature — claudebar automatically restarts Claude with `--resume`. Your conversation history is preserved.

**Interactive side panels.** View and manage tasks, agent teams, or open a shell — all alongside Claude, no slash commands needed.

## Install

```bash
# Homebrew
brew install lableaks/tap/claudebar

# Or download the binary
curl -sSfL https://raw.githubusercontent.com/lableaks/claudebar/main/install.sh | sh

# Or build from source (Go 1.26+)
git clone https://github.com/lableaks/claudebar && cd claudebar && make install
```

Requires [tmux](https://github.com/tmux/tmux) (`brew install tmux`).

## Usage

```bash
claudebar                  # Launch or reattach to session for this directory
claudebar --model sonnet   # Pass any Claude Code flags
claudebar sessions         # Manage all sessions across projects
```

### Shortcuts

| Key | Action |
|-----|--------|
| `⌥W` | Background (session keeps running) |
| `⌥M` | Menu (or click the status bar) |
| `⌥T` | Tasks pane |
| `⌥A` | Agent teams pane |
| `⌥S` | Shell pane |
| `⌥H` | Help |

### Features menu

Toggle features without leaving Claude. Each cycles through three states:

- `○ OFF` — disabled
- `● ON` — enabled this session
- `◉ ALWAYS` — default for all new sessions

Includes: bypass permissions, remote control, agent teams, max thinking tokens, and more.

## How it works

Dedicated tmux socket (`-L claudebar`), isolated from your other tmux sessions. Sessions are scoped to your working directory. Feature changes atomically restart Claude with `--resume` — side panes survive, conversation preserved.

## License

MIT
