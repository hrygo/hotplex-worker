# Skills Discovery Package

## OVERVIEW

Discovers skill definitions from filesystem directories with TTL-based caching. Scans `~/.claude/skills/`, `~/.agents/skills/` (global) and `<workdir>/.claude/skills/`, `<workdir>/.agents/skills/` (project) for `.md` files with YAML frontmatter.

## STRUCTURE
```
skills.go    # Skill struct + source constants
locator.go   # Locator: TTL cache (5min), background sweep, max 100 entries
scanner.go   # scanDirs → scanDir → parseSkillFile → dedup pipeline
```

## WHERE TO LOOK
| Task | Location |
|------|----------|
| Add scan source | `scanner.go` `scanDirs()` — add entry to dirs slice |
| Change cache TTL | `locator.go` constants (`defaultTTL`, `maxCacheEntries`) |
| Change skill format | `scanner.go` `skillFrontmatter` + `parseSkillFile` |
| List skills | `locator.go` `List(ctx, homeDir, workDir)` — main entry point |

## KEY PATTERNS

**Scan pipeline**: `scanDirs()` → per-dir `scanDir()` → `parseSkillFile()` → `dedup()`
- Subdirectories: looks for `SKILL.md` or `skill.md`
- Flat files: any `.md` with valid YAML frontmatter (`name` required)
- Symlinks skipped (`.agents` often symlinks to `.claude`)

**Dedup**: Project skills override global by name

**Cache**: Keyed by `workDir`, RWMutex-protected, background sweep every 5min

## CONVENTIONS
- Skill `name` comes from YAML frontmatter `name` field (required)
- Skill `description` from frontmatter `description` (optional, folded scalar unfolded)
- Source: `"global"` or `"project"`
