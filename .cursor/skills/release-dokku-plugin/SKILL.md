---
name: release-dokku-plugin
description: >-
  Bumps the Dokku plugin version in plugin.toml, commits, tags vX.Y.Z, and
  pushes main plus the tag to origin. Use when releasing this plugin, bumping
  plugin version, cutting a Dokku plugin release, or tagging a new version.
disable-model-invocation: true
---

# Release Dokku Plugin

Run this from the **plugin git root** (the directory that contains `plugin.toml` and `.git`).

Version source of truth for bumping is the latest git tag `v*`. After the release, `plugin.toml`'s `version` must equal the tag without the `v` prefix (they have historically drifted — always sync on release).

## Workflow

Copy and track:

```
Release Progress:
- [ ] 1. Confirm plugin root + current version (from latest tag)
- [ ] 2. Choose bump (patch / minor / major) unless user specified
- [ ] 3. Bump plugin.toml version
- [ ] 4. git add
- [ ] 5. Commit with meaningful message
- [ ] 6. Create annotated tag vX.Y.Z
- [ ] 7. Push origin main and the tag
```

### 1. Confirm context

```bash
pwd
test -f plugin.toml
git status -sb
git log --oneline -5
git describe --tags --abbrev=0          # current release, e.g. v0.0.36
grep -E '^version\s*=' plugin.toml     # may be stale; do not bump from this alone
```

Abort if not on `main`, if the working tree has unrelated unfinished work the user did not intend to release, or if a tag for the new version already exists.

### 2. Choose version bump

SemVer against the **latest tag** (strip leading `v`), not against a stale `plugin.toml`:

| Bump | When |
|------|------|
| **patch** | Bug fixes, small docs/hook fixes (default if unclear) |
| **minor** | New features, backward-compatible behavior |
| **major** | Breaking changes (config, CLI, nginx layout) |

If the user gave an explicit version (e.g. `0.0.37`), use that instead of inferring.

```bash
CUR=$(git describe --tags --abbrev=0)   # e.g. v0.0.36
CUR="${CUR#v}"                          # e.g. 0.0.36
# compute NEW = bump of CUR → X.Y.Z
```

### 3. Bump `plugin.toml`

Update only the `version = "..."` field under `[plugin]` to the new `X.Y.Z` (no `v` prefix).

```toml
[plugin]
description = "dokku nginx-custom plugin"
version = "X.Y.Z"
[plugin.config]
```

### 4. Stage

```bash
git add plugin.toml
# Also stage any other release-intended changes the user asked to include
git status
git diff --cached
```

Do not stage secrets (`.env`, tokens, credentials) or built binaries (`nginx-config-builder`, `file-config`, etc. — they are gitignored).

### 5. Commit

Message must be meaningful: state the release and why (summary of included changes), not only the version number.

```bash
git commit -m "$(cat <<'EOF'
Release vX.Y.Z: <short why / what ships>

EOF
)"
```

Examples:

- `Release v0.0.37: fix nginx -t rollback when current is missing`
- `Release v0.1.0: add rate-limit zone generation`

Follow repo commit style from `git log` when it differs.

### 6. Tag

Annotated tag; name is `v` + `plugin.toml` version (must match):

```bash
NEW=$(grep -E '^version\s*=' plugin.toml | sed -E 's/.*=\s*"([^"]+)".*/\1/')
git tag -a "v${NEW}" -m "Release v${NEW}"
```

### 7. Push main and tag

```bash
git push origin main "v${NEW}"
```

Confirm:

```bash
git status -sb
git log -1 --oneline
git tag -l "v${NEW}"
grep -E '^version\s*=' plugin.toml
```

Report the new version, commit SHA, tag name, and that push succeeded. Consumers install/update via Dokku from this git remote + tag.

## Rules

- Always run inside the plugin repo root (where `plugin.toml` lives).
- Tag format is always `vX.Y.Z`; `plugin.toml` version is always `X.Y.Z` without `v`.
- Derive the bump from the latest `v*` tag, then write that new version into `plugin.toml`.
- Never force-push `main` or move/recreate an existing release tag unless the user explicitly asks.
- Never skip hooks (`--no-verify`) unless the user explicitly asks.
- If the user only asked to bump/tag locally, stop before push.
