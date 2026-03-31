# CLAUDE.md

Project-specific context for AI agents working with CCU (Claude Code Usage Monitor).

<ARCHITECTURE>
Dual data source system:
- **OAuth API** (primary): Live usage from Anthropic's API at `https://api.anthropic.com/api/oauth/usage`
  - Requires OAuth token with `user:profile` scope (re-authenticate: `claude logout && claude login`)
  - Credentials retrieved from macOS Keychain using `security` command
  - Returns exact reset times and combined web + CLI usage
- **JSONL fallback**: Parses `~/.claude/projects/**/*.jsonl` when OAuth unavailable
  - CLI-only activity (no web usage)
  - Automatically used when OAuth credentials missing or API fails

Data flow: OAuth/JSONL → Analysis (sessions/weekly) → Bubbletea UI
See `internal/oauth/client.go` for OAuth implementation, `internal/data/` for JSONL parsing.
</ARCHITECTURE>

<CONVENTIONS>
**Token accounting** (critical distinction):
- `DisplayTokens`: Input + output only (shown in UI, matches Python implementation)
- `TotalTokens`: All tokens including cache (used for cost calculations)
DO NOT confuse these - they serve different purposes.

**Model normalisation**: Use `NormaliseModelName()` to handle variations:
- "claude-sonnet-4" → "sonnet"
- "claude-3-5-sonnet-20241022" → "sonnet"
- "claude-opus-4-20250514" → "opus"

**Session/weekly percentages are OAuth-only**: When OAuth is unavailable, the UI shows a degraded
fallback with raw cost and message count but no progress bars or percentages. JSONL data cannot
produce accurate percentages because the hardcoded plan limits don't match Anthropic's actual limits.

**OAuth retry**: When OAuth is disabled due to a non-permanent error, it automatically retries after
5 minutes. Errors requiring user action (token expired, re-authenticate) do not auto-retry.

**Time handling**: All times converted to UTC. Session blocks round start times DOWN to current hour.

**Plan limits**: Claude has NO per-5-hour-session token limits. The ~200k context window is per-conversation, not per-session. `TokenLimit` field in `models.Limits` is always 0 for predefined plans.
</CONVENTIONS>

<GOTCHAS>
**Session block calculation** (matches Python implementation):
- Session start times rounded DOWN to current hour (activity at 12:52 PM → session starts 12:00 PM)
- Sessions on the hour remain unchanged (activity at 1:00 PM → session starts 1:00 PM)
- New sessions start when entries occur after 5-hour window
- Gap blocks created when ≥5 hours passes between sessions
See `CreateSessionBlocks()` in `internal/analysis/sessions.go`.

**Burn rate uses proportional overlapping sessions** to prevent double-counting:
```go
// For each session overlapping the last hour:
// 1. Calculate overlap duration within hour window
// 2. Multiply session tokens by (overlap_duration / total_session_duration)
// 3. Sum proportional tokens across all overlapping sessions
// 4. Divide by 60 to get tokens/minute
```
This is the PRIMARY metric for predictions. See `CalculateBurnRate()` in `internal/analysis/sessions.go`.

**Cost-based prediction is primary** because model costs differ per token:
- Opus: More expensive per token
- Sonnet: Less expensive per token
- Mixed usage makes token-based predictions inaccurate

Display cost depletion time prominently. See `PredictCostDepletion()` in `internal/analysis/sessions.go`.

**Pricing complexity**: Input tokens, output tokens, cache creation, and cache read tokens all have different prices. See `internal/pricing/pricing.go` for model-specific pricing tables.
</GOTCHAS>

<TESTING>
Table-driven tests with `testify/assert`. Run single tests: `go test -v -run TestName ./internal/package`

Commands available via Makefile: `make test` (race detection + coverage), `make lint`, `make modernise`.
</TESTING>

# context-mode — MANDATORY routing rules

You have context-mode MCP tools available. These rules are NOT optional — they protect your context window from flooding. A single unrouted command can dump 56 KB into context and waste the entire session.

## BLOCKED commands — do NOT attempt these

### curl / wget — BLOCKED
Any Bash command containing `curl` or `wget` is intercepted and replaced with an error message. Do NOT retry.
Instead use:
- `ctx_fetch_and_index(url, source)` to fetch and index web pages
- `ctx_execute(language: "javascript", code: "const r = await fetch(...)")` to run HTTP calls in sandbox

### Inline HTTP — BLOCKED
Any Bash command containing `fetch('http`, `requests.get(`, `requests.post(`, `http.get(`, or `http.request(` is intercepted and replaced with an error message. Do NOT retry with Bash.
Instead use:
- `ctx_execute(language, code)` to run HTTP calls in sandbox — only stdout enters context

### WebFetch — BLOCKED
WebFetch calls are denied entirely. The URL is extracted and you are told to use `ctx_fetch_and_index` instead.
Instead use:
- `ctx_fetch_and_index(url, source)` then `ctx_search(queries)` to query the indexed content

## REDIRECTED tools — use sandbox equivalents

### Bash (>20 lines output)
Bash is ONLY for: `git`, `mkdir`, `rm`, `mv`, `cd`, `ls`, `npm install`, `pip install`, and other short-output commands.
For everything else, use:
- `ctx_batch_execute(commands, queries)` — run multiple commands + search in ONE call
- `ctx_execute(language: "shell", code: "...")` — run in sandbox, only stdout enters context

### Read (for analysis)
If you are reading a file to **Edit** it → Read is correct (Edit needs content in context).
If you are reading to **analyze, explore, or summarize** → use `ctx_execute_file(path, language, code)` instead. Only your printed summary enters context. The raw file content stays in the sandbox.

### Grep (large results)
Grep results can flood context. Use `ctx_execute(language: "shell", code: "grep ...")` to run searches in sandbox. Only your printed summary enters context.

## Tool selection hierarchy

1. **GATHER**: `ctx_batch_execute(commands, queries)` — Primary tool. Runs all commands, auto-indexes output, returns search results. ONE call replaces 30+ individual calls.
2. **FOLLOW-UP**: `ctx_search(queries: ["q1", "q2", ...])` — Query indexed content. Pass ALL questions as array in ONE call.
3. **PROCESSING**: `ctx_execute(language, code)` | `ctx_execute_file(path, language, code)` — Sandbox execution. Only stdout enters context.
4. **WEB**: `ctx_fetch_and_index(url, source)` then `ctx_search(queries)` — Fetch, chunk, index, query. Raw HTML never enters context.
5. **INDEX**: `ctx_index(content, source)` — Store content in FTS5 knowledge base for later search.

## Subagent routing

When spawning subagents (Agent/Task tool), the routing block is automatically injected into their prompt. Bash-type subagents are upgraded to general-purpose so they have access to MCP tools. You do NOT need to manually instruct subagents about context-mode.

## Output constraints

- Keep responses under 500 words.
- Write artifacts (code, configs, PRDs) to FILES — never return them as inline text. Return only: file path + 1-line description.
- When indexing content, use descriptive source labels so others can `ctx_search(source: "label")` later.

## ctx commands

| Command | Action |
|---------|--------|
| `ctx stats` | Call the `ctx_stats` MCP tool and display the full output verbatim |
| `ctx doctor` | Call the `ctx_doctor` MCP tool, run the returned shell command, display as checklist |
| `ctx upgrade` | Call the `ctx_upgrade` MCP tool, run the returned shell command, display as checklist |
