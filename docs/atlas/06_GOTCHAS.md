# Gotchas

## Environment Contract (`mustEnvs`)

### `nginx-config-builder` hard-fails on missing env vars
- **File**: `src/cmd/nginx-config-builder/main.go` (`mustEnvs`, `mustEnv`)
- **Risk**: running the binary outside of `functions:nginx_build_config`
  context fails with `missing required env var: <name>`.
- **Rule**: always run via `nginx_build_config` (or set every exported var in
  tests). `DOKKU_APP_CONTAINER_DETAILS` is *not* required (commented out) —
  but the other ~18 are.
- **Why**: the builder builds the whole config from a single env snapshot and
  treats an empty value as a bug.

## Release Lifecycle & Symlink Safety

### Never leave `current` pointing at a failed release (auto-rollback on)
- **File**: `main.go` `handleNginxTestFailure`; see `docs/spec-auto-rollback.md`
- **Risk**: nginx is **shared across apps**; a broken `current` include breaks
  other apps on the next unrelated reload.
- **Rule**: on failure with auto-rollback: restore previous good, or remove
  `current` if there is none; quarantine under `conf.d/failed/`.
- **Why**: `nginx -t` only tests include resolution at the time it runs.

### `release-YYYYMMDD.N` dirs are immutable, never overwritten
- **File**: `allocateNextReleaseDirectory`, `copyConfigToRelease`
- **Risk**: reusing a path or writing into a release mid-read corrupts state.
- **Rule**: always allocate the next sequential name first; write new tree; then
  flip `current`; only then test and prune.

### Separate retain counters — don't mix them
- **File**: `pruneOldReleases` vs `pruneFailedReleases`; `resolveOldConfigRetainCount`
  vs `resolveFailedConfigRetainCount`
- **Risk**: pruning `failed/` with `old-config-retain-count` loses quarantine
  evidence; pruning good releases with the failed count keeps garbage.
- **Rule**: `old-config-retain-count` applies to non-current good releases only;
  `failed-config-retain-count` applies to `failed/` only.

### `resolved: YAML > global > default`
- **File**: `resolveAutoRollback`, `resolveOldConfigRetainCount`,
  `resolveFailedConfigRetainCount`, `nginx-config-get-upstream-address-mode`
- **Risk**: changing a global property silently does nothing when the YAML has
  the field set (and vice versa). Verify which source won before debugging.

## Bash Glue

### `restart_nginx` uses `eval` on a user property
- **File**: `functions` (`restart_nginx`, `nginx_restart_command`)
- **Risk**: `nginx-restart-command` global property is `eval`'d — arbitrary
  command execution by anyone who can set global properties.
- **Rule**: treat the property as trusted operator input; don't add new
  eval'd properties without review.

### `nginx -t` command comes from property or platform
- **File**: `get_nginx_test_command`
- **Risk**: value passed to the builder as a single string and split on spaces
  (`strings.Split(nginxTestCommand, " ")`). Quoted args break.
- **Rule**: keep the command simple word-only (e.g. `sudo nginx -t`).

### Container helpers have side effects / debug traces
- **File**: `functions` (`get_container_dns_names` uses `set -x`;
  `container_get_mounts` etc. shell out to `docker inspect`)
- **Risk**: `set -x` traces secrets/credentials if labels contain them; docker
  calls require the dokku user to have docker socket access.
- **Rule**: don't rely on these in hot paths; expect noisy traces under
  `DOKKU_TRACE`.

## Config & Validation

### YAML validation is strict; no silent defaults
- **File**: `src/pkg/file_config/file_config.go` (`validateConfig`);
  `docs/spec-limit-zones.md`
- **Risk**: missing `name`/`key`/`size` on a zone, or a location with both
  `uri` and `named`, fails validation with `In vhosts #0: ...` messages.
- **Rule**: keep required fields; don't "fix" validation by adding defaults
  unless the spec says so.

### `file-config` queries are JMESPath with backticks
- **File**: `functions` (`nginx_purge_cache`), `src/cmd/file-config/main.go`
- **Risk**: `join(',', proxy_caches[?purge_on_deploy == \`true\`].name)` —
  bash quoting of backticks/`?` is easy to get wrong.
- **Rule**: prefer `file-config` for read-only inspection; mind shell quoting.

## Deploy Flow Ordering (init-time)

- `pre-build` fails the deploy when cache/log properties are unset. Set them
  **before** first deploy (see `install` + README). This is intentional
  (`dokku_log_fail`), not a bug.
- `post-extract` must find the YAML in the image — a config-only change with no
  image rebuild will fail deploy. The `config-file` property selects which file.
- `post-delete` removes the whole `<data-dir>/app-<app>/` tree then runs
  `nginx -t` + reload — deleting an app that was the default app will break the
  domain's nginx include until another default app is configured.