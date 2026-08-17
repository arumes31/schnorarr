# Global Traffic Shaping in the UI — Design

Date: 2026-08-17
Status: Approved (brainstormed and validated section by section)

## Goal

Make the global bandwidth limit adjustable at runtime from the dashboard —
both manually and on a quiet-hours schedule — with the limit treated as a
**shared pool** across all concurrently transferring engines. Today the limit
is fixed at startup from `BWLIMIT_MBPS` and applied per rsync process.

## Constraints & Background

- Remote transfers run through the `rsync` subprocess; `--bwlimit` is fixed
  per invocation and cannot be changed mid-flight. A literal shared token
  bucket would require replacing rsync (rejected: too large a blast radius).
- Every file is its own rsync invocation (`Transferer.CopyFile` →
  `copyRemote`), so shares rebalance naturally at file boundaries.
- `Transferer.SetBandwidthLimit` exists (`internal/sync/transfer.go:691`) but
  is never called and is not thread-safe.
- `internal/monitor/scheduler` is dead code: it writes `/config/bwlimit`,
  which nothing reads, and it is not wired into `main.go`. It gets rewired,
  not rewritten.
- Settings persistence already has two patterns: `database.SaveSetting`
  (SQLite) and `/config/config.json` (`internal/monitor/config`). The webhook
  config shows the env-fallback pattern: file value wins, env is fallback.

## Approach: Bandwidth Manager with Dynamic Division

A coordinator owns the global limit and the set of active transfers. On every
change (limit set, transfer starts/stops) it recomputes each active engine's
share (`global ÷ n active`) and pushes it down before the engine's next file
transfer. The actual total converges to the global limit within a file or
two; a file mid-flight keeps its old share until it finishes (documented,
accepted behavior).

## Components

### `BandwidthManager` (new, e.g. `internal/sync/bwmanager.go`)

```go
type BandwidthManager struct {
    mu       sync.Mutex
    limitBps int64            // global limit, bytes/sec; 0 = unlimited
    active   map[int]struct{} // engine IDs currently transferring
    engines  map[int]*Engine  // registry for pushing shares down
}
```

- `SetGlobalLimit(bps int64)` — recompute shares.
- `Acquire(engineID)` / `Release(engineID)` — mark transfer batch start/end,
  recompute shares. `Release` must be deferred at the call site.
- `recompute()` — `share = limitBps / len(active)`, floored at 1 KB/s when a
  limit is set (rsync `--bwlimit=0` would mean unlimited); push to each
  active engine via `engine.SetBandwidthLimit(share)`.

### Engine / Transferer changes

- `Engine.SetBandwidthLimit(bps int64)` delegating to the transferer.
- Make `Transferer.SetBandwidthLimit` (and reads of `opts.BandwidthLimit`)
  mutex-guarded — it is now called concurrently with running transfers.
- Engines register with the manager at creation instead of receiving a static
  limit. `Acquire`/`Release` wrap the transfer batch in the engine's
  execution path.
- `transfer.go:92`: parallel copy is currently disabled whenever a limit is
  set. Under the manager, parallel stays enabled and each worker uses the
  divided share. One behavioral side effect to watch.

### Startup wiring (`internal/app/sender.go`)

- `BWLIMIT_MBPS` becomes the manager's initial value; a saved value in
  `/config/config.json` overrides it on boot (file wins, env fallback — same
  pattern as the Discord webhook).

### API & persistence (`internal/monitor/handlers/api.go`, `config/config.go`)

- `POST /api/settings/bwlimit` — parse Mbps (validate: integer ≥ 0), convert
  `× 125000` (same formula as today), `manager.SetGlobalLimit()`, persist to
  `config.json`, `database.LogSystemEvent`, JSON response. Mirrors the
  existing `UpdateAutoApprove` handler shape.
- Extend `Config` with a `bwlimit_mbps` field (file wins over env on boot).
- Extend the existing `SetScheduler` handler to save all quiet-hours fields
  (`scheduler_enabled`, `quiet_start`, `quiet_end`, `quiet_limit`,
  `normal_limit`) with `HH:MM` validation (400 on invalid input).

### Scheduler (`internal/monitor/scheduler`)

- Rewire: call `manager.SetGlobalLimit()` instead of writing
  `/config/bwlimit` + reload func. Keep the existing quiet-window logic
  (including midnight crossing). Wire it into app startup.
- Interaction rule: scheduler and manual setting share one "current limit."
  The scheduler overwrites the manual value at window boundaries; the UI
  shows the source of the current value ("manual" / "schedule").

### UI (`internal/ui/web/templates/index.html` + static JS)

- "Traffic Shaping" card in the settings area: global Mbps input
  (0 = unlimited), apply button, live readout of the effective limit and the
  number of transfers sharing it (pushed over the existing WebSocket with
  engine stats).
- Quiet-hours block: enabled checkbox, start/end time, quiet Mbps, normal
  Mbps.
- Sender mode only — hidden/disabled in receiver mode, like other
  sender-only UI.

## Edge Cases

- Share below 1 KB/s → floor at 1 KB/s and log (never emit `--bwlimit=0`
  when a limit is meant).
- Limit set to 0 → shares become 0 (unlimited), parallel fast path
  re-engages, mid-flight rsync keeps its old limit until the file finishes.
- Crashed transfer must not leak an `Acquire` slot — `Release` deferred.
- Invalid scheduler times → rejected at the handler; last-known-good stays
  on disk.
- Both env and saved config present → saved config wins; README stays
  accurate (env documented as fallback).

## Testing

- `bwmanager_test.go` (table-driven): limit set, acquire/release N engines,
  each engine receives `limit/n`; mid-flight limit change re-pushes shares;
  release-all returns to unlimited; tiny-limit floor.
- Handler tests (pattern of existing `auth_test.go`): valid/invalid Mbps,
  scheduler field validation.
- Scheduler test: quiet-window boundary crossing midnight.
- Manual smoke via `docker-compose.test.yml`: two senders, 50 Mbps global,
  combined throughput converges to ~50 after a file boundary.

## Out of Scope

- Exact/instant shaping (would require replacing rsync or `tc` — rejected).
- Per-engine individual limits (the shared pool is the feature).
