# Critical Flows

## Flow 1: `dokku proxy:build-config <app>` (manual config build)

**Trigger**: operator runs `dokku proxy:build-config <default-app>` (must be the
default app for multi-app domain routing).

1. `proxy-build-config:trigger-nginx-custom-proxy-build-config` — guard
   `is_this_the_proxy "$APP"`, then call `nginx_build_config "$APP"`.
2. `functions:nginx_build_config` — `plugn trigger ports-configure "$APP"`;
   collects `DOKKU_PROCESS_TYPES`, `UPSTREAM_ADDRESS_MODE`,
   `DOKKU_APP_LISTENERS` (JSON via `get_app_listeners_json`), one container via
   `app_containers_get`.
3. Exports env: `PROXY_UPSTREAM_PORTS`, `PROXY_CACHE_*`, `FASTCGI_CACHE_*`,
   `NGINX_ADD_HEADER_MODE`, `NGINX_*_LOG_ROOT_DIR`, container labels/mounts.
4. `src/cmd/nginx-config-builder/main.go:main()` — `mustEnvs(...)` (19 vars),
   reads YAML via `file_config.ReadConfig`, builds fragment strings
   (`buildUpstreamConfig`, caches, maps, limit zones, locations).
5. Release lifecycle: `allocateNextReleaseDirectory` → write fragments →
   `updateCurrentSymlink` → `nginx -t` → prune (or rollback; see Flow 4).
6. Back in bash: `restart_nginx` (sudo systemctl reload nginx/openresty, or the
   `nginx-restart-command` property via `eval`).

**End state**: `current` points to newest passing release; nginx reloaded.
**Gotchas**: env contract strict; failed test → `dokku_log_fail`.

## Flow 2: App deploy — `post-extract` pulls YAML from image

**Trigger**: dokku extracts app source/image during a deploy.

1. `post-extract:trigger-nginx-custom-post-extract` — guard
   `is_this_the_proxy "$APP"`.
2. Get `source-image` via `plugn trigger git-get-property`.
3. `fn-nginx-custom-copy-from-image "$APP" "$IMAGE" "<config-file>"`
   — deletes stale `config-file.*` in the YAML dir, then
   `copy_from_image` into `nginx-custom-config-yaml/`.
4. Not found in image → `dokku_log_fail` (deploy stops).

**End state**: `<app-data>/.../nginx-custom-config-yaml/<config-file>` exists.
**Gotchas**: `find ... -name "$conf_dest_path_filename_prefix.*" -delete`
deletes every sibling of the config filename pattern before copy.

## Flow 3: Deploy finalize — `post-deploy`

**Trigger**: dokku finishes a deploy.

1. `post-deploy:trigger-nginx-custom-post-deploy` — guard
   `is_this_the_proxy "$APP"`.
2. `plugn trigger proxy-build-config "$APP"` → Flow 1.
3. `nginx_purge_cache "$APP"`:
   - `functions:nginx_yaml_get_config` → `file-config -config <path> 'proxy_caches'`.
   - Query `join(',', proxy_caches[?purge_on_deploy == \`true\`].name)`.
   - `cache-purger` binary purges each selected cache dir.

**End state**: nginx config rebuilt for new release; flagged caches emptied.
**Gotchas**: `file-config` query syntax is JMESPath; opens with backticks.

## Flow 4: `nginx -t` failure with auto-rollback

**Trigger**: builder's `testNginxConfig` fails (bad generated config).

1. `main()` calls `handleNginxTestFailure(nginxConfigDirectory, newReleaseDir,
   previousGood, autoRollback, failedRetainCount, err)`.
2. If `autoRollback`:
   - `previousGood != ""` → `updateCurrentSymlink(conf.d, previousGood)`.
   - no previous good → `removeCurrentSymlink(conf.d)` (do not leave `current`
     pointing at the failed release — shared nginx would break other apps).
   - `quarantineFailedRelease` moves the release under `conf.d/failed/`.
   - `pruneFailedReleases` keeps newest `failedRetainCount`.
3. If disabled: leave `current` on the failed release; exit non-zero.
4. `log.Fatalf("nginx config test failed: %v", err)` — non-zero exit.

**End state**: `current` = last known good (or removed); failed tree preserved
under `failed/`; deploy fails loudly.
**Gotchas**: never prune `failed/` with `old-config-retain-count`; never
invent a previous good when none exists.

## Flow 5: CLI reads/writes — `nginx-custom:set` / `nginx-custom:get`

**Trigger**: `dokku nginx-custom:set <app> <key> <value>` (or unset by omitting
value; `--global` allowed).

1. `commands` → `subcommands/set:cmd-nginx-custom-set` — optional `verify_app_name`,
   then `fn-plugin-property-write "$PROXY_NAME" "$APP" "$KEY" "$VALUE"` else
   `fn-plugin-property-delete`.
2. Read path: `subcommands/get:cmd-nginx-custom-get` →
   `fn-get-property --app "$APP" "$KEY"` →
   `internal-functions:fn-get-property` → `nginx-property` binary →
   `dokku_property.GetComputedProperty(app, property)`.

**End state**: property stored/removed in dokku's plugin property store; get
prints value (computed: app → global).
**Gotchas**: `nginx-property` requires `--log-root`; missing `PROXY_NAME` env
→ `log.Fatalln`.

## Flow 6: `pre-build` gate

**Trigger**: dokku builds the app image.

1. `pre-build:trigger-nginx-custom-pre-build` — guard `is_this_the_proxy`.
2. Requires non-empty `proxy-cache-on-disk-root-path`,
   `proxy-cache-in-mem-root-path`, `fastcgi-cache-on-disk-root-path`,
   `fastcgi-cache-in-mem-root-path`, `proxy-cache-default-key-zone-size`,
   `fastcgi-cache-default-key-zone-size`, `nginx-access-log-root-dir`,
   `nginx-error-log-root-dir` — any missing → `dokku_log_fail`.
3. Validates `upstream-address-mode` ∈ {ip, dns}.

**End state**: deploy proceeds only if cache/log properties are configured.