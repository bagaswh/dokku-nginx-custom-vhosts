# Spec: Limit Req / Conn Zones

## Objective

Add first-class `limit_req_zones` and `limit_conn_zones` config, mirroring proxy/fastcgi caches: define zones once, emit http-level directives, reference namespaced zone names in location bodies.

## Schema

```yaml
limit_req_zones:
  - name: api          # required
    key: $binary_remote_addr  # required, no default
    size: 10m          # required, no default
    rate: 10r/s        # required, no default

limit_conn_zones:
  - name: addr         # required
    key: $binary_remote_addr  # required, no default
    size: 10m          # required, no default
```

## Generated nginx

```nginx
limit_req_zone $binary_remote_addr zone=limit_req_<app>_<name>:<size> rate=<rate>;
limit_conn_zone $binary_remote_addr zone=limit_conn_<app>_<name>:<size>;
```

## Location usage (name-only)

```nginx
limit_req zone={{ .limit_req_zones.api }} burst=20 nodelay;
limit_conn {{ .limit_conn_zones.addr }} 10;
```

## Output files

- `limit_req_zones.conf`
- `limit_conn_zones.conf`

(same release-dir pattern as `proxy_caches.conf`)

## Success criteria

- [ ] YAML with required fields parses; missing required fields fail validation
- [ ] Builder emits correct directives with namespaced zone names
- [ ] Location templates resolve `{{ .limit_req_zones.<name> }}` and `{{ .limit_conn_zones.<name> }}`
- [ ] Existing cache/upstream behavior unchanged
- [ ] Unit tests pass

## Out of scope

- Structured `limit_req` / `limit_conn` helpers on locations
- Silent defaults for key/size/rate
- Auto-migration of existing `in_http_block` zone lines

## Boundaries

- Always: validate required fields; namespace zone names with app
- Ask first: changing include wiring for http-block templates
- Never: invent silent defaults for key/size/rate
