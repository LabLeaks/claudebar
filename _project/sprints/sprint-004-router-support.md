# Sprint 004 — Router Support (OpenRouter + Non-Anthropic Models)

## Goal

Let users route Claude Code through non-Anthropic providers (OpenRouter, DeepSeek, etc.) via claudebar, using claude-code-router (CCR) as the translation proxy.

Target UX:
```
# Define a config (edit JSON or TUI wizard from menu)
# Activate from menu: ⌥M → ⚙ Features → Router → openrouter-qwen → ON/ALWAYS
# Or one-shot from CLI:
claudebar --router=openrouter-qwen /path/to/project
```

## Architecture

```
claudebar
  ├── owns named router configs (in ~/.config/claudebar/config.json)
  ├── manages single shared CCR instance lifecycle (start/stop)
  ├── generates ~/.claude-code-router/config.json with ALL router configs as presets
  ├── points each session at its preset: http://127.0.0.1:3456/preset/<name>/v1/messages
  └── injects env vars into tmux session (same pattern as features)

Claude Code → http://127.0.0.1:3456/preset/openrouter-qwen/v1/messages → CCR → OpenRouter
```

CCR is a hard dependency. Its transformer layer (enhancetool, cleancache, tooluse, etc.) is required for non-Anthropic models to work reliably with Claude Code's tool calling. Without it, tool use breaks, cache_control fields cause errors, and thinking format mismatches crash sessions.

**Single shared CCR instance.** CCR does not support multiple instances (hardcoded singleton PID file, hardcoded config path). Instead, all router configs are written as CCR presets on a single server. Each claudebar session points at its preset's URL path. Config is only written/regenerated when no CCR instance is running — never mutate config under a live server.

## Research

### claude-code-router (CCR) — Verified Against Source

`@musistudio/claude-code-router` v2.0.0. Local Fastify proxy. Research date: 2026-04-03.

**WARNING: CCR docs are unreliable.** The docs claim `--config <path>` and `--daemon` flags exist. They do not. The CLI (`cli.ts`) has no argument parser — it reads `process.argv[2]` for the command name and nothing else. Always verify against source.

**Config**: Hardcoded to `~/.claude-code-router/config.json`. No override mechanism. JSON5 with env var interpolation (`$VAR_NAME`).

**PID file**: Hardcoded to `~/.claude-code-router/.claude-code-router.pid`. Singleton. No per-instance namespacing.

**`ccr start` blocks.** Does not fork or daemonize. Starts Fastify, writes PID, blocks on event loop. Other CCR commands (`ccr code`, `ccr restart`) handle backgrounding by spawning a detached child process themselves. Claudebar must do the same — `cmd.Start()` without `cmd.Wait()` in Go.

**Port**: Configurable via `PORT` in config.json or `SERVICE_PORT` env var. Default 3456.

**Presets**: Named config variants on the same server. Each preset gets its own URL path: `http://127.0.0.1:3456/preset/<name>/v1/messages`. Presets live in `~/.claude-code-router/presets/<name>.json`. This is CCR's intended multi-config mechanism.

**Providers**: Array in config, each with name, base URL, API key, transformer chain, model list.

**Router slots**: Scenario-based — `default`, `background`, `think`, `longContext`, `webSearch`, `image`. Format: `"providerName,modelName"`. Unset slots fall through to `default`.

**Key transformers**:

| Name | Purpose |
|------|---------|
| `openrouter` | OpenRouter API adaptation |
| `deepseek` | DeepSeek API adaptation |
| `enhancetool` | Error-tolerant tool call parsing (critical for non-Anthropic models) |
| `cleancache` | Strips cache_control fields (non-Anthropic endpoints reject these) |
| `tooluse` | Optimizes tool_choice for weaker tool support |
| `reasoning` | Translates thinking/reasoning output formats |
| `maxtoken` | Caps max_tokens |

### Claude Code Env Vars (Reference)

Env vars claudebar sets on the tmux session when router is active:

| Variable | Purpose |
|---|---|
| `ANTHROPIC_BASE_URL` | Points at CCR preset URL |
| `ANTHROPIC_AUTH_TOKEN` | Auth token for CCR proxy |
| `ANTHROPIC_API_KEY=` | Empty — prevents Anthropic login fallback |
| `DISABLE_PROMPT_CACHING=1` | Non-Anthropic endpoints reject cache_control |
| `DISABLE_COST_WARNINGS=1` | Costs aren't real Anthropic charges |
| `NO_PROXY=127.0.0.1` | Prevent system proxy from intercepting local traffic |
| `ENABLE_TOOL_SEARCH=true` | Disabled by default on custom base URLs |

CCR handles model routing internally via its Router slots — we don't set `ANTHROPIC_DEFAULT_*_MODEL` env vars.

### Known Limitations

- **Tool use**: Fidelity varies by model. Qwen3-Coder supports it; many others are flaky even with enhancetool.
- **Context window**: Claude Code expects 200k+ for compaction. Shorter-window models may hit issues.
- **Feature degradation**: Extended thinking, vision, web search are model-dependent.
- **Prompt caching**: Anthropic-only. CCR's cleancache transformer strips these fields.
- **Config changes require deactivate+reactivate.** CCR config is only generated when CCR is not running. Editing `router_configs` while CCR is live has no effect until all routed sessions deactivate (stopping CCR) and a session reactivates (regenerating config and restarting CCR). A warn+restart UX is deferred to the TUI wizard (nice-to-have #14).

## Design

### Claudebar Router Configs

Named configs in claudebar's `config.json`. Each config defines provider, credentials, model assignments per CCR scenario slot, and transformer chain.

```go
type routerConfig struct {
    Provider     string            `json:"provider"`               // key into knownProviders
    APIKey       string            `json:"api_key"`                // provider API key (or $ENV_VAR)
    Models       map[string]string `json:"models"`                 // CCR slot → "provider,model"
    Transformers []interface{}     `json:"transformers,omitempty"`  // transformer chain
}
```

Example `config.json`:
```json
{
  "router_configs": {
    "openrouter-qwen": {
      "provider": "openrouter",
      "api_key": "$OPENROUTER_KEY",
      "models": {
        "default": "openrouter,qwen/qwen3.6-plus:free",
        "background": "openrouter,qwen/qwen3.6-plus:free",
        "think": "openrouter,qwen/qwen3.6-plus:free"
      },
      "transformers": ["openrouter", "enhancetool", "cleancache"]
    },
    "openrouter-deepseek": {
      "provider": "openrouter",
      "api_key": "$OPENROUTER_KEY",
      "models": {
        "default": "openrouter,deepseek/deepseek-coder-v3",
        "think": "openrouter,deepseek/deepseek-reasoner"
      },
      "transformers": ["openrouter", "enhancetool", "cleancache", "reasoning"]
    }
  }
}
```

### Known Providers Registry

```go
var knownProviders = map[string]string{
    "openrouter": "https://openrouter.ai/api/v1",
}
```

### CCR Config Generation

Claudebar generates `~/.claude-code-router/config.json` containing ALL providers referenced by any router config, plus preset files in `~/.claude-code-router/presets/` — one per router config.

```go
func generateCCRConfig(cfg *globalConfig) error
```

This function:
1. Collects all unique providers across all `router_configs`
2. Writes `~/.claude-code-router/config.json` with the `Providers` array and server settings
3. Writes `~/.claude-code-router/presets/<name>.json` for each router config (contains that config's `Router` slots)
4. `chmod 600` on all generated files (contain API keys)

**Only called when CCR is not running.** If CCR is live, error: "stop active router sessions before modifying router configs."

### CCR Lifecycle Management

Single shared CCR instance. Claudebar manages start/stop:

**Start** (on first router activation across any session):
1. Check `ccr` in PATH → error with install instructions if missing
2. Generate CCR config + presets from claudebar's `router_configs`
3. Spawn `ccr start` as detached background process (Go: `cmd.Start()`, no `Wait()`)
4. Wait for port 3456 to accept connections (poll with short timeout)
5. Record PID in a shared file: `~/.config/claudebar/ccr.pid`

**Stop** (when last routed session deactivates or exits):
1. Read PID from `~/.config/claudebar/ccr.pid`
2. Kill process
3. Clean up PID file

**Liveness check**: Before starting, check if CCR is already running (PID file exists + process alive). If so, skip start — reuse existing instance.

**Config reload**: Config changes don't take effect until CCR restarts. If CCR is running when config is edited, warn the user and offer to restart CCR from the editor. User takes responsibility for ensuring active sessions aren't mid-request before restarting.

### Session State Changes

```go
type claudeSessionState struct {
    // ... existing fields ...
    Router string `json:"router,omitempty"` // active router config name
}
```

No per-session CCRPort/CCRPid — single shared instance.

### Global Config Changes

```go
type globalConfig struct {
    // ... existing fields ...
    Router        string                   `json:"router,omitempty"`         // default router config (ALWAYS)
    RouterConfigs map[string]*routerConfig  `json:"router_configs,omitempty"` // named configs
}
```

### Activation: Features Menu (ON / ALWAYS / OFF)

Same pattern as feature toggles. Router configs appear in the features menu:

```
Features  ○ off  ● on  ◉ always
────────────────────────────────
◉ ALWAYS  Agent Teams
○    OFF  Max Thinking (32k)
────────────────────────────────
Router
● ON      openrouter-qwen
○ OFF     openrouter-deepseek
          + New router config...
```

Radio-button: only one router active at a time per session. Cycle: OFF → ON → ALWAYS → OFF.

Activating: ensure CCR running → inject env vars → restart Claude Code.
Deactivating: remove env vars → restart Claude Code → if no other routed sessions, stop CCR.

### CLI Flag: `--router=`

```
claudebar --router=openrouter-qwen /path/to/project
```

Sets `state.Router` for this session (equivalent to ON). Extracted from args before passing to Claude. Error if config name doesn't exist in `router_configs`.

### Env Var Injection

```go
func routerEnvVars(routerName string) []string {
    if routerName == "" {
        return nil
    }
    return []string{
        fmt.Sprintf("ANTHROPIC_BASE_URL=http://127.0.0.1:3456/preset/%s/v1/messages", routerName),
        "ANTHROPIC_AUTH_TOKEN=claudebar",
        "ANTHROPIC_API_KEY=",
        "DISABLE_PROMPT_CACHING=1",
        "DISABLE_COST_WARNINGS=1",
        "NO_PROXY=127.0.0.1",
        "ENABLE_TOOL_SEARCH=true",
    }
}
```

Injected via same two paths as `featureEnvVars()`:
- Fresh session: tmux `-e` flags
- Restart/resume: env prefix on respawn-pane command

### Config File Permissions

`chmod 600` on claudebar config.json when it contains `router_configs` (API keys). Also on all generated CCR config/preset files.

### Status Bar

When router is active, show the config name:
```
⚡ openrouter-qwen │ tasks: 3 │ ...
```

### Validation

- `--router=foo` but `foo` not in `router_configs` → error with available configs
- Router config references unknown provider → error with known providers
- Router config has no API key (and env var not set) → error
- Router config has no models → error: at least `default` slot required
- `ccr` not in PATH → error with install instructions
- Config edited while CCR running → warn: changes take effect on CCR restart, offer restart option

## New Files

- `router.go` — `knownProviders`, `routerConfig`, `routerEnvVars()`, `generateCCRConfig()`, CCR lifecycle (start/stop/liveness), `extractRouterFlag()`, `runToggleRouter()`, router menu section

## Modified Files

- `claude.go` — `Router` in session state, `Router`/`RouterConfigs` in global config
- `commands.go` — `startSession` handles `--router` flag + CCR startup, `runFeatures` includes router section, `restartClaudeWithResume` includes router env vars
- `main.go` — `"_toggle_router"` case
- `status.go` — shows active router config name in status bar

## Tests

- `generateCCRConfig` — produces correct CCR JSON + preset files, transformers mapped correctly
- `routerEnvVars` — correct env vars with preset URL when active, nil when inactive
- `extractRouterFlag` — `--router=val` and `--router val`, no flag, mixed with other args
- Config round-trip with router fields
- Radio-button: activating one deactivates others
- Validation: missing provider, missing key, missing models, missing ccr binary
- CCR liveness check: detects running instance correctly
- `featureEnvVars` unaffected by router state

## Task Breakdown

### Must-have
- [x] 1. `routerConfig` type, config structs, `knownProviders` registry
- [x] 2. `generateCCRConfig()` — claudebar config → CCR config.json + preset files
- [x] 3. CCR lifecycle — start (detached), stop, liveness check, dependency check
- [x] 4. `routerEnvVars()` — router name → env var list with preset URL
- [x] 5. `extractRouterFlag()` — parse `--router=` from CLI args
- [x] 6. `startSession` integration — router flag, ensure CCR running, env var injection
- [x] 7. `restartClaudeWithResume` integration — env var injection
- [x] 8. Features menu: router section with ON/ALWAYS/OFF radio-button
- [x] 9. Status bar: show active router config name
- [x] 10. `chmod 600` on config files with API keys
- [ ] 11. Config reload: warn on edit while CCR running, offer restart option — deferred to TUI wizard
- [x] 12. Session cleanup: on session exit, if last routed session, stop CCR
- [x] 13. Tests

### Nice-to-have (deferred)
- [ ] 14. TUI wizard for creating new router configs from menu
- [ ] 15. Delete/edit router config from menu

## Decisions

- **CCR is a hard dependency.** Transformer layer required for non-Anthropic model compatibility. Users don't interact with CCR directly.
- **Single shared CCR instance.** All router configs are CCR presets on one server. Sessions point at their preset URL. No multi-instance complexity.
- **Claudebar owns the config surface.** Named router configs in claudebar's config.json. CCR config is a generated artifact.
- **Config reload.** Config changes are allowed anytime but don't take effect until CCR restarts. Editor warns and offers restart option when CCR is live.
- **No `claudebar router` subcommand.** Config is declarative (edit JSON) or via TUI wizard from menu.
- **Require at least `default` model slot.** Routing without model assignment would send Anthropic models through provider at markup pricing.
- **One active router per session** (radio button). Multiple providers out of scope.
- **API keys in plaintext** in config.json, protected by chmod 600. Env var interpolation (`$VAR_NAME`) supported.

## Execution Plan

### Phase 0 — Architect + PM + QA Plan
- Architect: design file-level implementation from this sprint doc
- PM: validate architect's plan against this doc (sprint doc = spec, no separate PRD)
- QA: write QA plan from this doc — what to verify, how to test CCR lifecycle, what "working" looks like

### Phase 1 — Dev + Test Engineer (parallel)
File ownership:
- **Developer**: `router.go` (new), `claude.go`, `commands.go`, `main.go`
- **Test Engineer**: `router_test.go` (new)

Build check after Phase 1: `go build && go test ./...`

### Phase 2 — PM + Architect + QA (parallel validation)
- PM: does implementation match every decision in this doc?
- Architect: does code follow the plan?
- QA: build binary, create router config, activate from menu, verify CCR starts, verify env vars, verify status bar. Bring receipts.

All three must ACCEPT before Phase 3.

### Phase 3 — Code Review
Different agent than developer. Focus: correctness, security (API key handling, chmod 600), edge cases (CCR not installed, port busy, malformed config).

### Phase 4 — Docs Maintainer
Update CLAUDE.md, patterns docs, sprint doc completion.

### Researcher — On-call
Available throughout for CCR behavioral questions. Not a fixed phase.
