# CCU Code Review

Reviewed on 2026-04-19 against `main`. Severity tags: **CRITICAL** (correctness/data-loss/security), **MEDIUM** (real bug with workaround or limited blast radius), **LOW** (polish, tests, minor inefficiency).

Each finding lists file paths without line numbers so it survives churn.

## Fixed in this pass

The following items were addressed without changing external behaviour or caching semantics:

- **#1** stale model pointer — `loadDataCmdWithModel` now does every model read and mutation synchronously, so state persists via `Update`'s returned model. No behavioural change; the closure still fetches OAuth and JSONL on the same schedule.
- **#3** DisplayTokens vs TotalTokens parity tests added (`models/entry_test.go`).
- **#4** JSONL parse/read errors now counted and logged once per refresh via `log.Printf` to `~/Library/Caches/ccu/ccu.log`. File-skip semantics unchanged.
- **#5** scanner buffer allocated once per `ReadUsageEntries` call instead of once per file. The 10 MiB cap is preserved.
- **#6** `-api-port` / `-api-bind` now use `flag.Visit` to detect explicit CLI flags, matching `-plan`'s behaviour.
- **#7** `parseRetryAfter` uses `http.ParseTime`, covering IMF-fixdate, RFC 850 and asctime.
- **#10** `p90_test.go` added covering empty/single/duplicate/unsorted inputs, the floor cap, and the active/gap exclusion invariant.
- **#11** `TestCalculateBurnRate_ProportionalOverlap` added covering the five cases the docstring calls out.
- **#13** API response write errors now logged (`internal/api/server.go`).
- **#15** OAuth 4xx/5xx responses now include a body snippet when the payload isn't JSON, giving real diagnostics instead of `map[]`.
- **#17** indentation in the `CCU_API` / `CCU_ENABLE_API` env block corrected.
- **#23** `IsTransientError` prefers `errors.Is`/`errors.As` for `context.DeadlineExceeded`, `io.ErrUnexpectedEOF`, `net.Error.Timeout()` etc; substring match retained as a fallback.
- **#24** `pricing.init()` panics at startup if the fallback model is ever removed from `ModelPricing`, and the name is now a named constant.

Tests: `go test -race ./...` green across all packages (9 of 9). `golangci-lint run ./...` reports zero issues.

## Deliberately not changed (would alter behaviour or surface area)

- **#2** `globalLoadCache` stays module-level — the user asked that caching behaviour not change.
- **#8** default API bind address stays `0.0.0.0` — changing this would break existing users with `-api` in their scripts.
- **#9** P90 hardcoded caps — plan-configurable wiring is a larger change.
- **#12** `NormaliseModelName` unknown-case behaviour kept as-is.
- **#14** rate-limit warning auto-clear via `tea.Tick` — would change UI timing.
- **#16** OAuth warning banner — UX change; captured in the review for later discussion.
- **#19**, **#20**, **#21**, **#22** — polish items left for follow-up.

---

## Critical

### 1. Stale model pointer captured by the async data-load closure

`internal/app/app.go` — `AppModel.Update` uses a value receiver (`func (m AppModel) Update`). In the `tea.KeyMsg`, `tea.FocusMsg`, `resumeMsg` and `tickMsg` branches it calls `loadDataCmdWithModel(m.config, &m)`, handing the closure a pointer to the local copy of `m`. Once `Update` returns, Bubbletea replaces the model with whatever the handler returned, but the closure still holds the original pointer. Consequences:

- `isOAuthSessionStale(model)` calls `model.SetZeroRemainingStart(...)` / `model.ClearZeroRemainingStart()` inside the closure (via pointer methods). Those writes mutate the discarded copy and are silently lost.
- Reads of `model.oauthData`, `model.lastOAuthFetch`, `model.oauthRateLimitUntil`, `model.lastRefresh` inside the closure see a stale snapshot, so rate-limit back-offs, "just fetched" detection, and stale-session detection can disagree with the live model.
- It also constitutes a data race if Bubbletea ever parallelises command execution (it does, on its own goroutine) with a subsequent `Update`.

Fix: pass value-typed fields the closure actually needs (rate-limit until, last fetch, cached OAuth data) as captured locals, and return mutations back via the `dataLoadedMsg` for the next `Update` to apply.

### 2. Global mutable parse cache couples callers and tests

`internal/data/reader.go` uses a package-level `globalLoadCache`. Anything in the process (TUI, embedded API, report mode, tests) shares the same cached slice and mutex. Tests that change `hoursBack` or mutate files leak state across `t.Run` boundaries, and there is no way to force a refresh without importing the internal package. Fix: make the cache an instance held by `AppModel` (or by a `*data.Loader`) and inject it.

### 3. `DisplayTokens` vs `TotalTokens` Python-parity claim is untested

`internal/models/entry.go` and `CLAUDE.md` both call this distinction out as critical, but no unit test asserts that `DisplayTokens() == Input+Output` and excludes cache tokens, nor that `TotalTokens()` includes them. Add a table-driven test with non-zero cache values so future refactors can't silently break the parity contract.

---

## Medium

### 4. Silent swallowing of parse/read errors in JSONL ingestion

`internal/data/reader.go`:

- `ReadUsageEntries` skips any file that fails to open or scan with no log and no counter.
- `readJSONLFileWithFilter` discards malformed-JSON errors from `ParseJSONLLine` with the comment "Suppress warnings for cleaner output".

Combined, a single corrupt file or oversized line can silently drop hours of data. At minimum, keep running counters (`skippedFiles`, `parseErrors`, last error seen) and surface them through the loader API so the TUI/CLI can render a small warning. `lineNum` is already being incremented but never used — wire it into the error context.

### 5. 10 MB scanner buffer allocated per file

`readJSONLFileWithFilter` allocates `make([]byte, 10*1024*1024)` on every file open. With ~2k JSONL files under `~/.claude/projects` this is ~20 GB of cumulative allocations per full reload (GC-reclaimable, but wasteful). Allocate one buffer per `ReadUsageEntries` call and reuse via `scanner.Buffer(buf, cap)` — the buffer only needs to accommodate the longest single JSONL line.

### 6. Flag/env precedence inversion for `-api-port` and `-api-bind`

`internal/config/config.go` detects "flag was explicitly set" by comparing against the default value. If a user passes `-api-port=19840` (the default) or `-api-bind=0.0.0.0` (the default), the code falls through and lets `CCU_API_PORT` / `CCU_API_BIND` override them. The `-plan` flag uses `flag.Visit` to solve the same problem correctly; apply the same pattern here.

### 7. `parseRetryAfter` covers RFC 1123 but not RFC 7231 IMF-fixdate

`internal/oauth/client.go` falls back to `time.Parse(time.RFC1123, header)`. Real-world `Retry-After` date forms are IMF-fixdate (RFC 7231 §7.1.3), which is `time.RFC1123` only when the timezone is `GMT` — `time.RFC1123Z` / `http.ParseTime` is the correct choice. Switch to `http.ParseTime`.

### 8. HTTP API: unauthenticated bind warning fires but binding still occurs

`internal/api/server.go` logs a WARN when `AllowedCIDRs` is empty and `Token` is unset, then binds `0.0.0.0:19840` anyway. For a tool primarily consumed on a developer's workstation this is risky — anything on the local network can poll the snapshot. Options:

- Default `BindAddr` to `127.0.0.1` and require an explicit `-api-bind=0.0.0.0` to open it up.
- Refuse to start when both the token and allowlist are empty unless `-api-unsafe` (or similar) is passed.

### 9. Hardcoded P90 knobs

`internal/analysis/p90.go` hardcodes plan caps `[19_000, 88_000, 220_000]` and the `0.95` threshold. When Anthropic adjusts a plan, this needs a recompile. Thread these through `models.Limits` (or a `P90Config` built from plan) rather than baking them in.

### 10. Missing quantile/P90 tests

There is no `p90_test.go`. `quantile` needs coverage for `n==0`, `n==1`, duplicates, and `q∈{0, 0.5, 0.9, 0.99, 1}`; `CalculateP90Limit` needs coverage for the hardcoded-cap branch.

### 11. `CalculateBurnRate` overlapping-sessions math lacks test

The proportional overlap logic is called out in `CLAUDE.md` as the primary prediction input but has no direct unit test. Add tests for: (a) a single session spanning the entire 1-hour window; (b) two sessions that overlap the window partially; (c) a session shorter than the window; (d) an expired gap block.

### 12. `NormaliseModelName` passes unknown values through unchanged

`internal/models/entry.go` returns the input `model` (preserving case) whenever no substring matches. So `"Opus-Like-Model"` is kept as-is but `"opus-3"` is normalised to `"claude-3-opus"`, which makes grouping inconsistent. Either lowercase the default-case return or map unknown models to a sentinel (`"unknown"`) so the UI can display them distinctly.

### 13. Weekly write error paths in `api/server.go` are silent

`handleStatus` does `_, _ = w.Write(...)` for both the error and success responses. Client disconnects or buffer failures vanish. Log (don't propagate) write errors so they show up in `~/Library/Caches/ccu/ccu.log`.

---

## Low

### 14. Rate-limit warning can outlive its expiry on-screen

`AppModel.ClearExpiredRateLimitWarning` is never scheduled. The warning only disappears when the next Bubbletea message forces a re-render — on an idle terminal it can linger past its expiry. Schedule a `tea.Tick` when `SetRateLimitWarning` is called to clear it on time.

### 15. OAuth decode error ignored in failure path

`oauth/client.go` uses `_ = json.NewDecoder(resp.Body).Decode(&errorBody)`. When the body is malformed the returned error string says `API returned status N: map[]`, which is unhelpful for support. Capture the decode error and include the first ~200 bytes of the raw body.

### 16. OAuth errors not surfaced in the UI

`AppModel.getOAuthUnavailableReason` returns only "rate limited", "loading…", or the stored reason. A failing `keychain access` or a persistent non-transient 4xx is logged once to `ccu.log` but the user sees no banner, only that OAuth data is missing. Consider a single-line warning row in the dashboard (truncated reason) with a `?` key to toggle detail.

### 17. Weird indentation in `config.go` API env block

`internal/config/config.go` around `CCU_API` / `CCU_ENABLE_API` has misaligned bodies. Reformat — `gofmt`/`golangci-lint` should catch it.

### 18. Unused `lineNum` in JSONL reader

`readJSONLFileWithFilter` increments `lineNum` but never references it. Either drop it or use it in the (future) error log.

### 19. SIGCONT handler is UNIX-specific

`cmd/ccu/main.go` imports `syscall.SIGCONT` unconditionally. The build is Go-portable so this will fail on Windows. Gate the SIGCONT handler behind `//go:build unix` (or `!windows`) if cross-platform builds matter; otherwise declare macOS/Linux-only in the README.

### 20. Duplicated version vars

`Version`, `Commit`, `BuildDate` live in both `cmd/ccu/main.go` and `internal/config/config.go`. One is the ldflags target; the other gets copied at runtime. Either expose them only from `config` and inject via `-X github.com/sammcj/ccu/internal/config.Version=...`, or drop the config-package copies.

### 21. `sessions_realdata_test.go` depends on local fixture

If the file requires `~/.claude/projects` to be populated, gate it with `testing.Short()` / `t.Skip()` when the path is missing so CI doesn't fail on clean runners.

### 22. Magic intervals strewn across `app.go`

`2 * time.Minute` (sleep detection), `15 * time.Minute` (weekly fetch), `5 * time.Minute` (zero-remaining stall), `30 * time.Second` (manual-refresh backoff reset) are inline literals. Promote to named consts next to `oauthNormalInterval` for a single knobs-panel.

### 23. `oauth.IsTransientError` substring-matching is fragile

`internal/oauth/client.go` lowercases and substring-searches the error text for "network", "timeout", "dial", "eof", etc. This breaks silently if the Go stdlib error wording changes. Prefer `errors.Is(err, context.DeadlineExceeded)`, `errors.As(err, &netErr)` / `netErr.Timeout()`, and `errors.Is(err, io.ErrUnexpectedEOF)` where possible; keep substring match as a last resort.

### 24. `findReferences`-style rename safety for `NormaliseModelName` defaults

Pricing fallback in `internal/pricing/pricing.go` hardcodes `"claude-sonnet-4-6"`. If `NormaliseModelName` ever stops returning exactly that string the fallback silently returns $0. Add an `init()` that asserts the fallback key exists in `ModelPricing` to fail fast.

---

## Verified false positives from first-pass review

These were flagged by the initial scan but are actually correct:

- **OAuth shell injection in `getKeychainCredentials`** — `exec.Command("security", ...)` passes args as a slice; no shell interpreter, no injection path.
- **HTTP listener leak in `api/server.go`** — `http.Server.Serve(ln)` closes the listener on return, so the `net.Listen` handle is not leaked.
- **Race on `allowedNets`** — populated once in `New` before the server goroutine starts; reads are safe without a lock.
- **Shutdown context disconnected from parent** — intentional; once `ctx` is cancelled we want a fresh 5-second window to drain, not "already cancelled".
- **`extractBearerToken` panic on short header** — guarded by `len(auth) > len(prefix)` already.
- **`-help` flag never wired up** — `config.ParseFlags` at lines 51–54 handles `*showHelp` correctly.
- **Spinner tick chain never stops** — `case spinner.TickMsg` drops the message when `!IsLoading()`, ending the chain.

---

## Suggested fix order

1. #1 (stale pointer) — silent correctness bug, touches every refresh path.
2. #2 (global load cache) — unblocks clean tests and simplifies #1.
3. #8 (default bind address) — security posture.
4. #4 + #18 (parse-error visibility) — helps diagnose production issues users report.
5. #6 + #7 (flag precedence, `Retry-After` parsing) — small, self-contained.
6. Tests: #3, #10, #11.
7. Everything else as polish.

Run `make lint` after #17 and `make test` after each of the test additions to keep the tree green.
