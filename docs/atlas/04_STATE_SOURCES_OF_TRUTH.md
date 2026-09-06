# State — Sources of Truth

## 1. Dokku plugin property store (per-app + global)

- **Location**: dokku's own store, keyed by plugin name `nginx-custom`
  (`src/pkg/dokku_property/dokku_property.go` →
  `common.PropertyGet(getProxyName(), appName, property)`).
- **Stores**: properties such as `root-domain`, `app-path`, `default-app`,
  `add-header-mode`, `config-file`, `config-file-mode`,
  `config-file-owner-uid/gid`, `config-files-root-dir`, cache root paths,
  log dir paths, `old-config-retain-count`, `auto-rollback`,
  `failed-config-retain-count`, `nginx-restart-command`,
  `nginx-restart-command-run-with-sudo`, `nginx-test-command`, `data-dir`.
- **Written by**: `subcommands/set` → `fn-plugin-property-write` /
  `fn-plugin-property-delete` (bash, dokku common).
- **Read by**: `internal-functions` (`fn-get-property` → `nginx-property`
  binary), `get_app_proxy_type` (read `proxy-type` trigger instead).
- **Consistency**: strong per dokku's property plugin; computed resolution is
  in memory in `nginx-property` (`GetComputedProperty`: app → global → default).

## 2. YAML nginx config (in app image, copied to dokku data)

- **Location**: `${DOKKU_LIB_ROOT}/data/nginx-custom/app-<APP>/nginx-custom-config-yaml/<config-file>`
  (`functions:nginx_get_yaml_config_absolute_path`; `config-file` property
  selects the filename).
- **Stores**: vhosts, upstreams, overrides, maps, caches, limit zones, user
  vars, retain/rollback settings.
- **Written by**: copied out of the app image at `post-extract`
  (`fn-nginx-custom-copy-from-image`); authored in the app repo.
- **Read by**: `nginx-config-builder` (via `-config-file-path`),
  `file-config` (JMESPath queries), `cache-purger`.
- **Consistency**: must exist at build-config time; missing file → deploy fail.

## 3. Rendered config release tree + symlink

- **Location**: `${DOKKU_LIB_ROOT}/data/nginx-custom/app-<APP>/nginx-custom-config/conf.d/`
  (`nginxWorkingDirectory` in builder `main()`).
- **Stores**: `release-YYYYMMDD.N/` dirs (upstreams.conf, proxy_caches.conf,
  fastcgi_caches.conf, limit_req_zones.conf, limit_conn_zones.conf, maps.conf,
  `vhosts/<server>/vhost.conf`), `current` symlink, `failed/` quarantine dir.
- **Written by**: `nginx-config-builder` (WriteFragment → copyConfigToRelease →
  updateCurrentSymlink; quarantine on failure).
- **Read by**: nginx via `current` include; `getPreviousVersionDirectory`
  resolves previous good.
- **Consistency**: `current` MUST always point at a passing release when
  auto-rollback is on (see `00 spec-auto-rollback` and `06_GOTCHAS`).

## 4. Shell/environment state (ephemeral)

- **Location**: process env during a hook.
- **Stores**: `PROXY_NAME`; exported `DOKKU_APP_LISTENERS`,
  `PROXY_UPSTREAM_PORTS`, `PROXY_CACHE_*`, `FASTCGI_CACHE_*`,
  `NGINX_ADD_HEADER_MODE`, `NGINX_ACCESS_LOG_ROOT_DIR`,
  `NGINX_ERROR_LOG_ROOT_DIR`, container labels/mounts JSON.
- **Written by**: `functions:nginx_build_config`.
- **Read by**: `nginx-config-builder` via `mustEnvs` (hard fail on missing).

## 5. Runtime system state (owned by host, not by this plugin)

- **Location**: `/etc/nginx/` (or openresty), `/var/log/nginx|openresty`,
  init system (`systemctl`/`sv`), docker engine, `/etc/sudoers.d/`.
- **Stores**: include dir `conf.d`, access/error logs, running nginx config.
- **Written/read by**: `restart_nginx`, `nginx -t` (`get_nginx_test_command`),
  `install` (sudoers + log ownership).

## Reconciliation Rules

1. **YAML overrides global property**: for `auto_rollback`,
   `old_config_retain_count`, `failed_config_retain_count`, and
   `upstream_address_mode`, the YAML field wins when set
   (see `resolveAutoRollback` / `resolveOldConfigRetainCount` /
   `resolveFailedConfigRetainCount`, and
   `nginx-config-get-upstream-address-mode`); otherwise the global property
   wins, otherwise the hardcoded default (`true`, `10`, `10`, `ip`).
2. **Computed property = app value > global > default**
   (`dokku_property.GetComputedProperty`).
3. **File on disk is source of truth for nginx**: nginx serves whatever
   `current` points at; no reconciliation with YAML after renders.
4. **Container state wins over glob for listeners**: `network-get-listeners`
   (ip mode) or container DNS names (dns mode) are read at build time; stale
   containers show up as stale listeners until rebuild.