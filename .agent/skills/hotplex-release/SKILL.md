---
name: hotplex-release
description: Standardized release workflow for HotPlex Worker Gateway. Covers version bumping, changelog updates, multi-language SDK synchronization, and GitHub Release automation.
---

# HotPlex Release Workflow Skill

This skill provides a standardized, high-fidelity workflow for releasing new versions of the HotPlex Worker Gateway. Follow these instructions to ensure consistency across the binary, multi-language SDKs, documentation, and GitHub releases.

## Prerequisites

- `gh` CLI authenticated with repo access.
- `make` and `go` 1.26+ installed.
- All tests passing (`make check`).
- Clean working directory.

## Step 1: Version Unification

You MUST update the version string in the following core locations. Use the semver format (e.g., `v1.1.0`).

### 1.1 Core Gateway (Go)
- **`cmd/hotplex/main.go`**: Update `version` variable.
- **`Makefile`**: Update `LDFLAGS` to hardcode the version for the release build (e.g., `-X main.version=v1.1.0`).
- **`internal/tracing/tracing.go`**: Update `semconv.ServiceVersion`.

### 1.2 Multi-Language Examples (SDKs)
- **TypeScript**: `examples/typescript-client/package.json` -> `version`.
- **Python**: `examples/python-client/pyproject.toml` -> `version` and `examples/python-client/hotplex_client/__init__.py` -> `__version__`.
- **Java**: `examples/java-client/pom.xml` -> `<version>` (use `-SNAPSHOT` suffix for dev, remove for final tag if applicable).

### 1.3 Infrastructure & Docs
- **Dockerfile**: `LABEL version="1.x.x"`.
- **Install Script**: `scripts/install.sh` (banner version).
- **Docs**: `docs/_index.md` (Design milestone & titles), `docs/Disaster-Recovery.md` (version table).
- **WebChat Specs**: `webchat/docs/specs/premium-ux-sdk-integration.md` (version header).

## Step 2: Update Changelog

Update `CHANGELOG.md` following the [Keep a Changelog](https://keepachangelog.com/) format.
- Add a new `## [x.x.x] - YYYY-MM-DD` section at the top.
- Categorize changes into `Added`, `Changed`, `Fixed`, `Deprecated`, `Removed`, and `Security`.

## Step 3: Verification

1. **Lint & Test**: Run `make quality` to ensure code integrity.
2. **Build**: Run `make build`.
3. **Verify Binary**: Run `./bin/hotplex-<os>-<arch> version` to confirm the version string is correctly injected.
4. **Docs Check**: Ensure `README.md` and `CHANGELOG.md` are saved.

## Step 4: Git Commitment & Tagging

1. **Commit**: `git add . && git commit -m "chore: release vX.X.X"`
2. **Tag**: `git tag -a vX.X.X -m "Release vX.X.X"`
3. **Push**: `git push origin main && git push origin vX.X.X`

## Step 5: GitHub Release

Standardize the release note by mirroring the `CHANGELOG.md` entry.

```bash
# 1. Extract the latest version notes to a temp file
# (Usually lines 8 to end of section in CHANGELOG.md)

# 2. Create the release via gh CLI
gh release create vX.X.X --title "vX.X.X" --notes-file RELEASE_NOTES.md
```

## Step 6: Post-Release

1. **Clean up**: Remove any temporary release note files.
2. **Announce**: Notify the team and verify the release artifacts (assets) are correctly attached on GitHub.

---
> [!IMPORTANT]
> **HotPlex Specific Constraint**: Always ensure `main.go` and `Makefile` are in sync. If `Makefile` uses `GIT_SHA` for dev builds, ensure it is switched to the literal version string for the final release commit.
