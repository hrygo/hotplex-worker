---
type: spec
tags:
  - project/HotPlex
date: 2026-04-25
status: proposed
progress: 0
---
# Specification: Onboard Wizard Refactoring via go:embed & YAML AST Mutation

## 1. Problem Statement

The current implementation of the `hotplex onboard` wizard relies on massive, hardcoded Go string builders (e.g., in `internal/cli/onboard/templates.go`) to generate the default `config.yaml` and `.env` files. 

This approach introduces significant "technical debt":
- **Violation of SSOT (Single Source of Truth)**: Any update to the default `configs/config.yaml` requires a synchronized update to the Go string literals in `templates.go`.
- **Fragility**: Constructing YAML via string concatenation is prone to syntax and indentation errors.
- **Brittle Config Inheritance**: Preserving existing platform configurations (like Slack/Feishu blocks) relies on indentation-based string extraction (`extractPlatformBlock`), which breaks easily if the original YAML formatting changes.

## 2. Proposed Architecture

We propose migrating to an **Embedded Base + AST Mutation** architecture:
1. **Embedding**: Use `//go:embed` to embed the raw, pristine `configs/config.yaml` and `configs/env.example` files directly into the binary. These files become the singular source of truth.
2. **YAML AST Mutation**: Instead of string replacement or Go templates (which would render the base YAML invalid), we will unmarshal the embedded `config.yaml` into a `*yaml.Node` using `gopkg.in/yaml.v3`. This preserves all comments and structural formatting.
3. **Dynamic Configuration**: The wizard will mutate the AST nodes directly (e.g., toggling `messaging.slack.enabled`) based on user input, and then marshal the AST back to disk.

## 3. Implementation Plan

### Step 1: Embed Assets
Create `internal/cli/onboard/embed.go`:
```go
package onboard
import _ "embed"

//go:embed templates/config.yaml
var DefaultConfigYAML []byte

//go:embed templates/env.example
var DefaultEnvExample []byte
```
*(Note: We will need to copy or link the configs directory into the onboard package to satisfy embed rules, or embed them from the root via a root-level package).*

### Step 2: Implement AST Utilities
Create `yamlutil` helpers to interact with `*yaml.Node`:
- `SetNodeValue(root *yaml.Node, path []string, value string)`
- `CopyNode(dst *yaml.Node, src *yaml.Node, path []string)` - used for preserving existing Slack/Feishu configurations safely.

### Step 3: Refactor Wizard Logic
- Refactor `stepConfigGen` to load the embedded YAML, parse it to `*yaml.Node`, apply wizard inputs, and serialize.
- Refactor `stepWriteConfig` to perform simple regex/string replacements on the embedded `.env` content.

### Step 4: Cleanup
- Delete `internal/cli/onboard/templates.go`.
- Delete brittle regex/indentation parsers.

## 4. Return on Investment
- **Zero Duplication**: `configs/config.yaml` is the only file developers need to update.
- **Bullet-proof Upgrades**: AST manipulation completely guarantees valid YAML output and preserves all user comments.
- **Code Reduction**: Deletes ~350 lines of messy string templates and brittle parsers, replaced by ~150 lines of robust AST utilities.
