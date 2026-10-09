# Changelog

<!-- AI agents: first run `make stamp-version` to freeze any already-released [Unreleased] content against the latest git tag (safe no-op when empty), then add entries under the ## [Unreleased] header. Do NOT add version numbers or dates - git tags are the source of truth and versions are stamped at release time. Do NOT duplicate headings. Group entries under ### Added, ### Changed, ### Fixed, or ### Removed. Combine or update items refined within the same session. If the file exceeds 2000 lines, truncate the oldest releases. -->

## [Unreleased]

### Changed

- The 1H Cache comparison on the `Session - Cache Hit` row includes the session cost so far, so the difference has a scale: `[1H Cache: 100% (+$3.93 of $98.12 vs 5m)]`

### Fixed

- The 1H cache comparison measured the gap between requests from the previous response's last transcript line, which can land minutes after the request started. The cache TTL runs from the start of a request, so gaps are now measured from each response's first line, which slightly undercounted 5-minute cache misses before

## [0.2.17] - 2026-10-09

### Added

- Pricing for Sonnet 5.5 ($2 input, $10 output, $0.10 cache read) and Haiku 5.5 ($0.10 input, $0.50 output, $0.01 cache read)
- Prompt-length pricing tiers. A Haiku 5.5 request whose prompt (input plus cache write and cache read tokens) is over 100,000 tokens is costed at the higher rates ($0.50 input, $2.50 output), per request. Tiers on Anthropic's pricing page are read the same way
- `Session - Cache Hit` row shows the share of the session's cache writes that went to the 1-hour cache, and what the session cost against the 5-minute cache, e.g. `[1H Cache: 100% (+$11.75 vs 5m)]`. The comparison replays each conversation under a 5-minute cache: a cache read more than 5 minutes (and at most an hour) after the previous request is costed as a rewrite. Green when the 1-hour cache paid off, amber when the 5-minute cache would have been up to 5% cheaper, orange beyond that

### Changed

- The built-in fallbacks for unversioned `sonnet` and `haiku` are now the 5.5 releases, used until ccu has read the pricing page
- Unknown Sonnet and Haiku versions are estimated at their family's 5.5 rates

### Fixed

- 1-hour cache writes are costed at 2x the input rate. Every cache write was costed at the 5-minute rate (1.25x), but Claude Code writes most of its cache to the 1-hour tier, so costs read low (16.6% over a sample 30 days). The split comes from each response's `usage.cache_creation`; entries without it are costed as 5-minute writes
- Fast mode requests (`usage.speed: "fast"`) are costed at the fast mode premium, 2x every rate on Opus 5.5, Opus 5 and Opus 4.8. They were costed at standard rates. Models priced from Anthropic's pricing page take their premium from its fast mode table

## [0.2.16] - 2026-10-09

### Added

- Pricing for Opus 5.5 ($4 input, $20 output, $0.20 cache read per million tokens). It previously fell back to Opus family rates with a warning
- Models missing from the built-in pricing table are priced from Anthropic's pricing page, cached for 24 hours. Family rates remain the fallback when the page is unreachable or doesn't list the model
- Per-model weekly limits (Fable and any other `weekly_scoped` limit) now get a depletion prediction on the same terms as the All Models weekly limit. The prediction line shows `Weekly Fable limit: <time>` when the model's cap will be hit before its own reset, and the API's `weekly.scoped[*]` entries gain `limit_at`, `limit_in_seconds` and `will_hit_limit`

### Changed

- An unversioned model name (`sonnet`, `opus`, ...) resolves to the newest generally available release of that family on Anthropic's pricing page, so a release like Sonnet 5.5 is picked up without a ccu update. ccu refreshes the page in the background at startup once its cached copy is a day old
- The built-in fallback for an unversioned `opus` is now Opus 5.5, used until ccu has read the pricing page
- Unknown Opus versions are estimated at Opus 5.5 rates

### Fixed

- Mythos 5.1 cache reads are $0.25 per million tokens, matching Anthropic's pricing page (was $1.00)
- Dashboard rows are clipped to the terminal width. A session spanning three or more models made the `Session - Usage` distribution wrap, which shifted every row below it and left the previous frame's rows on screen as duplicates
- `make install` removes the old binary before copying so macOS doesn't SIGKILL the first launch on a cached-signature mismatch

## [0.2.13] - 2026-09-16

### Added

- Pricing for Fable 5.1 and Mythos 5.1. Fable 5.1 cache reads are $0.25 per million tokens, down from $1.00 on Fable 5

### Changed

- An unversioned `fable` or `mythos` model name now resolves to 5.1
- The unpublished-pricing notice now says which family rate stood in for a model CCU has no exact rate for (a future Fable 5.2 or Fable 6 is costed at current Fable rates and the notice says so). Only an unrecognised, non-Claude model is still labelled an outright estimate

## [0.2.11] - 2026-09-02

### Changed

- Opus model names render in a deeper orchid instead of the previous pale pink


## [0.2.11] - 2026-07-27

### Fixed

- Opus 5 was detected as Opus 4 and costed at $15/$75 per million tokens instead of $5/$25, overstating every Opus 5 cost by 3x and skewing burn rate and depletion predictions. Sonnet 5 and Haiku 5 were similarly mapped to older generations. Model normalisation now parses the version out of the model ID instead of matching against a list of known versions, so a model released after this code was written normalises to its own name rather than to its family's oldest generation

- Model IDs that put the version before the family (`claude-4-opus-20250514`, which Anthropic still publishes) were read as unversioned and priced as the newest model in that family. Both ID shapes are now parsed

### Added

- Pricing for Opus 5 and Sonnet 5, including Sonnet 5's introductory rate of $2/$10 per million tokens
- Per-family fallback pricing, used when a model normalises to a version with no published rate. A new Opus version now costs Opus rates rather than falling through to the Sonnet fallback
- The dashboard names any model CCU has no published rate for and marks the costs as estimates, instead of presenting a fallback rate as measured
- A weekly CI job runs `ccu -check-models` and opens an issue when the pricing tables drift from upstream

### Changed

- An unversioned model name (`opus`, `sonnet`, `fable` - these appear in some Claude Code JSONL entries) now resolves to that family's newest release rather than its oldest


## [0.2.6] - 2026-07-27

### Changed

- The session cache hit rate row moves up to sit directly below the burn rate rows, above session usage
- Renamed the "Time Before Reset" row to "Session - Reset" so it groups with the other session rows


## [0.2.6] - 2026-07-10

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
