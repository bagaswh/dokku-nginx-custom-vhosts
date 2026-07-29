# Todo: Automatic Rollback

Spec: `docs/spec-auto-rollback.md` · Plan: `tasks/plan.md`

- [x] Task 1: Schema for `auto_rollback` (*bool) and `failed_config_retain_count` (*int) in `file_config.Config`
- [x] Task 2: `resolveAutoRollback` + `resolveFailedConfigRetainCount`; builder flags; bash pass-through
- [x] Task 3: Primitives — `removeCurrentSymlink`, `quarantineFailedRelease`, `pruneFailedReleases`
- [x] Task 4: Wire failure/success paths in `main`
- [x] Task 5: Useful logs on failure (test output, rollback/remove/quarantine/opt-out outcome)

## Checkpoint: Complete

- [x] Spec success criteria covered by unit tests
- [x] `go test ./src/cmd/nginx-config-builder/ ./src/pkg/file_config/ -run '…'` passes
