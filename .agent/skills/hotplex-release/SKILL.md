---
name: hotplex-release
description: Standardized release workflow for HotPlex Worker Gateway. Covers version determination, automated change collection, changelog writing, version unification, verification, tagging, and GitHub Release.
---

# HotPlex Release Workflow

## Prerequisites

- `gh` CLI authenticated with repo access
- `make` and `go` 1.26+ installed
- All tests passing (`make check`)
- Clean working directory (no uncommitted changes)

## Branch Guard

**Tags and GitHub Releases MUST only be created on `main` branch.**

1. At the start of the workflow, check the current branch:
   ```bash
   git branch --show-current
   ```
2. **If on `main`**: proceed with the full workflow (Steps 1–8), including tag creation and release.
3. **If NOT on `main`** (feature branch, release prep branch, etc.): execute Steps 1–5 only (version determination, change collection, changelog writing, version unification, verification). Then:
   - Commit the version bump + changelog as a **preparation commit** (e.g. `chore: prepare release vX.X.X`).
   - **Do NOT** create a git tag.
   - **Do NOT** push a tag or trigger GitHub Release.
   - Inform the user: "Release preparation committed on `<branch>`. Tag and publish after merging to main."
4. **After merge to main**: fast-forward or checkout main, then run Step 6 (tag) and Step 7 (push tag + GitHub Release) only.

## Step 1: Determine Next Version

Read the current version from `cmd/hotplex/main.go:16` (the `version` variable).

Apply [Semantic Versioning](https://semver.org/):
- **Patch** (`v1.1.0` → `v1.1.1`): Bug fixes, security patches, no new features
- **Minor** (`v1.1.0` → `v1.2.0`): New features, backwards-compatible changes
- **Major** (`v1.1.0` → `v2.0.0`): Breaking changes

Confirm the new version with the user before proceeding.

## Step 2: Collect Changes

Run the following commands to gather all changes since the last release:

```bash
# Get the last release tag
LAST_TAG=$(git tag --sort=-version:refname | head -1)

# Collect conventional commit summary (grouped by type)
echo "=== Changes since ${LAST_TAG} ==="
git log --oneline "${LAST_TAG}..HEAD" --no-merges

echo ""
echo "=== By Category ==="
echo "--- feat (Added) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^feat'
echo ""
echo "--- fix (Fixed) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^fix'
echo ""
echo "--- refactor / perf (Changed) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^refactor\|^perf'
echo ""
echo "--- chore / ci / docs (Infrastructure) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^chore\|^ci\|^docs\|^build'
echo ""
echo "--- Other ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --invert-grep --grep='^feat\|^fix\|^refactor\|^perf\|^chore\|^ci\|^docs\|^build'

# For detailed review of specific changes
echo ""
echo "=== Full diffstat ==="
git diff --stat "${LAST_TAG}..HEAD"
```

Review each commit's full message for context when needed:

```bash
git log "${LAST_TAG}..HEAD" --no-merges --format="%h %s%n%b---"
```

### Scope Categorization Map

Group commits by their scope into changelog sections:

| Conventional Commit | Changelog Section |
|:---|:---|
| `feat(...)` | **Added** |
| `fix(...)` | **Fixed** |
| `refactor(...)`, `perf(...)` | **Changed** |
| `chore(deps)`, `build(...)` | **Changed** (or **Dependencies** if only dep bumps) |
| feat with breaking change suffix or BREAKING CHANGE footer | **Changed** + callout |
| `ci(...)`, `docs(...)` | Omit from changelog unless user-facing |

### Scope → Display Group Map

When writing changelog entries, group by functional area:

| Commit Scope | Display Group |
|:---|:---|
| `gateway`, `session`, `hub`, `conn` | **Gateway Core** |
| `worker`, `claude-code`, `opencode`, `pi` | **Worker** |
| `slack`, `feishu`, `messaging`, `stt` | **Messaging** |
| `webchat`, `ui`, `chat` | **WebChat UI** |
| `config`, `agent-config` | **Configuration** |
| `security`, `jwt`, `ssrf` | **Security** |
| `cli`, `onboard`, `doctor` | **CLI** |
| `client`, `sdk`, `ts`, `python`, `java` | **SDK** |
| `test`, `ci`, `build`, `makefile` | **Infrastructure** |

## Step 3: Write Changelog

Update `CHANGELOG.md` following [Keep a Changelog](https://keepachangelog.com/) format.

### Template

```markdown
## [X.X.X] - YYYY-MM-DD

### Added

- **Display Group**: One-line description of the change. (#PR or commit SHA for significant changes)

### Changed

- **Display Group**: Description of what changed and why.

### Fixed

- **Display Group**: Description of what was broken and how it was fixed.

### Security

- Description of security-relevant changes (omit section if none).
```

### Writing Rules

1. **One entry per logical change**, not per commit — squash related commits into one entry
2. **Present tense, imperative mood**: "Add feature" not "Added feature" or "Adds feature"
3. **Bold the display group** at the start of each entry for scanability
4. **Include PR number or commit SHA** only for significant/user-facing changes
5. **Omit** internal refactors, CI changes, and doc-only updates unless user-facing
6. **Merge small fixes** into a single "minor fixes" entry if individually insignificant
7. **Order entries** within each section by impact (most significant first)

### Example Entry

```markdown
## [1.2.0] - 2026-04-30

### Added

- **Gateway Core**: Request throttling with in-memory token bucket rate limiter.
- **CLI**: `hotplex status` command for gateway process health monitoring.
- **Messaging**: Speech-to-text support for Feishu via SenseVoice-Small ONNX engine.

### Changed

- **Configuration**: Default `idle_timeout` raised from 30m to 60m.

### Fixed

- **Worker**: Reset context bug causing "done" event leaks during session resets.
- **Slack**: Help command replies to wrong thread — now always replies in original thread.
```

## Step 4: Version Unification

Update the version string in ALL of the following locations. Use the semver format (e.g., `v1.2.0` for code, `1.2.0` for package managers).

### 4.1 Core Gateway (Go)

| File | Pattern | Example |
|:---|:---|:---|
| `cmd/hotplex/main.go:16` | `version = "v1.x.x"` | `v1.2.0` |
| `Makefile:24` | `LDFLAGS ... -X main.version=v1.x.x` | `v1.2.0` |
| `internal/tracing/tracing.go` | `semconv.ServiceVersion("1.x.x")` | `1.2.0` |

### 4.2 Multi-Language SDKs

| File | Pattern |
|:---|:---|
| `examples/typescript-client/package.json` | `"version": "1.x.x"` |
| `examples/python-client/pyproject.toml` | `version = "1.x.x"` |
| `examples/python-client/hotplex_client/__init__.py` | `__version__ = "1.x.x"` |
| `examples/java-client/pom.xml` | `<version>1.x.x-SNAPSHOT</version>` |

### 4.3 Infrastructure

| File | Pattern |
|:---|:---|
| `Dockerfile` | `LABEL version="1.x.x"` |

### Verification Command

After updating, verify all locations were changed:

```bash
# Replace OLD with previous version, NEW with target version
grep -rn "1\.1\.0" cmd/hotplex/main.go Makefile internal/tracing/tracing.go \
  examples/typescript-client/package.json examples/python-client/pyproject.toml \
  examples/python-client/hotplex_client/__init__.py examples/java-client/pom.xml \
  Dockerfile CHANGELOG.md
```

## Step 5: Verification

Run in order:

```bash
# 1. Code quality
make quality

# 2. Build binary
make build

# 3. Verify version injection
./bin/hotplex-$(go env GOOS)-$(go env GOARCH) version

# 4. Verify CHANGELOG formatting
head -50 CHANGELOG.md

# 5. Confirm clean diff (only version + changelog changes)
git diff --stat
```

## Step 6: Git Commit & Tag

```bash
# Stage all version-related files explicitly
git add \
  cmd/hotplex/main.go \
  Makefile \
  internal/tracing/tracing.go \
  examples/typescript-client/package.json \
  examples/python-client/pyproject.toml \
  examples/python-client/hotplex_client/__init__.py \
  examples/java-client/pom.xml \
  Dockerfile \
  CHANGELOG.md

# Commit
git commit -m "chore: release vX.X.X"

# Annotated tag
git tag -a vX.X.X -m "Release vX.X.X"
```

## Step 7: GitHub Release

The CI workflow (`.github/workflows/release.yml`) auto-triggers on tag push and:
- Builds binaries for `darwin-amd64`, `darwin-arm64`, `linux-amd64`, `linux-arm64`
- Computes SHA-256 checksums
- Creates a GitHub Release with `generate_release_notes: true`

```bash
# Push commit and tag to trigger release
git push origin main && git push origin vX.X.X

# Monitor the workflow
gh run list --workflow=release.yml --limit=1

# After CI completes, verify the release
gh release view vX.X.X
```

If you need to manually create a release (e.g., for workflow_dispatch):

```bash
# Extract changelog section to temp file
sed -n "/^## \[X.X.X\]/,/^## \[/p" CHANGELOG.md | head -n -1 > /tmp/release-notes.md

gh release create vX.X.X --title "vX.X.X" --notes-file /tmp/release-notes.md
```

## Step 8: Post-Release

1. Verify release artifacts are attached on GitHub: `gh release view vX.X.X`
2. Verify install works: `./scripts/install.sh --release vX.X.X --prefix /tmp/test-install`
3. Clean up temporary files (`rm -f /tmp/release-notes.md`)
4. Announce to the team

---

> [!IMPORTANT]
> **Sync Check**: `cmd/hotplex/main.go` version, `Makefile` LDFLAGS version, and `CHANGELOG.md` header version MUST all match. The CI workflow overrides `main.version` via ldflags from the git tag, but the source files must be consistent for local builds.

> [!NOTE]
> **CI Auto-Release**: The `.github/workflows/release.yml` workflow handles binary builds, checksums, and release creation automatically on tag push. Manual release creation is only needed for workflow_dispatch or recovery scenarios.
