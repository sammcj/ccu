# Changelog

<!-- AI agents: add entries under the ## [Unreleased] header. Do NOT add version numbers or dates - git tags are the source of truth and versions are stamped at release time. Do NOT duplicate headings. The ## Known Bugs section must always stay pinned above ## [Unreleased]. Group entries under ### Added, ### Changed, ### Fixed, or ### Removed. Combine or update items refined within the same session. If the file exceeds 2000 lines, truncate the oldest releases. -->

## Known Bugs

## [Unreleased]

### Added

- Test suites for the JSONL reader/parser (`internal/data`) and configuration handling (`internal/config`), previously untested
- AI-managed changelog with `make version` / `make stamp-version` targets for release stamping

### Changed

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
