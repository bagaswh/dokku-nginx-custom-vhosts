# External Dependencies

## Package Dependencies (Go)

Declared in `go.mod` (module `dokku-nginx-custom`, go 1.23.0).

| Package | Purpose | Notes |
|---------|---------|-------|
| `github.com/gliderlabs/sigil` | Render `.sigil` templates (documents.conf.sigil, nginx.conf.sigil) | Templating engine with `{{ }}`; multiple `{{ . }}` interpolation + funcs |
| `github.com/dokku/dokku/plugins/common` | Property get/store, plugin runtime helpers | Upstream dokku source — **must match installed dokku version** |
| `github.com/Masterminds/sprig/v3` | Sprig template functions (v3) | Added at v0.1.0 (`mergeTemplateFuncs`) |
| `github.com/go-playground/validator/v10` | YAML config validation | `validateConfig` in `file_config.go` |
| `github.com/jmespath/go-jmespath` | JSON querying of YAML-raw config (`file-config` binary) | `QueryConfig` used for `proxy_caches` / `fastcgi_caches` queries |
| `gopkg.in/yaml.v3` | YAML parse/unmarshal | Both `ReadConfig` and JMESPath raw parse |
| `github.com/spf13/pflag` | CLI flag parsing (Go binaries) | `nginx-property`, `nginx-config-builder`, `cache-purger` |
| `dario.cat/mergo` | (indirect) merge helpers | — |
| `github.com/alexellis/go-execute/v2` | (indirect) exec helper | — |
| `github.com/Masterminds/goutils`, `semver/v3` | (indirect, sprig deps) | — |

No other runtime packages beyond stdlib (`flag`, `os`, `os/exec`, `path`,
`filepath`, `slices`, `time`, `syscall`, `strconv`, `strings`, `fmt`, `log`,
`json`, `net`).

## External Services / System Components

### Dokku (host daemon + trigger API)
- **Used for**: everything. `plugn trigger` calls: `proxy-type`,
  `proxy-is-enabled`, `network-get-listeners`, `ports-get`, `ports-configure`,
  `ps-current-scale`, `git-get-property`, `copy_from_image`, plus
  `fn-plugin-property-*` from the common/property-functions lib.
- **Integration point**: `functions`, `install`, hook scripts at repo root.
- **If unavailable**: hooks fail fast (`DOKKU_FAIL_EXIT_CODE` trap in
  `proxy-build-config`); property lookups return empty.

### nginx / OpenResty
- **Used for**: serving web traffic; `nginx -t` config validation; reload via
  `systemctl reload nginx` (or `openresty`).
- **Detected by**: `fn-nginx-custom-uses-openresty` (checks
  `/usr/bin/openresty`), `fn-nginx-custom-nginx-location`.
- **If unavailable**: `dokku_log_fail` in `fn-nginx-custom-nginx-location`;
  install hardening (`install` script) creates sudoers entries.

### Docker (container runtime)
- **Used for**: container inspection (labels/mounts/details) in
  `functions` (`container_get_labels`, `container_inspect`,
  `get_container_dns_names`); building Go binaries in a docker image
  (Makefile `build-in-docker`).
- **If unavailable**: socket errors in bash helpers; build pipeline breaks.

### CoreDNS (test environment)
- **Used for**: `tests/coredns` — DNS-based listener tests under Vagrant.
- **If unavailable**: dns-mode integration tests cannot run.

## Failure Behavior Summary

| Failure | Effect | Graceful? |
|---------|--------|-----------|
| Missing env var (`mustEnvs`) | `nginx-config-builder` exits non-zero | No |
| Missing/`-t`-failing nginx | `handleNginxTestFailure` + auto-rollback or prune | Yes (rollback) |
| Unreachable docker | `docker inspect` fails → bash error → hook fails | No |
| Missing YAML in image | `dokku_log_fail` at post-extract | No |
| Missing cache/log root props | `dokku_log_fail` at pre-build | No |
| Bad `nginx-test-command` property | exec fails in `get_nginx_test_command` | No |