# Architecture

## Overview

`dokku-nginx-custom-vhosts` is a Dokku plugin registered as proxy `nginx-custom`
(`config` exports `PROXY_NAME="nginx-custom"`). It serves multiple Dokku apps
under a single root domain, distinguished by URL paths. Each app ships a YAML
config file inside its container image; the plugin copies it out at
`post-extract`, renders NGINX config fragments with the Go builder, writes them
into a versioned release directory, and flips a `current` symlink after
`nginx -t` passes.

The codebase is split into **bash glue** (dokku trigger/hook entrypoints and
property plumbing) and **Go binaries** (rendering, YAML schema, property lookup,
cache purging). The Go side does all the heavy lifting; bash collects dokku
state and passes it via env vars.

## Components

### Bash layer (plugin triggers & CLI)

- `commands` — CLI dispatcher: routes `nginx-custom:*` subcommands to
  `subcommands/*` or functions in `command-functions`/`help-functions`.
- `subcommands/set`, `subcommands/get`, `subcommands/default` — thin wrappers
  over `fn-plugin-property-write`/`fn-plugin-property-delete` (dokku common) and
  `fn-get-property` (this plugin).
- `proxy-build-config` — the `proxy-build-config` trigger that runs
  `nginx_build_config` then `restart_nginx`. Invoked via
  `dokku proxy:build-config <app>`.
- `pre-build`, `post-extract`, `post-deploy`, `post-delete` — lifecycle hooks.
  Each guards on `is_this_the_proxy` and quits early for apps that do not use
  this proxy.
- `functions` — `nginx_build_config`, `nginx_purge_cache`, listener/container
  helpers; calls the Go binaries at the bottom.
- `internal-functions` — `fn-nginx-custom-*` property getters keyed to
  `nginx-property` Go binary, plus `fn-get-property`.
- `install` — writes sudoers entries for nginx/systemctl/reload and builds the
  binaries in docker.

### Go layer

| Binary | Role |
|--------|------|
| `src/cmd/nginx-config-builder/main.go` | Core 1700-line builder: reads YAML (`-config-file-path`), renders config fragments, release lifecycle (`release-YYYYMMDD.N`, `current` symlink), `nginx -t`, auto-rollback/quarantine. |
| `src/cmd/nginx-property/main.go` | Property lookup for bash: `--app`, `--global`, `--computed`; prints value on stdout. |
| `src/cmd/file-config/main.go` | JMESPath-style query tool `-config <path> '<query>'` used by bash (`nginx_yaml_get_config`, cache-purge queries). |
| `src/cmd/cache-purger/main.go` | Purges `proxy_caches`/`fastcgi_caches` marked `purge_on_deploy`; refuses `/` and `""` paths. |
| `src/cmd/pagesize/pagesize.go` | Prints `os.Getpagesize()/1024`. |
| `src/pkg/file_config/file_config.go` | YAML schema (Config, VhostConfig, UpstreamConfig, caches, limit zones) + validation. |
| `src/pkg/dokku_property/{dokku_property,functions}.go` | Dokku property abstraction (`GetAppProperty`/`GetGlobalProperty`/`GetComputedProperty`); openresty detection. |

### Templates

`templates/*.sigil` — NGINX fragment templates compiled with
[gliderlabs/sigil](https://github.com/gliderlabs/sigil). Notably
`nginx.conf.sigil` (the master config with `include conf.d/*;`) and the
`400/404/500/502-error.html` pages.

## Communication Patterns

- **dokku trigger API** — bash calls `plugn trigger <name> <app>` (e.g.
  `proxy-type`, `proxy-is-enabled`, `network-get-listeners`, `ports-get`,
  `ps-current-scale`, `ports-configure`).
- **Env vars** — bash → Go. `nginx_build_config` exports
  `DOKKU_APP_LISTENERS`, `PROXY_UPSTREAM_PORTS`, `PROXY_CACHE_*`,
  `FASTCGI_CACHE_*`, `NGINX_ADD_HEADER_MODE`, log dirs; the builder requires
  them via `mustEnvs` and fails hard if missing.
- **Files** — YAML config in `<dokku-data-root>/app-<app>/nginx-custom-config-yaml/`
  (copied from the image at post-extract); rendered output in
  `<app-data>/nginx-custom-config/conf.d/release-*/`; `current` symlink.
- **docker** — bash inspects containers (`docker inspect ... .Labels/.Mounts/`)
  for listener details and mounts.

## Key Decisions

- **YAML-as-config, rendered to files**: apps declare vhosts/upstreams in YAML
  in their image; no hand-edited nginx conf. Tradeoff: a malformed YAML fails
  deploy at post-extract/nginx -t, so validation is strict
  (`file_config.validateConfig`).
- **Immutable release dirs + `current` symlink**: never overwrite a release;
  allocate `release-YYYYMMDD.N`, flip `current`, test, rollback on failure
  (see `docs/spec-auto-rollback.md`). Tradeoff: disk usage grows until prune.
- **Precedence `YAML > global property > default`**: same property (e.g.
  `auto_rollback`, retain counts) can come from YAML or from
  `dokku nginx-custom:set --global`. See `resolve*` funcs in the builder.
- **Proxy guard everywhere**: every hook checks `is_this_the_proxy "$APP"` so
  apps that use another proxy are untouched.
- **Build in docker**: `Makefile` builds each binary in `golang:1.24.2` to
  match dokku's runtime env (CGO_ENABLED=0, linux/amd64).