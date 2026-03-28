#!/bin/bash
# Capture claudebar screenshots programmatically
# Usage: ./screenshots.sh (run from a SEPARATE terminal, not inside claudebar)
# Requires: freeze, claudebar running in another terminal
# Produces: assets/screenshot-*.png

set -e

SOCKET="claudebar"
ASSETS="$(cd "$(dirname "$0")" && pwd)/assets"
mkdir -p "$ASSETS"

# Find the claudebar session
SESSION=$(tmux -L "$SOCKET" list-sessions -F '#{session_name}' 2>/dev/null | head -1)
if [ -z "$SESSION" ]; then
  echo "Error: No claudebar session found."
  echo "Start claudebar in another terminal first."
  exit 1
fi
echo "Found session: $SESSION"

capture() {
  local name="$1"
  sleep 0.5

  # Capture pane content
  tmux -L "$SOCKET" capture-pane -t "$SESSION" -e -p > /tmp/claudebar-pane.ansi

  # Build status bar (strip tmux style tags, keep text)
  local status_line1
  status_line1=$("$(cd "$(dirname "$0")" && pwd)/claudebar" _status 2>/dev/null || echo "")
  local status_bar=" claudebar  $SESSION  $status_line1"
  local shortcut_bar=" ⌥W background  ⌥S shell  ⌥T tasks  ⌥A agents  ⌥H help  ⌥M menu"

  # Combine: pane + separator + status bars
  {
    cat /tmp/claudebar-pane.ansi
    echo ""
    printf '\033[48;2;22;33;62m\033[38;2;0;212;255m\033[1m %s \033[0m\n' "$status_bar"
    printf '\033[38;2;136;136;136m %s\033[0m\n' "$shortcut_bar"
  } > /tmp/claudebar-capture.ansi

  freeze --output "$ASSETS/screenshot-${name}.png" \
    --window --shadow.blur 8 \
    --padding 10 \
    --font.size 14 \
    --theme "Catppuccin Mocha" \
    --language ansi \
    /tmp/claudebar-capture.ansi
  echo "  captured: screenshot-${name}.png"
}

# Screenshot 1: Main view
echo "Capturing main view..."
capture "main"

# Screenshot 2: Tasks pane
echo "Capturing tasks pane..."
tmux -L "$SOCKET" send-keys -t "$SESSION" M-t
sleep 2

# Capture both panes side by side
tmux -L "$SOCKET" capture-pane -t "$SESSION:0.0" -e -p > /tmp/claudebar-left.ansi
# Find the tasks pane
TASKS_PANE=$(tmux -L "$SOCKET" list-panes -t "$SESSION" -F '#{pane_id}' | tail -1)
if [ "$TASKS_PANE" != "$(tmux -L "$SOCKET" list-panes -t "$SESSION" -F '#{pane_id}' | head -1)" ]; then
  tmux -L "$SOCKET" capture-pane -t "$TASKS_PANE" -e -p > /tmp/claudebar-right.ansi
  # Just capture the tasks pane alone for a detail shot
  freeze --output "$ASSETS/screenshot-tasks.png" \
    --window --shadow.blur 8 \
    --padding 10 \
    --font.size 14 \
    --theme "Catppuccin Mocha" \
    --language ansi \
    /tmp/claudebar-right.ansi
  echo "  captured: screenshot-tasks.png"
fi

tmux -L "$SOCKET" send-keys -t "$SESSION" M-t
sleep 0.5

echo ""
echo "Done! Screenshots in $ASSETS/"
ls "$ASSETS/"
