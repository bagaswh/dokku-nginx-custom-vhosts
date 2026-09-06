# Atlas — dokku-nginx-custom-vhosts

## What Is This?

The `docs/atlas/` folder is a persistent context system — structured documentation
that helps engineers and LLM coding agents understand this codebase quickly.

This repo is a **Dokku plugin** (`nginx-custom`) that replaces Dokku's built-in
nginx proxy. It serves multiple apps under one domain with path-based routing,
generates NGINX config from a YAML description (supplied in the app image), and
manages versioned config releases with automatic rollback on `nginx -t` failure.

## Files

| File | Purpose | Auto-generated? |
|------|---------|----------------|
| `00_README.md` | This file — how to use the atlas | No |
| `01_ARCHITECTURE.md` | System overview, bash↔Go split, module boundaries | No |
| `02_DOMAIN_MODEL.md` | YAML schema entities, properties, release lifecycle | No |
| `03_CRITICAL_FLOWS.md` | Happy-path call chains: build-config, deploy, rollback | No |
| `04_STATE_SOURCES_OF_TRUTH.md` | Where config/props/releases live + precedence rules | No |
| `05_EXTERNAL_DEPENDENCIES.md` | Go modules + dokku triggers + nginx/docker | No |
| `06_GOTCHAS.md` | Known traps: env contract, symlink safety, eval | No |
| `07_TEST_MATRIX.md` | Go unit tests + Vagrant integration tests | No |
| `08_CHANGELOG_LAST_14_DAYS.md` | Recent changes summary | Yes |
| `repo-map.md` | Directory tree, router table, entrypoints | Partially |

## How to Use

1. Start with `repo-map.md` to orient yourself.
2. Read the domain-specific doc for your task area
   (e.g. `02_DOMAIN_MODEL.md` for YAML schema changes,
   `03_CRITICAL_FLOWS.md` for build-config changes).
3. Check `06_GOTCHAS.md` before modifying fragile areas.
4. Only then dive into source files.

## Working Rules for Agents

- Read `repo-map.md` before searching the repository.
- Verify critical flows (`03_CRITICAL_FLOWS.md`) after behavior changes.
- Check `06_GOTCHAS.md` before touching release lifecycle or bash glue.
- Confirm tests per `07_TEST_MATRIX.md` when changing Go code.

## Maintenance

- Run `make atlas-generate`
  (or `python3 scripts/atlas/generate_atlas.py --write`) after structural
  changes (new dirs, moved files).
- Update manual docs when architecture, flows, or state management changes.
- Run `make atlas-check` in CI to catch stale auto-generated files.