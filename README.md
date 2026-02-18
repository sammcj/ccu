# CCU - Claude Code Usage Monitor

A performant terminal dashboard for monitoring Claude Code usage, built in Go using the Charm family of packages (Bubbletea, Lipgloss, Bubbles).

![screenshot](screenshot.png)

## Features

- **Live Claude API Usage Data** - Near real-time usage stats from Anthropic's servers with exact reset times
- **5-Hour Session Tracking**: Monitor rolling 5-hour usage windows with OAuth utilisation percentage
- **Weekly Limits**: Track 7-day rolling usage for Sonnet and Opus models separately
- **Real-time Dashboard**: Live updating terminal UI with colour-coded progress bars
- **Burn Rate Monitoring**: Dual burn rate display - tokens/minute and cost/hour with visual indicators
- **Intelligent Predictions**: Cost depletion time with colour-coded warnings (red if before reset, orange if close, green if safe)
- **Plan Support**: Pro, Max5, Max20
- **Automatic Fallback**: Uses OAuth when available, falls back to JSONL with raw cost/message display (no percentages)
- **Model Distribution**: See which models you're using per session (Sonnet, Opus, Haiku)

## Installation

### From Source

```bash
git clone <repository>
cd ccu
make build
./bin/ccu
```

### Using Go Install

```bash
go install ./cmd/ccu
```

## Usage

### Basic Usage

```bash
# Run with default settings (Max5 plan)
ccu

# Use a different plan
ccu -plan=pro
ccu -plan=max20

# Custom plan with specific limits
ccu -plan=custom -custom-tokens=50000 -custom-cost=25 -custom-messages=500

# Interactive view modes (TUI)
ccu -view=daily    # Daily aggregation
ccu -view=monthly  # Monthly aggregation

# Static reports (stdout, no TUI)
ccu -report=monthly              # Monthly usage report
ccu -report=weekly               # Weekly usage report (last ~13 weeks)
ccu -report=daily                # Daily usage report (last 30 days)
ccu -report=daily -hours=90      # Last 90 days

# Adjust refresh rate (1-60 seconds, default: 5)
ccu -refresh=10

# Load more history (for JSONL fallback mode)
ccu -hours=48      # Last 48 hours
```

### Command-Line Flags

- `-plan` - Plan type: `pro`, `max5`, `max20`, `custom` (default: `max5`)
- `-view` - View mode: `realtime`, `daily`, `monthly` (default: `realtime`)
- `-report` - Generate static report to stdout: `daily`, `weekly`, `monthly` (bypasses TUI)
- `-refresh` - UI refresh rate in seconds, 1-60 (default: `5`). Note: OAuth data is cached for 60 seconds regardless of UI refresh rate
- `-hours` - Hours of history to load from JSONL files (default: `24`, only used in fallback mode)
- `-data` - Path to Claude data directory (default: `~/.claude/projects`, only used in fallback mode)
- `-custom-tokens` - Custom token limit (requires `-plan=custom`)
- `-custom-cost` - Custom cost limit in USD (requires `-plan=custom`)
- `-custom-messages` - Custom message limit (requires `-plan=custom`)
- `-weekly` - Show weekly usage panel (default: `true`)
- `-api` - Enable the embedded HTTP API server (default: `false`)
- `-api-port` - API server port (default: `19840`)
- `-api-bind` - API server bind address (default: `0.0.0.0`)
- `-api-token` - Bearer token for API auth; empty means no auth
- `-api-allow` - Comma-separated CIDR allowlist, e.g. `192.168.1.0/24,10.0.0.1/32`; empty means allow all
- `-help` - Show help message
- `-version` - Show version information

### Keyboard Controls

- `q` or `Ctrl-C` - Exit the application

## HTTP API

CCU can expose its computed metrics over a local HTTP API. This is opt-in and disabled by default.

The API is intended for local network consumers such as an ESP32 display or a home-automation system. It serves the same data the TUI displays — burn rates, session/weekly utilisation, depletion predictions — with no additional API calls to Anthropic.

### Enabling

```bash
# Minimal: open to all local connections, no auth
ccu -api

# With bearer token auth
ccu -api -api-token=mysecret

# Restrict to a subnet
ccu -api -api-allow=192.168.1.0/24

# Combined: subnet + token, custom port
ccu -api -api-port=8080 -api-allow=192.168.1.0/24 -api-token=mysecret
```

Environment variables are also supported (CLI flags take precedence):

| Env var                                 | Equivalent flag |
| --------------------------------------- | --------------- |
| `CCU_API=true` or `CCU_ENABLE_API=true` | `-api`          |
| `CCU_API_PORT=19840`                    | `-api-port`     |
| `CCU_API_BIND=0.0.0.0`                  | `-api-bind`     |
| `CCU_API_TOKEN=secret`                  | `-api-token`    |
| `CCU_API_ALLOW=192.168.1.0/24`          | `-api-allow`    |

A token file at `~/.ccu/.api_token` is read as a fallback when neither flag nor env var sets a token.

### Endpoint

`GET /api/status` returns a JSON snapshot updated on every data refresh (every 60 seconds when OAuth is active).

```bash
curl -s -H "Authorization: Bearer mysecret" http://localhost:19840/api/status | jq .
```

Example response (fields are omitted when data is unavailable):

```json
{
  "plan": "max5",
  "server_time": "2026-02-18T12:00:00Z",
  "data_age_seconds": 15,
  "weekly": {
    "all_models": { "utilisation_pct": 30.5, "resets_at": "2026-02-25T00:00:00Z", "resets_in_seconds": 594000 },
    "sonnet":     { "utilisation_pct": 25.0, "used_hours": 52.5, "limit_hours": 210, "resets_at": "...", "resets_in_seconds": 594000 }
  },
  "session": {
    "utilisation_pct": 42.0,
    "resets_at": "2026-02-18T15:00:00Z",
    "resets_in_seconds": 10800,
    "elapsed_seconds": 7200,
    "total_seconds": 18000,
    "remaining_seconds": 10800,
    "remaining_pct": 60.0,
    "cost_usd": 14.70,
    "message_count": 312,
    "model_distribution": [
      { "model": "claude-sonnet-4", "cost_pct": 72.5 },
      { "model": "claude-opus-4",   "cost_pct": 27.5 }
    ]
  },
  "burn_rate": {
    "tokens_per_min": 850.0,
    "cost_per_min_usd": 0.048,
    "cost_per_hour_usd": 2.88
  },
  "prediction": {
    "session_limit_at": "2026-02-18T19:30:00Z",
    "session_limit_in_seconds": 27000,
    "session_will_hit_limit": false,
    "weekly_will_hit_limit": false
  }
}
```

Returns `503` with `{"error":"no data"}` before the first data load completes.

### Security

- If both `-api-allow` and `-api-token` are unset, CCU logs a warning at startup. Only do this on a fully trusted, isolated network.
- IP allowlist is checked before auth so the existence of an auth requirement is not leaked to blocked hosts.
- `weekly` fields are only present when OAuth is active; they are omitted in JSONL-only mode.

## How It Works

### Data Sources

CCU uses **two data sources** for maximum accuracy:

#### 1. OAuth API (Primary - Requires Re-authentication)

CCU automatically fetches live usage data from Anthropic's servers when available:
- **Exact reset times** for 5-hour sessions and 7-day windows
- **Combined web + CLI usage** tracking

To enable OAuth you may need to re-authenticate Claude Code to get tokens with the required scopes:
```bash
claude logout
claude login
```

After re-authentication, CCU will automatically use OAuth data when available.

#### 2. Local JSONL Files (Degraded Fallback)

If OAuth is unavailable, CCU reads from `~/.claude/projects/**/*.jsonl` files and shows a degraded view:
- Raw session cost and message count (no progress bars or percentages)
- Burn rate (tokens/min, $/hr) -- still accurate from local data
- Session model distribution
- Time before reset (estimated from session blocks)

**Limitations**: JSONL files only contain CLI activity (no web usage). Usage percentages, weekly tracking, predictions, and limit warnings require OAuth. When OAuth fails due to a transient error, CCU automatically retries after 5 minutes.

### OAuth vs JSONL

| Feature              | OAuth API    | JSONL Fallback  |
| -------------------- | ------------ | --------------- |
| Web + CLI tracking   | ✅ Yes        | ❌ CLI only      |
| Usage percentages    | ✅ Yes        | ❌ Not available |
| Weekly limits        | ✅ Yes        | ❌ Not available |
| Predictions/warnings | ✅ Yes        | ❌ Not available |
| Burn rates           | ✅ Yes        | ✅ Yes           |
| Session distribution | ✅ Yes        | ✅ Yes           |
| Exact reset times    | ✅ Yes        | ⚠️ Estimated     |
| Setup required       | Re-auth once | None            |

### Session Blocks

Usage is grouped into 5-hour rolling session blocks. Each session tracks:
- Total tokens consumed
- Cost accumulated
- Messages sent
- Per-model statistics
- Burn rate (tokens/minute)

### Burn Rate Calculation

Burn rates are calculated using a proportional overlapping session method over the last hour:
- For each session overlapping the last 60 minutes, calculate the overlap duration
- Apply proportion of session's tokens/cost based on overlap
- Prevents double-counting when sessions overlap
- Displayed as: tokens/minute and dollars/hour

**Visual Indicators**: Burn rate bars use green→yellow→orange→red gradient based on intensity (percentage of limit at current rate).

### Predictions

The tool provides intelligent cost depletion predictions:
- **Cost Depletion Time**: When you'll hit the cost limit based on current burn rate (calculated from last hour)
- **Session Reset Time**: When the 5-hour window expires (from OAuth or estimated)

**Colour-Coded Warnings**:
- 🔴 **Red**: Depletion time is BEFORE reset (you'll be cut off)
- 🟠 **Orange**: Depletion time is within 30 minutes AFTER reset (close call - usage spike could cut you off)
- 🟢 **Green**: Depletion time is 30+ minutes after reset (safe)

Cost-based predictions are more accurate than token-based for mixed-model usage because different models have different costs per token.

### Weekly Tracking

Weekly limits are tracked over a rolling 7-day window:
- Separates Sonnet and Opus usage
- Converts tokens to estimated hours (Sonnet: ~5k tokens/hour, Opus: ~3k tokens/hour)
- Displays progress against plan limits

## Plan Limits

As of 2025-12-01

### Pro Plan
- Cost: $18 per 5-hour session
- Messages: ~250 per 5-hour session (estimated)
- Weekly: ~60 hours Sonnet

### Max5 Plan
- Cost: $35 per 5-hour session
- Messages: ~1,000 per 5-hour session (estimated)
- Weekly: ~210 hours Sonnet, ~25 hours Opus

### Max20 Plan
- Cost: $140 per 5-hour session
- Messages: ~2,000 per 5-hour session (estimated)
- Weekly: ~360 hours Sonnet, ~32 hours Opus

## Customisation

### Colours

All colours and their mappings are centralised in `internal/ui/styles.go` (lines 9-127):

- **Colour definitions**: Edit hex values for model colours (Sonnet/Opus/Haiku), UI elements, and gradients
- **Element mappings**: Documentation showing which UI element uses which colour (weekly bars, burn rates, session usage, time remaining, predictions)

To customise colours:
1. Edit `internal/ui/styles.go`
2. Find your element in the mappings section (lines 92-127)
3. Change the corresponding colour value (lines 16-90)
4. Run `make build`

### Project Structure

```
ccu/
├── cmd/ccu/          # Entry point
├── internal/
│   ├── api/          # Optional embedded HTTP API server
│   ├── app/          # Bubbletea application (MVU pattern)
│   ├── oauth/        # OAuth client for Anthropic API
│   ├── data/         # JSONL reading and parsing (fallback)
│   ├── analysis/     # Session blocks, burn rate, predictions
│   ├── models/       # Data structures
│   ├── pricing/      # Model pricing calculations
│   ├── ui/           # Dashboard rendering and colour logic
│   └── config/       # Configuration management
└── Makefile
```

## Licence

- [Apache 2.0](LICENSE)
