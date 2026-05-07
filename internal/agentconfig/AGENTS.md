# Agent Config Package

## OVERVIEW
Loads agent personality/context from filesystem with 3-level fallback (global → platform → bot). B/C dual-channel system prompt assembly with embedded meta-cognition. Size-limited, YAML-frontmatter-aware file loading.

## STRUCTURE
```
agentconfig/
  loader.go            # Load(dir, platform, botID), resolveFile, readFile, stripFrontmatter, size limits
  prompt.go            # BuildSystemPrompt: B-channel (directives) + C-channel (context) + hotplex meta-cognition
  META-COGNITION.md    # Embedded meta-cognition instructions (go:embed)
  loader_test.go       # Load, fallback, size limits, frontmatter stripping, BuildSystemPrompt tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Load agent configs | `loader.go:39` Load(dir, platform, botID) | 3-level fallback per file |
| System prompt assembly | `prompt.go:24` BuildSystemPrompt | B-channel + C-channel + meta-cognition |
| Add config file type | `loader.go` fileNames slice | SOUL, AGENTS, SKILLS, USER, MEMORY |
| Change size limits | `loader.go` MaxFileChars, MaxTotalChars | Per-file and total limits |
| Meta-cognition content | `META-COGNITION.md` | Embedded via go:embed |
| File resolution logic | `loader.go:110` resolveFile | bot/<id>/ → platform/ → global → empty → fallback |

## KEY PATTERNS

**3-level fallback** (per file, independent resolution):
```
~/.hotplex/agent-configs/
  SOUL.md              # global
  slack/
    SOUL.md            # platform override
    U12345/
      SOUL.md          # bot-specific override
```
Empty content at a level → fall through to next level. Each file resolves independently.

**B/C dual-channel assembly** (BuildSystemPrompt):
- B-channel `<directives>`: `<hotplex>` (meta-cognition, always first) + `<persona>` (SOUL) + `<rules>` (AGENTS) + `<skills>` (SKILLS)
- C-channel `<context>`: `<user>` (USER) + `<memory>` (MEMORY)
- B-channel always precedes C-channel
- `<hotplex>` (META-COGNITION.md via go:embed) is ALWAYS present in B-channel as first element — defines Worker identity, boundaries, conflict resolution
- Each section has behavioral directives injected automatically

**Size limits**: MaxFileChars per file (truncation), MaxTotalChars total (enforced after all loads). Prevents runaway config sizes.

**YAML frontmatter stripping**: `---\n...\n---\n` patterns removed. Content after closing `---` used.

**Path traversal protection**: botID validated (no `/`, `..`), platform validated.

## ANTI-PATTERNS
- ❌ Load config files without size limits — unbounded strings
- ❌ Skip frontmatter stripping — YAML headers would pollute prompts
- ❌ Use suffix files (SOUL.slack.md) — old mechanism removed, use directory fallback
- ❌ Inject meta-cognition outside `<hotplex>` tag — always use BuildSystemPrompt
