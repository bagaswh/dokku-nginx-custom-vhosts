# Spec: Automatic Rollback on Nginx Test Failure

## Objective

When a newly written release fails `nginx -t`, automatically restore `current` to the previous last-good release so traffic stays on known-good config. Quarantine the failing release under a `failed/` tree for investigation. Operators can opt out of auto-rollback via global property or YAML.

**User:** app operators deploying nginx-custom configs who need failed deploys not to strand nginx (shared across apps) on broken config.

**Success:** a bad config never remains as `current` when auto-rollback is enabled; the bad tree is preserved under `failed/` within a retain limit (when auto-rollback is on).

## Tech Stack

- Go builder: `src/cmd/nginx-config-builder`
- Bash wiring: `functions`, `internal-functions` (property pass-through)
- Config schema: `src/pkg/file_config`
- Existing release layout under `<app-data>/<proxy>-config/conf.d/`

## Commands

```bash
# Unit tests
go test ./src/cmd/nginx-config-builder/ ./src/pkg/file_config/ -count=1

# Opt out globally
dokku <proxy>:set --global auto-rollback false

# Or in app YAML (overrides global)
# auto_rollback: false

# Failed retain (global)
dokku <proxy>:set --global failed-config-retain-count 10

# Or in YAML
# failed_config_retain_count: 5
```

## Project Structure

```
conf.d/
  current -> release-YYYYMMDD.N          # last known good only (never left pointing at a failed release when auto-rollback is on)
  release-YYYYMMDD.N/                    # successful releases
  failed/
    release-YYYYMMDD.N/                  # quarantined failed releases (moved intact)
src/cmd/nginx-config-builder/            # allocate, flip, test, rollback, quarantine, prune
src/pkg/file_config/                     # auto_rollback, failed_config_retain_count fields
functions / internal-functions           # pass global property values into builder flags
docs/spec-auto-rollback.md               # this spec
```

## Behavior

### Deploy flow (auto-rollback **enabled**, default)

1. Resolve previous good dir = resolve `current` symlink (may be empty on first deploy).
2. `allocateNextReleaseDirectory` → write new release tree (immutable; never overwrite).
3. Point `current` at the new release.
4. Run nginx test.
5. **On success:** prune old non-current releases (`old-config-retain-count`); prune `failed/` to `failed-config-retain-count`.
6. **On failure:**
   - If previous good exists: point `current` back at previous good.
   - If no previous good (first deploy): **remove** the `current` symlink entirely — do **not** leave it pointing at the failed release (shared nginx would break other apps on next unrelated deploy).
   - Move the failed new release dir into `conf.d/failed/<basename>` (create `failed/` as needed).
   - Prune `failed/` to retain count (newest kept).
   - Exit non-zero with useful logs: nginx test output, whether rollback or “removed current (no previous good)”, and quarantine path.

### Deploy flow (auto-rollback **disabled**)

1–4 same as above.
5. **On success:** same prunes as enabled path.
6. **On failure:** do **not** restore/remove `current`; do **not** move to `failed/`; exit non-zero with nginx test output. Operator inspects the broken release left as `current`.

### `-without-nginx-test`

- No test ⇒ no rollback path. Treat as success for prune purposes (same as today).

## Config

| Source | Key | Default | Notes |
|--------|-----|---------|--------|
| YAML | `auto_rollback` | `true` (when unset) | overrides global when set |
| Global property | `auto-rollback` | unset → true | values: `true` / `false` |
| YAML | `failed_config_retain_count` | `10` when unset | overrides global; `>= 0` |
| Global property | `failed-config-retain-count` | unset → 10 | `>= 0` |

Precedence matches `old-config-retain-count`: **YAML > global property > default**.

`old-config-retain-count` applies only to non-current **successful** release dirs under `conf.d/` (not under `failed/`).

## Code Style

```go
if err := testNginxConfig(...); err != nil {
    if autoRollback {
        if prev != "" {
            log.Printf("[nginx-config-builder] auto-rollback: restoring current -> %s", prev)
            if rbErr := updateCurrentSymlink(nginxConfigDirectory, prev); rbErr != nil {
                log.Printf("[nginx-config-builder] auto-rollback restore failed: %v", rbErr)
            }
        } else {
            log.Printf("[nginx-config-builder] auto-rollback: no previous good release; removing current symlink")
            if rmErr := removeCurrentSymlink(nginxConfigDirectory); rmErr != nil {
                log.Printf("[nginx-config-builder] failed to remove current symlink: %v", rmErr)
            }
        }
        dest, qErr := quarantineFailedRelease(nginxConfigDirectory, newReleaseDir)
        if qErr != nil {
            log.Printf("[nginx-config-builder] quarantine failed: %v", qErr)
        } else {
            log.Printf("[nginx-config-builder] quarantined failed release at %s", dest)
        }
        if pErr := pruneFailedReleases(nginxConfigDirectory, failedRetainCount); pErr != nil {
            log.Printf("[nginx-config-builder] failed/ prune error: %v", pErr)
        }
    } else {
        log.Printf("[nginx-config-builder] auto-rollback disabled; leaving current -> %s", newReleaseDir)
    }
    log.Fatalf("nginx config test failed: %v", err)
}
```

- Keep release naming `release-YYYYMMDD.N` when moving into `failed/` (same basename).
- Collision if `failed/<basename>` exists: fail quarantine with a clear error (should be rare).

## Testing Strategy

- Framework: Go `testing` in `src/cmd/nginx-config-builder/main_test.go`
- Cover:
  - quarantine moves dir under `failed/`
  - rollback restores previous `current` symlink
  - first-deploy failure: `current` removed; release under `failed/`
  - prune keeps N newest under `failed/`
  - resolve helpers: YAML overrides property; defaults true / 10
  - auto-rollback disabled: current stays on failed release; no `failed/` move
- No live nginx required for unit tests (failing `-nginx-test-command` stub)

## Boundaries

- **Always:** never leave `current` pointing at a failed release when auto-rollback is on; never delete the previous good release as part of rollback; quarantine before prune; exit non-zero on test failure even after successful rollback; log rollback/quarantine outcome clearly; validate retain counts `>= 0`
- **Ask first:** adding a manual `rollback` / `restore-from-failed` CLI; changing nginx include paths
- **Never:** silently treat a failed test as success; prune `failed/` with `old-config-retain-count`; invent a previous good when none exists; quarantine when auto-rollback is disabled

## Success Criteria

- [ ] With auto-rollback on and a previous good release: failed `nginx -t` leaves `current` → previous good
- [ ] With auto-rollback on and no previous good: `current` symlink is removed; failed tree is under `conf.d/failed/`
- [ ] Failed new tree appears under `conf.d/failed/<release-...>` when auto-rollback is on
- [ ] `failed/` retains at most N entries (default 10), configurable via YAML/global
- [ ] With `auto_rollback: false` / global `auto-rollback false`: failed test does not restore/remove `current` and does not quarantine
- [ ] Failure logs include nginx test output plus rollback/quarantine (or opt-out) outcome
- [ ] Successful deploys unchanged aside from also pruning `failed/` to retain limit
- [ ] Unit tests cover the cases above
- [ ] `go test ./src/cmd/nginx-config-builder/ ./src/pkg/file_config/` passes

## Out of Scope

- Manual `dokku …:rollback` CLI
- Rolling back app containers / code
- Restoring a release from `failed/` back to `current` via CLI
- Diff/annotate tools for why nginx -t failed beyond captured test output in logs
- In-builder nginx reload (caller handles reload; non-zero exit is enough)

## Decisions (resolved)

1. **First deploy failure:** quarantine to `failed/`; remove `current` symlink (do not leave it pointing at the failed release).
2. **Opt-out + failure:** leave broken release as `current`; skip `failed/` entirely.
3. **Property names:** `auto-rollback` / `auto_rollback`, `failed-config-retain-count` / `failed_config_retain_count`.
4. **Post-rollback:** non-zero exit is enough; no in-builder reload; logs must be useful.
