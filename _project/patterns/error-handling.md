# Error Handling Patterns

## State/config loading — return defaults, never error

`loadState` and `loadConfig` return a default struct on any failure (missing file, corrupt JSON, unreadable). They never return errors. Rationale: claudebar must always start, even with corrupted state. The user can toggle features again; losing state is better than failing to launch.

```go
// Good — caller gets usable state no matter what
state := loadState(sess)

// Bad — error return was always nil, every callsite discarded it
state, _ := loadState(sess)  // removed in sprint 003
```

Corrupt state files log to stderr with a truncated content snippet for debugging, then return defaults.

## State/config saving — check errors at critical points

`saveState` and `saveConfig` return errors. Check them when the save is critical to the operation (e.g., before launching a session that depends on the state). Okay to discard in fire-and-forget contexts (e.g., caching usage data).

## tmux commands — fire-and-forget for display, check for structure

`tmuxExec` calls for `send-keys`, `display-message`, `set-option` are fire-and-forget. If they fail, the UX degrades but nothing breaks.

`tmuxExec` calls for `new-session`, `split-window`, `respawn-pane` should check errors — these are structural and the caller needs to know if they failed.

## os.Getwd — discard error

Every `os.Getwd()` call discards the error. If cwd is gone, we get an empty string and downstream operations fail gracefully (empty session name, no matching sessions). This is acceptable — the cwd being deleted mid-session is not a scenario we optimize for.
