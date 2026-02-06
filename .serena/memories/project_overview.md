# CCU - Claude Code Usage Monitor

Terminal dashboard for monitoring Claude Code usage. Dual data source: Anthropic OAuth API (primary) with JSONL fallback.

## Tech Stack
- Go 1.25.4
- Charm ecosystem: Bubbletea (TUI), Lipgloss (styling), Bubbles (components)
- Testing: testify/assert
- Platform: macOS (uses Keychain for OAuth credentials)

## Architecture
- `cmd/ccu/` - CLI entry point
- `internal/oauth/` - OAuth API client (Anthropic usage API)
- `internal/data/` - JSONL log parser (fallback)
- `internal/analysis/` - Session blocks, burn rates, predictions
- `internal/pricing/` - Model-specific token pricing
- `internal/models/` - Data structures (SessionBlock, UsageEntry, etc.)
- `internal/ui/` - Dashboard rendering (dashboard.go, styles.go)
- `internal/app/` - Bubbletea app model and update loop
- `internal/config/` - Configuration handling

## Key Conventions
- `DisplayTokens` = input+output only (UI display). `TotalTokens` = all including cache (cost calc).
- Model normalisation via `NormaliseModelName()`.
- Session start times rounded DOWN to current hour.
- Cost-based prediction is primary (different model prices).
- Burn rate uses proportional overlapping sessions.
- All times converted to UTC internally.
