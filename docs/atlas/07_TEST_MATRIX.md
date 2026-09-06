# Test Matrix

## Running Tests

Unit/package tests (Go):

```bash
# All packages
go test ./src/...

# Builder + config schema (most logic lives here)
go test ./src/cmd/nginx-config-builder/ ./src/pkg/file_config/ -count=1

# Single package
go test ./src/cmd/nginx-config-builder/ -run Resolve -count=1
```

The `docs/spec-auto-rollback.md` spec records the same commands as the
canonical check for that feature.

Integration tests are **Vagrant-based** (not run in CI here):

```bash
# From tests/ — boots an Ubuntu-24.04 box, syncs the repo to /dokku-nginx-path
cd tests && vagrant up
```

## Test Structure

| Directory / File | What It Tests | Framework |
|------------------|--------------|-----------|
| `src/cmd/nginx-config-builder/main_test.go` | Builder core: upstreams, release lifecycle, rollback/quarantine helpers (per spec) | Go `testing` |
| `src/cmd/nginx-config-builder/limit_zones_test.go` | `limit_req` / `limit_conn` zone rendering (spec-limit-zones) | Go `testing` |
| `src/cmd/nginx-config-builder/upstream_overrides_test.go` | Upstream override + server override behavior | Go `testing` |
| `src/cmd/nginx-config-builder/template_funcs_test.go` | Sigil/Sprig template funcs (`nginx_add_header`, `nginx_log`, `tpl`, etc.) | Go `testing` |
| `src/pkg/file_config/file_config_test.go` (+ `testdata/`) | YAML schema parse + validation (required/excluded/conditional fields) | Go `testing` |
| `tests/` (Vagrantfile, coredns/) | Real dokku + nginx integration on Ubuntu 24.04; coredns for dns-mode tests | Vagrant |

## What's Covered

- YAML schema validation edge cases (required fields, `excluded_with`,
  null-vs-absent nullable zones).
- Release allocation, symlink flip, prune, and auto-rollback/quarantine paths.
- Limit-req/conn zone output and namespaced names.
- Upstream overrides and template functions.
- End-to-end deploys on a real VM (via Vagrant).

## Coverage Gaps

- The **bash** layer (`functions`, `internal-functions`, hooks, `install`) has
  no automated tests — regressions there are only caught on the Vagrant VM.
- **cache-purger** and **file-config** binaries have no unit tests.
- **dns-mode** upstream resolution only exercised via coredns on the VM.
- Multi-app/path-domain (default-app) matrix is only validated manually / on VM,
  not as a Go unit test.
- OpenResty path (`fn-nginx-custom-uses-openresty`) is environment-dependent;
  not unit-tested.

## Adding Tests

Follow the repo's `AGENTS.md` `testing` rule (integration-first):

1. Prefer one test calling a **public entry point** over per-helper unit tests.
2. For the builder, call exported `build*` helpers / `resolve*` funcs
   directly, or drive via a temp `nginxConfigDirectory` + stub `-nginx-test-command`.
3. For the schema, feed YAML bytes to `file_config.ReadConfigBytes` and assert
   parsed values/validation errors — no fixture files unless a case truly needs one.
4. Avoid change-detector tests (assert behavior, not call order).
5. New test *files* are opt-in per `AGENTS.md` — confirm with the owner before
   adding a new `_test.go`.

## CI

- The `Makefile` generates binaries via docker (`build-in-docker`); no Go-test
  target is wired into CI in this repo.
- `make atlas-check` is the atlas staleness gate (exit 0 when generated files
  are current).