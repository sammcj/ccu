# Changelog

<!-- AI agents: first run `make stamp-version` to freeze any already-released [Unreleased] content against the latest git tag (safe no-op when empty), then add entries under the ## [Unreleased] header. Do NOT add version numbers or dates - git tags are the source of truth and versions are stamped at release time. Do NOT duplicate headings. Group entries under ### Added, ### Changed, ### Fixed, or ### Removed. Combine or update items refined within the same session. If the file exceeds 2000 lines, truncate the oldest releases. -->

## [Unreleased]

### Added

- Weekly usage bars for any model Anthropic scopes an individual weekly limit to, driven by the `limits` array the OAuth usage API now returns. Fable's weekly limit shows up automatically, as will any future model's, with no code change
- Colours for the Fable (hot pink) and Mythos (red) model names, plus a neutral fallback colour for models CCU doesn't recognise

### Changed

- A per-model weekly bar omits its reset time when it matches the All Models row, so the column only draws attention to a model resetting on its own schedule
- `GET /api/status`: the `weekly.sonnet` and `weekly.opus` objects are replaced by a `weekly.scoped` map keyed by lowercased model name (suffixed `/<surface>` for surface-scoped limits), with a `model` field carrying the API's display name. `used_hours` and `limit_hours` are now omitted for models with no published hour allowance rather than reported as zero

### Fixed

- Weekly usage section now always shows when OAuth data is available, instead of only appearing once the API starts returning per-model (Sonnet/Opus) weekly fields
- Per-model weekly bars no longer disappear when Anthropic nulls the legacy `seven_day_sonnet` / `seven_day_opus` response fields, which it now does

## [0.2.6] - 2026-07-03

### Added

- Test suites for the JSONL reader/parser (`internal/data`) and configuration handling (`internal/config`), previously untested
- AI-managed changelog with `make version` / `make stamp-version` targets for release stamping

### Changed

- Changelog conventions: agents stamp already-released content before adding entries; Known Bugs section removed
- JSONL parsing caches per file: active use re-parses only the file that changed instead of the whole window each refresh
- Dashboard rendering aggregates per-model stats instead of iterating every usage entry; session blocks are only rebuilt when usage data actually changes
- Session blocks no longer retain a copy of every usage entry, roughly halving entry memory
- OAuth keychain availability is cached for 60s so refresh ticks no longer spawn a `security` subprocess each time
- Local API server sets HTTP timeouts, logs startup failures to the log file instead of corrupting the TUI, and is awaited during graceful shutdown
- `ccu.log` rotates to `ccu.log.old` at 10MB on startup
- Burn-rate and stale-utilisation clamp logic consolidated into single tested implementations (previously duplicated across up to five call sites)

### Fixed

- TUI no longer freezes when the macOS keychain is locked: keychain lookups time out after 5 seconds
- A transient JSONL load failure no longer locks the UI on a permanent error screen or discards OAuth data fetched in the same refresh
- OAuth errors requiring re-authentication (missing `user:profile` scope) are now correctly classified and no longer retry every 5 minutes
- A single oversized JSONL line no longer discards the whole file's valid usage data
- Files that fail to open transiently are retried on the next refresh instead of being cached as permanently empty
- Slow in-flight data loads can no longer overwrite newer results
- Daily/monthly views no longer render current model names as "Mixed"
- Current-session data served via the API can no longer go stale after a session rebuild

### Removed

- Dead code: unused progress-bar renderers, the p90 plan-detection module, the legacy JSONL read pipeline, and unused styles and model accessors
