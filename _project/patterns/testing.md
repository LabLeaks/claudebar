# Testing Patterns

## What to test

**Pure functions** — always test. Table-driven format. `cycleFeature`, `featureState`, `timeAgo`, `peakInfo`, `shellQuote`, `truncate`, `buildClaudeArgs`.

**State management** — test the data flow. `loadState` corrupt handling, `findUnclaimedSessions`, `resolveSessionID`, `claimedSessionIDs`.

**tmux-dependent functions** — NOT unit-testable without mocking the entire tmux interface. `runDefault`, `startSession`, `runUpgrade`, `togglePane` are integration-level. Don't write fake tests that mock everything — either test for real or don't test.

## Test isolation

### Filesystem

Use `t.TempDir()` for any test that creates files. Tests that touch real `stateDir()` or `configDir()` must override `HOME` env var to a temp directory and restore with `t.Cleanup`.

```go
tmp := t.TempDir()
t.Setenv("HOME", tmp)
// stateDir() now resolves under tmp
```

### Global state

`liveTmuxSessionsFunc` is the only mutable package-level variable (test seam for tmux). Override with `setLiveSessions(t, sessions)` which uses `t.Cleanup` for automatic restore. Don't use `t.Parallel()` on tests that touch this.

### Time

Functions using `time.Now()` internally (like `peakInfo`) can't be boundary-tested without refactoring to accept a clock. Test output contracts (non-empty, valid format) rather than specific values. If precise boundary testing becomes necessary, refactor the function to accept `time.Time`.

## Naming

`TestFunctionName_Scenario` — e.g., `TestCycleFeature_OffToOn`, `TestLoadState_CorruptFile`.

## Table-driven tests

Use for pure functions with discrete input/output:

```go
tests := []struct {
    name string
    // inputs...
    // expected...
}{...}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

## File placement

Tests go in the same package (`package main`), in `<source>_test.go`. New test files for previously untested source files (e.g., `commands_test.go`, `status_test.go`).
