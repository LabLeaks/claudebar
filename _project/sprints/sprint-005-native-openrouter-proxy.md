# Sprint 005 — Native OpenRouter Proxy (Replace CCR)

## Goal

Replace claude-code-router (CCR) with a native Go proxy built into claudebar. Eliminates the separate Node.js process, gives us direct access to usage/cost data, preserves cache_control, and enables per-model system prompt injection to fix tool use on non-Anthropic models.

## Motivation

CCR has critical problems for our use case:
1. **Strips `usage.cost` and `usage.prompt_tokens_details`** during Anthropic↔OpenAI response transformation — we can't track spend
2. **Strips `cache_control` for non-Claude models** — no prompt caching on providers that support it automatically (OpenAI, Grok, DeepSeek, Gemini 2.5) or explicitly (Anthropic)
3. **Separate process** — PID tracking, orphan cleanup, startup latency, port conflicts
4. **No per-session usage tracking** — all sessions share one CCR instance with no session isolation
5. **Tool use breaks on non-Anthropic models** — deferred tool protocol (ToolSearch) not understood by Qwen/etc.

## Architecture

```
claudebar (single Go binary)
  ├── native OpenRouter proxy runs as goroutine (port 3457)
  ├── singleton instance, multiple presets (one per router config)
  ├── each session gets: ANTHROPIC_BASE_URL=http://127.0.0.1:3457/preset/<name>/v1/messages?session=<tmux-session>
  ├── usage logged per-session to ~/.claudebar/openrouter-usage/<session>.jsonl
  └── status bar reads JSONL logs → shows TOKENS X | $Y.YY

Claude Code → http://127.0.0.1:3457/preset/<name>/v1/messages → Native Proxy → OpenRouter API
```

No separate process. No PID files for the proxy (it's our own goroutine). Session identity passed via URL query param.

## Research Findings

### OpenRouter Caching (Critical)
- **Automatic caching** for: OpenAI, Grok, DeepSeek, Groq, Gemini 2.5 — no headers needed
- **Explicit `cache_control`** for: Anthropic models only — requires `cache_control: {"type": "ephemeral"}` on content blocks
- **Stick routing** — OpenRouter routes subsequent requests to same provider to keep cache warm
- **Usage response** includes `prompt_tokens_details.cached_tokens` and `cost` fields
- CCR strips both `cache_control` (for non-Claude models) and `usage.cost` (always) — our proxy preserves them
- See: https://openrouter.ai/docs/features/prompt-caching

### CCR Transformer Analysis (Extracted from Bundle)

We decompiled CCR's minified source and extracted all transformer logic:

| Transformer | Request | Response | Must Port? |
|---|---|---|---|
| **openrouter** | Fix image URLs to data URIs, strip `cache_control` for non-Claude, merge provider options | Fix tool call IDs (int→string), convert reasoning→thinking blocks, fix finish_reason | Yes (partial — keep cache_control) |
| **enhancetool** | None | Buffer streaming tool args, 3-tier JSON repair (JSON→JSON5→jsonrepair→"{}") | **Yes — critical** |
| **cleancache** | Strip all `cache_control` | None | **No — we WANT cache_control** |
| **tooluse** | Force `tool_choice=required`, inject ExitTool escape hatch | Convert ExitTool calls back to plain text | Maybe (per-model) |
| **reasoning** | Convert `reasoning` → `thinking` format (or disable) | Convert `reasoning_content` → `thinking` blocks | Yes (for thinking models) |
| **maxtoken** | Cap `max_tokens` to model limit | None | Yes |
| **sampling** | Override temperature, top_p, top_k | None | Later |

### Tool Use Protocol Problem (Critical — In Progress)
- Claude Code sends only ~14 "core" tools in the `tools` array
- ~20+ additional tools (TaskCreate, TeamCreate, SendMessage, etc.) are **deferred** — listed by name only in a `<system-reminder>` text block
- Expected flow: model calls `ToolSearch` → gets schema → then calls deferred tool
- **Anthropic models know this from training. Non-Anthropic models (Qwen, etc.) try to call deferred tools directly → fails with "tool schema not sent to API"**
- Fix: per-model system prompt injection explaining the ToolSearch protocol, or pre-loading deferred tool schemas
- Research in progress: reading Claude Code source to understand full protocol

### CCR Alternatives Evaluated

| Project | Lang | Verdict |
|---|---|---|
| claude-code-adapter (x5iu) | Go | Best alternative — JSONL snapshots, Go binary, anthropic-beta forwarding. But still external dep. |
| litellm (BerriAI) | Python | Full-featured but heavy. Production platform, not a lightweight proxy. |
| anthropic-proxy (maxnowack) | TS/npm | Basic proxy, same issues as CCR |
| anthropic-proxy-rs (m0n0x41d) | Rust | Fast, daemon mode, but no cache/usage support |

**Decision: Build native Go proxy into claudebar.** Eliminates external deps, gives us full control over format conversion, usage tracking, and system prompt injection.

## What Was Built

### New Files
- `openrouter/types.go` — Data structures for Anthropic ↔ OpenAI format conversion, usage tracking
- `openrouter/transform.go` — Format conversion (messages, tools, system prompts, streaming SSE, tool calling round-trip)
- `openrouter/proxy.go` — HTTP proxy server with streaming, usage logging, LRU eviction, security (path traversal protection, request size limits, server timeouts)
- `openrouter/config.go` — Config loading, API key resolution, model slot parsing

### Modified Files
- `router.go` — Added `ensureOpenRouterRunning()`, `stopOpenRouter()`, `cleanupOrphanedProxy()`, provider-based routing in `routerEnvVars()` and `runToggleRouter()`
- `commands.go` — Updated `startSession` to dispatch to correct transport based on provider. Updated cleanup to handle both CCR and native proxy.
- `claude.go` — Updated `restartClaudeWithResume` to pass session name to `routerEnvVars`
- `statusline.go` — Extended `cachedUsage` struct with `TotalTokens`, `CachedTokens`, `CostUSD`, `RouterActive`. Added `refreshOpenRouterUsage()` that reads JSONL logs.
- `status.go` — Shows `TOKENS X | $Y.YY` when router active, falls back to `USAGE X%` for Anthropic sessions
- `router_test.go` — Updated for new `routerEnvVars` signature

### Removed
- `cmd_claudebar2.go` — Standalone test binary (superseded by integrated proxy)

## What's Not Working Yet

### Usage metrics not displaying
- JSONL logging code exists but proxy isn't active (sessions still routing through CCR)
- Status bar code reads JSONL but no data is being written
- Peak/offpeak display still shows for router sessions (should be hidden — irrelevant for OpenRouter)

### Hard cutover to native proxy not done
- CCR code paths still exist in router.go (generateCCRConfig, ensureCCRRunning, etc.)
- Old router configs lack `provider: "openrouter"` field — fall through to CCR
- Need to: wipe old configs, require `provider` field, remove CCR fallback, fail explicitly on missing fields

### CCR transformer logic not ported
Must port from CCR:
- [ ] **enhancetool** — JSON repair for malformed tool call arguments (3-tier: JSON → JSON5 → jsonrepair → "{}")
- [ ] **openrouter tool call ID fix** — replace integer IDs with `call_${uuid}` strings
- [ ] **openrouter image URL fix** — convert base64 to proper data URIs for Claude models
- [ ] **reasoning** — convert `reasoning_content` ↔ Anthropic `thinking` blocks
- [ ] **maxtoken** — cap max_tokens to model limits

### Per-model system prompt injection not built
- Deferred tool protocol needs explicit instructions for non-Anthropic models
- Need to rewrite `<system-reminder>` about deferred tools to include ToolSearch protocol
- Possibly pre-load all deferred tool schemas into the tools array
- Research in progress (reading Claude Code source)

## Config Format (New)

Router configs require `provider` field. Old configs without it are rejected.

```json
{
  "router_configs": {
    "openrouter-qwen": {
      "provider": "openrouter",
      "api_key": "$OPENROUTER_API_KEY",
      "models": {
        "default": "qwen/qwen3.6-plus:free",
        "background": "qwen/qwen3.6-plus:free",
        "think": "anthropic/claude-sonnet-4.6",
        "longContext": "qwen/qwen3-coder-plus"
      },
      "context_1m": true
    }
  }
}
```

## Deferred Tools Protocol (from CC Source Analysis)

Claude Code uses a two-tier tool loading system. Understanding this is critical for non-Anthropic model support.

### Always-Loaded Tools (~14)
Bash, Read, Write, Edit, Glob, Grep, ToolSearch, Agent, Skill, Task, etc. Full schemas sent in every request's `tools` array.

### Deferred Tools (~22)
TaskCreate, TaskGet, TaskList, TaskUpdate, TaskStop, TaskOutput, TeamCreate, TeamDelete, SendMessage, WebFetch, WebSearch, NotebookEdit, EnterWorktree, ExitWorktree, EnterPlanMode, ExitPlanMode, AskUserQuestion, RemoteTrigger, LSP, CronCreate/Delete/List, plus all MCP tools.

Only their **names** appear in a `<system-reminder>` text block. No schemas sent. The model must call `ToolSearch(query: "select:TaskCreate")` to load the schema, then call the tool.

### Why It Breaks on Non-Anthropic Models

1. **`tool_reference`** — Anthropic-proprietary API feature. Claude models emit `tool_reference` content blocks that auto-trigger schema loading. No other model supports this.
2. **ToolSearch description** is the ONLY instruction explaining the protocol. It says "deferred tools appear by name in `<system-reminder>` messages. Until fetched, only the name is known — there is no parameter schema, so the tool cannot be invoked." Non-Anthropic models don't reliably follow this.
3. **`ENABLE_TOOL_SEARCH=true`** — We already set this in `routerEnvVars()`, which force-enables deferred tools even through non-Anthropic hosts. But the model can't use them.

### Fix Options (Evaluated)

| Option | Approach | Pros | Cons |
|---|---|---|---|
| **A: Disable deferred tools** | Don't set `ENABLE_TOOL_SEARCH` | Simplest. CC loads ALL tool schemas into every request. | Higher token usage (~hundreds extra per turn). No code changes needed. |
| **B: Pre-expand in proxy** | Intercept request, find deferred tool names from `<system-reminder>`, inject cached schemas into `tools` array, remove ToolSearch | Model sees all tools with full schemas. No two-step dance. | Proxy must cache/fetch schemas. Complex. |
| **C: System prompt injection** | Add explicit "you MUST call ToolSearch first" instructions | Cheapest. No structural changes. | Relies on model compliance. Qwen may still ignore. |
| **D: Hybrid** | Expand deferred schemas inline, remove ToolSearch, rewrite system-reminder | Most robust for non-Anthropic models. | Most complex. Must maintain schema cache. |

**Current recommendation: Option A first** (just remove `ENABLE_TOOL_SEARCH=true` from `routerEnvVars` for non-Anthropic models). If token cost is acceptable, done. If not, graduate to Option D.

## Decisions Made

1. **Native Go proxy over CCR fork** — Full control, no external deps, runs as goroutine
2. **OpenRouter-first** — Only OpenRouter supported initially, but provider abstraction allows adding others
3. **Per-session JSONL logging** — Usage data written to `~/.claudebar/openrouter-usage/<session>.jsonl`
4. **Singleton proxy with presets** — One proxy instance, multiple named configs registered as presets
5. **No CCR backcompat** — Hard cutover planned, old configs rejected if missing `provider` field
6. **Keep `Transformers` config field** — Will define our own set (not CCR's), default based on provider

## Claudebar as Active Coordination Layer

Beyond just proxying, claudebar can become the **coordination layer** between agent team members using CC's hooks + skills + CLI infrastructure.

### The Problem
Agent team members spawned via non-Anthropic models:
- Don't reliably load their role-based skills
- Don't understand the deferred tool protocol (ToolSearch)
- Fall back to always-loaded tools instead of using the right deferred tools
- Parent agent has no deterministic way to know if skill loaded successfully

### The Mechanism
CC skills support `!`command`` syntax — shell commands that run before skill content reaches the model. Output gets injected into the prompt. This is **deterministic**, not model-dependent.

CC hooks fire on lifecycle events (SubagentStart, SubagentStop, TaskCreated, etc.) and can run shell commands.

Combined, these give claudebar a **side channel** for coordination:

### Proposed CLI Commands
```
claudebar _agent_ready <role>           # Signal skill loaded successfully
claudebar _agent_check <role>           # Return role-specific context (called via !`...` in skill)
claudebar _preload_tools <tool,list>    # Return deferred tool schemas for injection
claudebar _team_status                  # Return what other agents are doing
claudebar _model_guidance <model>       # Return model-specific instructions
```

### How It Works

1. **Skill loading confirmation**: Agent team skill frontmatter includes `!`claudebar _agent_check developer``. This runs before the skill reaches the model. It can:
   - Signal to parent that skill loaded
   - Inject model-specific ToolSearch guidance
   - Pre-load deferred tool schemas needed for the role
   - Return role-appropriate context

2. **Deferred tool guidance**: Instead of hoping the model calls ToolSearch, `_agent_check` injects explicit instructions: "You need TaskCreate and SendMessage. Call ToolSearch('select:TaskCreate,SendMessage') now."

3. **Cross-agent coordination**: `_team_status` lets agents know what others are doing without relying on SendMessage (which is itself a deferred tool).

4. **Model-specific tuning**: `_model_guidance qwen` returns Qwen-specific instructions (tool calling format, ToolSearch examples, behavioral nudges).

### Open Questions
- Can hooks in skill frontmatter fire for subagents? Or only project-level hooks?
- Can `!`command`` in skills access the subagent's context (agent name, team name)?
- What env vars are available inside skill shell commands? (`$CLAUDE_SESSION_ID`? `$CLAUDE_AGENT_NAME`?)
- Can we use `SubagentStart` hook to inject context BEFORE the skill loads?
- Latency impact of shell commands in skill loading path?

## Next Steps

1. **Hard cutover** — Remove all CCR code, require `provider` field, wipe old configs
2. **Port CCR transformers** — enhancetool JSON repair, tool call ID fix, reasoning, maxtoken
3. **Fix usage metrics** — Ensure proxy writes JSONL, status bar reads it (peak/offpeak already hidden for router)
4. **Deferred tool guidance via skills** — Add `!`claudebar _agent_check`` to agent team skills, inject ToolSearch instructions per model
5. **Build claudebar CLI commands** — `_agent_ready`, `_agent_check`, `_preload_tools`, `_model_guidance`
6. **Test end-to-end** — Activate router, verify tool calling, usage display, streaming, agent teams
