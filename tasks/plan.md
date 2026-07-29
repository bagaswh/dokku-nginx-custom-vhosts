# Implementation Plan: Automatic Rollback on Nginx Test Failure

Spec: [`docs/spec-auto-rollback.md`](../docs/spec-auto-rollback.md)

## Overview

When `nginx -t` fails after writing a new release, optionally (default on) restore `current` to the previous good release—or remove `current` if none exists—and move the failed tree under `conf.d/failed/` with its own retain limit. Opt-out leaves the broken release as `current` and skips quarantine. Non-zero exit + useful logs; no in-builder reload.

## Architecture Decisions

- **Test-then-recover, not test-before-flip:** nginx includes via `current`, so we must point `current` at the new release to test it, then recover on failure.
- **Reuse existing helpers:** `getPreviousVersionDirectory`, `updateCurrentSymlink`, `allocateNextReleaseDirectory`, prune patterns already in the builder.
- **Resolve config in Go:** YAML fields on `file_config.Config` override global property flags (same as `old-config-retain-count`).
- **Quarantine = rename/move:** `os.Rename` of `release-*` → `failed/release-*` (same basename); prune `failed/` separately from successful releases.
- **First-deploy failure:** remove `current` symlink entirely so shared nginx is not left on a broken include target.
- **Opt-out:** no restore, no remove-current, no move to `failed/`.

## Dependency Order

```
Schema (auto_rollback, failed_config_retain_count)
    → resolve helpers + flags/bash wiring
        → removeCurrentSymlink / quarantineFailedRelease / pruneFailedReleases
            → wire failure path in main() + useful logs
                → success path also prunes failed/
```

## Task List

### Phase 1: Schema + resolve

- [ ] Task 1: Add YAML fields + resolve helpers
- [ ] Task 2: Builder flags + bash property pass-through

### Checkpoint: Foundation

- [ ] Resolve helpers unit-tested (defaults, YAML override, invalid values)
- [ ] `go test ./src/pkg/file_config/ ./src/cmd/nginx-config-builder/ -run Resolve` passes

### Phase 2: Quarantine + rollback primitives

- [ ] Task 3: `removeCurrentSymlink`, `quarantineFailedRelease`, `pruneFailedReleases`
- [ ] Task 4: Wire auto-rollback into deploy failure/success paths in `main`

### Checkpoint: Core

- [ ] Unit tests for quarantine, restore, first-deploy remove-current, opt-out, failed prune
- [ ] `go test ./src/cmd/nginx-config-builder/ ./src/pkg/file_config/` passes

### Phase 3: Polish

- [ ] Task 5: Useful failure logs + light example/doc touch if needed

### Checkpoint: Complete

- [ ] All success criteria in the spec checked off
- [ ] Ready for code review

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| `os.Rename` across filesystems fails | Med | Same `conf.d` tree today; if rename fails, log loudly and still attempt restore |
| Race: another process reads `current` mid-flip | Low | Same as today’s deploy path; accept brief window |
| Opt-out leaves shared nginx broken | Med | Document clearly; default remains auto-rollback on |
| `failed/<basename>` collision | Low | Error with clear message; basenames are date.seq unique |

## Open Questions

None — resolved in spec decisions.
