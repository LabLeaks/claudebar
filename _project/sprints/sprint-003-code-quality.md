# Sprint 003 — Code Quality & Polish

Clean up tech debt so v0.2.0 ships clean. Findings from parallel audit (patterns, errors, tests, UX, security).

## Tasks

### Security
- [x] `writeStatuslineSettings` — use `json.Marshal` instead of `fmt.Sprintf` (directory names with `"` break JSON)

### Bug prevention
- [x] `sessionName()` — sanitize dots/colons that confuse tmux session targeting (`1.0` → session 1 window 0)
- [x] `truncate()` — bounds check for max=0

### UX
- [x] Add tmux-installed check at startup (clear error vs raw exec failure)
- [x] Add claude-installed check at startup
- [x] Help text: `○ off-peak` → `🌙 OFF-PEAK` to match actual indicator
- [x] Help popup height: 16 → 24 (content gets clipped)
- [x] Empty state messages: "(no tasks)" and "No active teams" need context/guidance

### Code quality
- [x] Drop dead error return from `loadState` (always nil, every callsite discards it)
- [x] Extract `currentSession()` helper (8 callsites doing same tmux query)
- [x] Move `editorCmd` to shared location (agentview inlines it twice instead of reusing taskview's)
- [x] Remove unused `matching`/`others` fields from `pickerModel`
- [x] Remove dead stale-team fallback block in agentview.go (lines 107-110)

### Tests
- [x] Table-driven tests for `peakInfo()` — output validation (time.Now not injectable for boundary tests)
- [x] Table-driven tests for `cycleFeature()` and `featureState()`
- [x] Table-driven tests for `timeAgo()`
- [x] Test `loadState` corrupt file handling (returns default state)

## Not doing (reviewed and rejected)

- File permissions 0644→0600: session IDs are UUIDs, not secrets
- Quit key unification across picker/side panes: different contexts, intentional
- moveCursor infinite loop guard: impossible with current item construction
- Non-atomic state writes: tiny JSON, loadState handles corruption, over-engineering
- Error message capitalization: bikeshedding
- commands.go test coverage: mostly tmux-dependent integration code
- WorkDir fallback dedup: would add side effects to loadState
- sessionInfo file location: Go packages aren't organized by file
