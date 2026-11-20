# CLAUDE.md

Project-specific context for AI agents working with CCU (Claude Code Usage Monitor).

<ARCHITECTURE>
Three data sources, in order of preference:
- **OAuth API** (primary): Live usage from Anthropic's API at `https://api.anthropic.com/api/oauth/usage`
  - Requires OAuth token with `user:profile` scope (re-authenticate: `claude logout && claude login`)
  - Credentials retrieved from macOS Keychain using `security` command
  - Expired Keychain tokens are rejected locally. When an API key, setup-token or cloud provider env var is set, Claude Code isn't refreshing the token, and the error says so (see `altAuthEnvVars`)
  - Never use `CLAUDE_CODE_OAUTH_TOKEN` for the usage call: `claude setup-token` hardcodes an inference-only scope, so the endpoint always returns 403
  - Returns exact reset times and combined web + CLI usage
- **Status line capture** (setup-token users): `ccu statusline` runs as Claude Code's status line command and saves `rate_limits` from its stdin JSON to `rate-limits.json` in the user cache dir. Claude Code holds these values in memory only (hooks don't receive them), so the status line is the only way out
  - The app loads the file only when OAuth has no data; `AppModel.GetOAuthData()` falls back to it, and `UsageData.Source` marks it
  - Each Claude Code session sends its own copy, which an idle session replays every `refreshInterval`. The file keeps each session's latest numbers (keyed by `session_id`); `record()` dates them only when `cost.total_api_duration_ms` grows or the numbers change, since first sightings may be replays of any age. `Load` runs `freshest()`: the session known to be most recent (the latest of its date and its windows' starts) wins even with lower usage, because Anthropic can reset a limit early; ties go to highest usage. Both windows come from that one session, since the payload doesn't identify the Claude account (mixing sessions showed a Max5 account's stale 5-hour window beside an Ultra account's weekly one). `Capture` holds an flock on `rate-limits.lock` around its read-modify-write. See `internal/statusline/`
- **JSONL fallback**: Parses `~/.claude/projects/**/*.jsonl` when OAuth unavailable
  - CLI-only activity (no web usage)
  - Automatically used when OAuth credentials missing or API fails

Data flow: OAuth/status line/JSONL → Analysis (sessions/weekly) → Bubbletea UI
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

**OAuth retry**: When OAuth is disabled it automatically retries after 5 minutes, including for token-expired errors (Claude Code usually refreshes the token itself). Only the missing-scope error (`oauth.ErrMissingScope`, fixed by `claude logout && claude login`) never auto-retries - see `oauth.RequiresUserAction()`.

**Per-model weekly limits come from `limits[]`**: The usage API returns `seven_day_sonnet` and
`seven_day_opus` as null now. Per-model weekly limits arrive as `weekly_scoped` entries in the
`limits` array, each naming its model via `scope.model.display_name`. Read them through
`UsageData.WeeklyModelLimits()`, which prefers `limits[]` and falls back to the legacy fields.
Never add a hardcoded top-level field for a new model - the array already handles them.

**Unknown models must still render**: Anthropic can scope a limit to a model CCU has never heard of.
`models.WeeklyHoursForModel()` returns 0 when a plan publishes no hour allowance; show the reset time
rather than deriving hours from a limit we don't know. `ui.GetModelColour()` falls back to
`ColorModelUnknown` for the same reason.

**Time handling**: All times converted to UTC. Session blocks round start times DOWN to current hour.

**Plan limits**: Claude has NO per-5-hour-session token limits. The ~200k context window is per-conversation, not per-session. `TokenLimit` field in `models.Limits` is always 0 for predefined plans.

**Ultra has no 5-hour session limit**: Anthropic renamed Max 20x to Ultra and dropped its session limit, but kept the `default_claude_max_20x` tier string. The usage API returns `five_hour: null` and Claude Code's status line sends only `seven_day`. `UsageData.FiveHour` is a pointer for this reason: check `HasSessionLimit()` before reading it (`EffectiveFiveHour` already returns zero values). In JSONL mode there's no API signal, so `Limits.NoSessionLimit` stands in. `-plan=max20` is an alias for `ultra`.
  - Known trade-off: Claude Code drops `five_hour` from the status line once it resets, so a Pro/Max5 status line capture can briefly hold only `seven_day` and read as "no session limit". The next response corrects it. Gating on the plan isn't reliable, because setup-token users often have no Keychain tier and default to `max5`.
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

<CHANGELOG>
Record notable changes in `CHANGELOG.md` under `## [Unreleased]`, grouped under Added/Changed/Fixed/Removed. Combine or update items refined within the same session. Never write version numbers or dates - git tags are the source of truth and CI auto-bumps them on every push to main. Before adding new entries, run `make stamp-version` to freeze any already-released `[Unreleased]` content against the latest tag (safe no-op when empty). Truncate the oldest releases if the file exceeds 2000 lines.
</CHANGELOG>
