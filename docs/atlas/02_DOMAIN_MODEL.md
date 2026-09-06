# Domain Model

## Core Entities

### Config (YAML file in the app image)

- **Defined in**: `src/pkg/file_config/file_config.go` (`Config`)
- **Loaded from**: `<dokku-data>/app-<APP>/nginx-custom-config-yaml/<config-file>`,
  copied from the image at `post-extract`
- **Key fields**: `vhosts[]`, `upstreams[]`, `upstream_overrides[]`, `maps[]`,
  `proxy_caches[]`, `fastcgi_caches[]`, `limit_req_zones[]`,
  `limit_conn_zones[]`, `in_http_block`, `in_server_block`, `user_vars`,
  `upstream_address_mode`, `auto_rollback`, `old_config_retain_count`,
  `failed_config_retain_count`
- **Validation**: `validateConfig` uses `go-playground/validator`; required
  fields fail loudly with readable messages (see `In vhosts #0: ...` format).

### VhostConfig

- **Defined in**: `file_config.go` (`VhostConfig`)
- **Key fields**: `server_name` (required), `additional_server_names[]`,
  `locations[]` (required), `variables[]`, `in_server_block`

### LocationConfig

- **Defined in**: `file_config.go`
- **Shape**: `{modifier, uri | named, body}` — `uri` and `named` are mutually
  exclusive; `named` locations become `@name` aliases.

### Upstream / UpstreamOverride / UpstreamServer

- **Defined in**: `file_config.go`; rendered by `buildUpstreamConfig`
- `UpstreamConfig` is a selector (`select_process_type`) or a named server list
  (`name` + `servers[]`) — `Name`/`Servers` conditional-required.
- `UpstreamServerOverride`/`UpstreamOverride` attach extra `directives`,
  `server_overrides`, and optional `zone` to a selected upstream by
  `select_process_type` + `select_port`.

### CacheConfig (proxy_caches / fastcgi_caches)

- **Defined in**: `file_config.go`
- **Key fields**: `name`, `proxy_cache_path`, `key_zone_size`, `flags`,
  `in_mem` / `on_disk` (mutually exclusive), `purge_on_deploy`.
- Named result: `<prefix>_<app>_<name>` rendered by
  `buildProxyCacheConfig`/`buildFastcgiCacheConfig`.

### LimitReqZoneConfig / LimitConnZoneConfig

- **Defined in**: `file_config.go`; rendered by `buildLimitReqZoneConfig` /
  `buildLimitConnZoneConfig`
- All fields required (`name`, `key`, `size`; plus `rate` for req zones) — no
  silent defaults (see `docs/spec-limit-zones.md`).

### Property (dokku plugin property)

- **Defined in**: `src/pkg/dokku_property/dokku_property.go`
- Per-app or `--global`; computed = app value, else global, else default.
- Written via `subcommands/set` → `fn-plugin-property-write`.

## Vocabulary

| Term | Meaning | Where Used |
|------|---------|-----------|
| `app` | Dokku app name; also the app-name flag/env used to namespace upstreams/zones | `functions`, builder flags |
| `PROXY_NAME` | `nginx-custom`; name of this proxy plugin | `config`, `commands` |
| `is_this_the_proxy` | Guard: is this app's proxy enabled and `nginx-custom`? | `functions`, all hooks |
| `release-YYYYMMDD.N` | Immutable versioned config directory | `nginx-config-builder` |
| `current` | Symlink to the active release dir | `nginx-config-builder` |
| `conf.d/failed/` | Quarantined failed releases (auto-rollback on) | `nginx-config-builder` |
| `old_config_retain_count` | Keep N non-current good releases | YAML + `resolveOldConfigRetainCount` |
| `failed_config_retain_count` | Keep N quarantined failures | YAML + `resolveFailedConfigRetainCount` |
| `auto_rollback` | Restore previous good on `nginx -t` failure (default on) | YAML + `resolveAutoRollback` |
| `upstream_address_mode` | `ip` (default) or `dns` for listener lookup | YAML + `nginx-config-get-upstream-address-mode` |
| `purge_on_deploy` | Whether a cache is purged on deploy | `CacheConfig` + `cache-purger` |
| `sys_vars` / `user_vars` | Template variable namespaces | `main()` of builder |

## State Machines

### Release lifecycle (per app, per `proxy:build-config`)

```
allocate release-YYYYMMDD.N
        │
        ▼
write config fragments into release dir     (src/cmd/nginx-config-builder)
        │
        ▼
updateCurrentSymlink (current → new release)
        │
        ▼
nginx -t
   │
   ├── pass ──► pruneOldReleases(retain) + pruneFailedReleases(retain) → done
   │
   └── fail ──► handleNginxTestFailure
                   ├─ autoRollback ON:
                   │    prev good? restore current → prev
                   │    no prev?   remove current symlink entirely
                   │    quarantine release under conf.d/failed/
                   │    prune failed/ to retain
                   └─ autoRollback OFF:
                        leave current on failed release; exit non-zero
```

See `docs/spec-auto-rollback.md` and `tasks/plan.md` for the design rationale.

### Proxy enablement

```
app deploy/hook ──► is_this_the_proxy(app)?
   ├─ yes ──► proceed with build-config / purge / etc.
   └─ no  ──► log "Quitting" and return
```