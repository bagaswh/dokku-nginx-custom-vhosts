# Dokku NGINX Custom Config Plugin (`nginx-custom`)

A Dokku proxy plugin that replaces Dokku's built-in `nginx-vhosts` proxy with a
YAML-driven NGINX config renderer.

Instead of Dokku generating a vhost from `DOKKU_APP_HOST`/ports, each app ships
its **own YAML config file inside its image**, declaring vhosts, upstreams,
maps, caches, and rate/connection limit zones. This plugin copies that file
out at build time, renders it into NGINX config with a Go builder (using
[sigil](https://github.com/gliderlabs/sigil) + [Sprig](https://masterminds.github.io/sprig/)
templating for dynamic values), and writes it to an **immutable, versioned
release directory** with a `current` symlink. If `nginx -t` fails on the new
release, the plugin automatically rolls back to the last good release (or
removes `current` on a first deploy) and quarantines the broken one — see
[`docs/spec-auto-rollback.md`](docs/spec-auto-rollback.md).

## Features

- **YAML-as-config**: vhosts, locations, upstreams, maps, proxy/FastCGI
  caches, and `limit_req`/`limit_conn` zones are all declared in one YAML file
  per app — no hand-edited NGINX config.
- **Templated values**: any string field in the YAML (location bodies,
  upstream flags, map lines, ...) is rendered with sigil/Sprig, with access to
  container listeners, labels, mounts, and user-defined variables.
- **Upstreams from Dokku**: upstream servers are generated from the app's
  running containers (`ip` or `dns` address mode), with optional
  `upstream_overrides` to tweak directives/flags on the generated upstream.
- **Immutable releases + auto-rollback**: every `proxy:build-config` allocates
  a new `release-YYYYMMDD.N` directory, flips `current`, and runs `nginx -t`;
  a failing release is rolled back and quarantined automatically (configurable
  per app, per YAML, or globally).
- **Cache management**: `proxy_caches`/`fastcgi_caches` with optional
  `purge_on_deploy`, purged via a dedicated Go binary on every deploy.
- **Configurable via Dokku properties**: cache paths, log directories, retain
  counts, rollback behavior, and the restart/test commands can all be set
  per-app or `--global` with `dokku nginx-custom:set`.

## Installation

```shell
sudo dokku plugin:install https://github.com/szuryuu/nginx-custom-vhost.git nginx-custom-vhost
```

---

## Workflow

#### 1. Author a YAML config file in your app's repo

For example, `nginx-config.yaml` at the root of your app repo. See
[`src/pkg/file_config/testdata/example.yaml`](src/pkg/file_config/testdata/example.yaml)
for a fully annotated example covering every field, and
[Config file schema](#config-file-schema) below for a summary.

#### 2. Set the proxy type

```shell
dokku proxy:set my-app nginx-custom
```

#### 3. Point the plugin at your config file

`config-file` is the path to the YAML file **inside the app's repo/image**
(relative paths are resolved by `git`/the image's `copy_from_image`).

```shell
dokku nginx-custom:set my-app config-file nginx-config.yaml
```

#### 4. Set required global properties (once, before the first deploy)

`pre-build` fails the deploy if any of these are unset:

```shell
dokku nginx-custom:set --global proxy-cache-on-disk-root-path /var/lib/nginx/cache/proxy-on-disk
dokku nginx-custom:set --global proxy-cache-in-mem-root-path /var/lib/nginx/cache/proxy-in-mem
dokku nginx-custom:set --global fastcgi-cache-on-disk-root-path /var/lib/nginx/cache/fastcgi-on-disk
dokku nginx-custom:set --global fastcgi-cache-in-mem-root-path /var/lib/nginx/cache/fastcgi-in-mem
dokku nginx-custom:set --global proxy-cache-default-key-zone-size 64m
dokku nginx-custom:set --global fastcgi-cache-default-key-zone-size 64m
dokku nginx-custom:set --global nginx-access-log-root-dir /var/log/nginx
dokku nginx-custom:set --global nginx-error-log-root-dir /var/log/nginx
```

#### 5. Deploy

```shell
git push dokku main
```

This triggers, in order: `post-extract` (copies the YAML file out of the built
image), `pre-build` (checks the properties above are set), and on a
successful deploy, `post-deploy` → `proxy:build-config` (renders the config,
runs `nginx -t`, flips `current`) then purges any caches marked
`purge_on_deploy: true`.

You can also trigger a re-render manually without a new deploy:

```shell
dokku proxy:build-config my-app
```

---

## Config file schema

The YAML file is validated strictly — required fields fail the build with a
readable error (e.g. `In vhosts #0: field 'ServerName' is required`). Top-level
keys:

| Key | Purpose |
|---|---|
| `vhosts` | List of server blocks: `server_name`, `additional_server_names`, `locations`, `variables`, `in_server_block`. |
| `upstreams` | Named upstream server lists, or a selector (`select_process_type`) into upstreams generated from the app's Dokku listeners. |
| `upstream_overrides` | Attach extra directives/flags/zone to a generated (listener-based) upstream, selected by `select_process_type` + `select_port`. |
| `maps` | `map` blocks (`variable`, `string`, `lines`), namespaced per app. |
| `proxy_caches` / `fastcgi_caches` | Cache zones (`in_mem` or `on_disk`), optionally `purge_on_deploy`. |
| `limit_req_zones` / `limit_conn_zones` | `limit_req_zone`/`limit_conn_zone` definitions — all fields required, no silent defaults (see [`docs/spec-limit-zones.md`](docs/spec-limit-zones.md)). |
| `user_vars` | Arbitrary variables available to templated strings as `.vars.*`. |
| `upstream_address_mode` | `ip` (default) or `dns` — how upstream servers are resolved from containers. |
| `auto_rollback` | Override the `auto-rollback` property for this app (default `true`). |
| `old_config_retain_count` / `failed_config_retain_count` | Override the retain-count properties for this app (default `10` each). |
| `in_http_block` | Raw snippet injected into the `http` context for this app. |

Precedence for the overridable fields above is **YAML value > global property
> built-in default**.

---

## Commands

| Command | Description |
|---|---|
| `dokku nginx-custom:set <app\|--global> <property> [<value>]` | Set a property; omit `<value>` to unset it. |
| `dokku nginx-custom:get <app> <property>` | Read a property (empty output if unset). |
| `dokku proxy:build-config <app>` | Render this app's NGINX config from its YAML and reload NGINX. |
| `dokku nginx-custom:help` | List available subcommands. |

> **Note:** `nginx-custom:help` also lists `report`, `show-config`,
> `validate-config`, `access-logs`, `error-logs`, `start`, and `stop`. These
> are not implemented yet in this version of the plugin — running them will
> error out rather than do anything useful.

## Properties reference

Set with `dokku nginx-custom:set [--global] <app> <property> <value>`.

| Property | Scope | Default | Notes |
|---|---|---|---|
| `config-file` | app/global | *(none — required)* | Path to the app's YAML config file inside its repo/image. |
| `config-file-mode` | app/global | `0644` | File mode used when writing the copied-out config file. |
| `config-file-owner-uid` / `config-file-owner-gid` | app/global | Dokku system user's uid/gid | Ownership of the copied-out config file. |
| `add-header-mode` | app/global | `add_header` | Directive used by the `nginx_add_header` template helper. |
| `upstream-address-mode` | global | `ip` | Fallback when the YAML doesn't set `upstream_address_mode`. |
| `old-config-retain-count` | global | `10` | Fallback when the YAML doesn't set `old_config_retain_count`. |
| `failed-config-retain-count` | global | `10` | Fallback when the YAML doesn't set `failed_config_retain_count`. |
| `auto-rollback` | global | `true` | Fallback when the YAML doesn't set `auto_rollback`. |
| `proxy-cache-on-disk-root-path` / `proxy-cache-in-mem-root-path` | global | *(required)* | Root paths for `proxy_caches` with `on_disk`/`in_mem`. |
| `fastcgi-cache-on-disk-root-path` / `fastcgi-cache-in-mem-root-path` | global | *(required)* | Root paths for `fastcgi_caches` with `on_disk`/`in_mem`. |
| `proxy-cache-default-key-zone-size` / `fastcgi-cache-default-key-zone-size` | global | *(required)* | Default `keys_zone` size when a cache doesn't set one. |
| `proxy-cache-default-flags` / `fastcgi-cache-default-flags` | global | *(none)* | Default flags applied to caches that don't set their own. |
| `nginx-access-log-root-dir` / `nginx-error-log-root-dir` | global | *(required)* | Root directories for per-app access/error logs. |
| `nginx-default-access-log-format` | global | *(nginx default)* | Access log format name. |
| `nginx-purge-cache-command` | global | *(built-in purge logic)* | Custom command to purge caches instead of the built-in purger. |
| `nginx-restart-command` | global | `systemctl reload nginx` (or `openresty`) | Command used to reload NGINX; run via `eval`, so treat as trusted input. |
| `nginx-restart-command-run-with-sudo` | global | `true` | Whether the restart command above is run with `sudo`. |
| `nginx-test-command` | global | `sudo nginx -t` (or `openresty -t`) | Command used to validate a rendered release before flipping `current`. |
| `data-dir` | app | `${DOKKU_LIB_ROOT}/data` | Overrides where this plugin stores per-app config/release data. |

---

## Further reading

- [`docs/spec-auto-rollback.md`](docs/spec-auto-rollback.md) — release
  lifecycle and automatic rollback/quarantine design.
- [`docs/spec-limit-zones.md`](docs/spec-limit-zones.md) — `limit_req`/`limit_conn`
  zone design.
- [`docs/atlas/`](docs/atlas/) — deeper architecture, domain model, and
  gotchas for anyone modifying the plugin itself.
