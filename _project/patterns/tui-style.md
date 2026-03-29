# TUI Style Guide

## Color palette

| Color | Hex | Usage |
|-------|-----|-------|
| Brand cyan | `#00d4ff` | Titles, active/selected items, cursor |
| Green | `#00ff88` | Positive states (new session, off-peak, low usage) |
| Red | `#ff6b6b` | Warnings (peak hours, high usage) |
| Yellow | `#ffd700` | Moderate states (medium usage) |
| Dim | `#555555` | Deselected items, secondary text |
| Hint | `#777777` | Hint bars at bottom of TUI views |
| Muted | `#888888` | Parenthetical details (timestamps, reset times) |
| Background separator | `#1a1a2e` | Invisible separator in status bar |

All TUI views (picker, task view, agent view) should use these colors consistently.

## Hint bars

Bottom of every TUI view. Shows available keys. Format: `key action` pairs separated by two spaces. Uses hint color `#777777`.

```
  ↑↓ navigate  ⏎ select  n new  esc quit
  ↑↓ navigate  ⏎ toggle  e edit  d close
```

## Quit/close keys

- **Picker** (standalone TUI): `q`, `esc`, `ctrl+c` — exits the process
- **Side panes** (tmux splits): `d`, `ctrl+c` — closes the pane

These are different contexts. Picker is a full-screen selector before the session exists. Side panes coexist with Claude. Don't unify them — `q` in a side pane could conflict with other interactions.

## Cursor

Use `▸` for active selection. Two-space indent for non-selected items to align with cursor width.

## Section headers

Dimmed, non-selectable. Cursor skips over them. Use `headerStyle` / `isHeader` pattern.

## Empty states

Always include guidance — tell the user what would populate this view.

```
(no tasks — Claude creates these during work)
No active teams — enable in Features menu (⌥M → ⚙)
```

## Separators

`─` repeated to ~29 chars. Fixed width works for 35% split panes. Not dynamically sized (would need terminal width detection).
