# Plan: Limit Zones

## Approach

Mirror `proxy_caches` / `fastcgi_caches`: schema → builder → release conf files → inject resulting names into location template data.

## Order

1. Schema structs + validation in `file_config`
2. `buildLimitReqZoneConfig` / `buildLimitConnZoneConfig` (+ tests)
3. Wire into `main()` (conf output + `locationConfigData`)
4. Update `example.yaml`

## Risks

- Http-block include of new `.conf` files may be incomplete for caches today (`nginx.in_http_block.conf.sigil` is a stub). Follow same output pattern; do not invent include wiring unless already present.
