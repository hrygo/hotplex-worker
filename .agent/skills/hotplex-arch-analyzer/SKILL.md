---
name: hotplex-arch-analyzer
description: HotPlex 项目深度架构和非功能分析，系统性代码库审计。增量式模块分析、跨会话进度追踪、优先分析最少审计模块、自动创建 GitHub Issue 和验收标准。**使用此 skill**：架构分析、代码审计、质量审查、非功能性检查、SOLID/DRY 合规检查、代码健康度改进、模块质量评估。**HotPlex 专用**，针对 Gateway 多层架构（WebSocket/Session/Worker/Messaging）优化分析。支持 /loop 循环执行，适用于大型 Go 代码库的渐进式分析。
---

# Architecture Deep Analyzer

Incremental, module-by-module architecture and non-functional analysis with persistent progress tracking.

## Core Design

Each invocation = **one analysis cycle** on **one module** covering **2-3 aspects**. Designed for `/loop` — repeated calls progressively cover all modules × all aspects across 4 depth phases.

**State persisted in** `.claude/arch-analysis/progress.json` — survives session restarts, enables `/loop` continuity.

## Workflow (each invocation)

```
Load Progress → Select Module → Select Aspects → Deep Analysis → Triage (Confidence+ROI) → Create Issue → Update Progress → Report
```

### Step 1: Load Progress

Read `.claude/arch-analysis/progress.json`.

**If not found** — run initialization:
1. Discover modules (see Module Discovery below)
2. Create progress file with all modules, all aspects pending
3. Output discovered module list
4. Proceed to Step 2

### Step 2: Select Target Module

Priority (first match wins):
1. `analysis_count == 0` — never analyzed, highest priority
2. Lowest `analysis_count` — least coverage
3. Among ties: prefer core modules (gateway > session > messaging > worker > config > others)
4. Among still ties: alphabetical

### Step 3: Select Aspects

Each module has 12 aspects across 4 phases. Select 2-3 from `aspects_pending`:
- If Phase 1 aspects remain → pick from Phase 1
- If Phase 1 done, Phase 2 remain → pick from Phase 2
- etc.
- If ALL aspects covered → re-analyze oldest phase for deeper insights (incremental depth)

### Step 4: Deep Analysis

For each selected aspect, **read ALL source files** in the target module (including `_test.go`).

When analyzing a specific aspect, read the corresponding section in `references/analysis-checklist.md` for a detailed checklist of what to look for. The checklist provides concrete items to verify — use it as a guide, not a mandatory item-by-item checklist.

Analysis must produce **concrete, actionable findings**, each with:
- **What**: Specific issue
- **Where**: `file.go:line_range` reference (include enough context — often 2-3 file locations)
- **Why**: Impact/risk (why this matters — quantify if possible: "affects N call sites", "holds lock for ~200ms per request")
- **How**: Refactoring direction with **code snippets** showing before/after pattern (see Code Snippet Guide below)

#### Code Snippet Guide

Every finding must include a minimal code example demonstrating the problem and the fix direction. This makes issues immediately actionable without re-reading source.

**Good snippet** — shows the problematic pattern and the proposed alternative:
```go
// Current: silent error loss
if err != nil {
    log.Error("failed", "err", err)
    return nil  // caller has no idea this failed
}

// Proposed: propagate with context
if err != nil {
    return fmt.Errorf("handleMessage: %w", err)
}
```

**Too verbose** — don't paste entire functions. Extract the 5-15 line pattern that matters.
**Too vague** — don't write "refactor to use interface" without showing the interface signature.

For structural/design findings (SOLID, DRY), show the proposed interface or type decomposition:
```go
// Proposed extraction
type MessagePipeline interface {
    Parse(ctx context.Context, raw any) (*ParsedMessage, error)
    Filter(ctx context.Context, msg *ParsedMessage) (bool, error)
    Route(ctx context.Context, msg *ParsedMessage) error
}
```

**Severity levels**: Critical (data loss / security / deadlock) > High (reliability / performance) > Medium (maintainability / DRY) > Low (style / naming / minor)

Skip aspects that yield zero genuine findings — don't manufacture issues.

### Step 4.5: Finding Triage (Confidence + ROI)

Before creating an issue, triage each finding through two filters:

#### Confidence Assessment

Rate how certain you are that this is a real problem, not a false positive:

| Level | Criteria |
|-------|----------|
| **High** | Multiple code references confirm the issue; pattern is unambiguous; easily demonstrated with a concrete example |
| **Medium** | Single instance but the pattern is clear; matches well-known anti-patterns; moderate code evidence |
| **Low** | Speculative; may be intentional design; insufficient code evidence; requires deeper investigation to confirm |

**Drop Low-confidence findings** — they waste reviewer time. Note them in progress as "observed but not actionable."

#### ROI Assessment

Evaluate the effort-to-impact ratio of fixing each finding:

| Dimension | Questions to ask |
|-----------|-----------------|
| **Fix effort** | Lines of code to change? Risk of regression? Does it require architectural changes? |
| **Impact if fixed** | Does it unblock other improvements? Reduce bug surface area? Improve testability? |
| **Impact if deferred** | Will it decay further? Block other work? Cause production issues? |

Classify ROI:
- **High ROI** → Small effort, large impact (quick wins, unblocking improvements)
- **Medium ROI** → Moderate effort, clear impact (refactoring patterns, reducing duplication)
- **Low ROI** → Large effort, marginal impact (style issues, premature abstractions)

**Drop Low-ROI findings** unless they are Critical/High severity.

#### Triage Output

After triage, organize findings into the issue:
- Lead with **High confidence + High ROI** findings (most impactful)
- Follow with **Medium confidence or Medium ROI** findings
- Omit Low-confidence or Low-ROI findings (note in progress file)

### Step 5: Create GitHub Issue

Merge ALL findings from this cycle into **one** GitHub issue.

**IMPORTANT formatting rules**:
- Do NOT use `#` followed by a number anywhere in the issue body — GitHub interprets these as issue references (e.g. `#1` links to issue 1). Use descriptive headers or bullet-based numbering instead.
- Use `#### finding-name` or `- **Finding Name**:` for sub-headings, never `#### 1. Title`
- Write "cycle N" or "cycle number N" instead of "cycle #N"

#### Issue Template

The issue follows a structured format with 6 sections: Background → Finding Summary → Findings → Implementation Priority → Out of Scope → Verification.

```bash
gh issue create --title "<type>(<module>): <concise-scope-description>" \
  --label "architecture" --label "<severity-label>" \
  --body "$(cat <<'EOF'
## Background

<1-2 sentences: module's role, current scale (file count, line count), why this analysis was done>

**Scope**: <aspect1>, <aspect2> — cycle N (module analysis pass M)
**Key files**: `<file1.go>`, `<file2.go>`, `<file3.go>`

---

## Finding Summary

| Category | Critical | High | Medium | Low |
|----------|----------|------|--------|-----|
| <aspect1> | <n> | <n> | <n> | <n> |
| <aspect2> | <n> | <n> | <n> | <n> |
| **合计** | **<n>** | **<n>** | **<n>** | **<n>** |

---

## Findings

### <Aspect Name>

#### <descriptive-finding-name-without-numbers>

**Severity**: Critical | **Confidence**: High | **ROI**: High
**Location**: `file.go:123-145`, `file2.go:67-89`

**Problem**: <what's wrong, why it matters — quantify: "affects N call sites", "holds lock for ~Xms">

**Current Pattern**:
```go
// file.go:123-145
<5-15 line excerpt showing the problematic code>
```

**Proposed Fix**:
```go
<proposed code showing the fix direction — interface, type decomposition, or corrected logic>
```

**Estimated Impact**: <quantified: "~N lines reduced", "prevents X class of bugs", "enables Y">

**Acceptance Criteria**:
- [ ] <specific, verifiable change — file + expected behavior>
- [ ] <test added/updated to verify>
- [ ] <no regressions: what must NOT change>

---

<repeat for each finding, separated by --->

---

## Implementation Priority

| Finding | Priority | Effort | Risk | Impact |
|---------|----------|--------|------|--------|
| <finding-name-1> | P0 | Small | Low | ~N lines, unblocks X |
| <finding-name-2> | P1 | Medium | Medium | prevents Y class of bugs |
| <finding-name-3> | P2 | Large | Low | improves Z |

**Recommended starting point**: <which finding to tackle first and why — typically P0/High-ROI>

---

## Out of Scope

The following areas are intentionally NOT changed:
- <area1>: <reason — e.g., "platform API differences make abstraction counterproductive">
- <area2>: <reason — e.g., "already well-abstracted via existing interface">

---

## Verification

- [ ] `make test` passes with no regressions
- [ ] `make lint` produces no new warnings
- [ ] <module-specific behavior verification>
EOF
)"
```

#### Title Format

Use conventional commit style: `<type>(<module>): <scope>`

| Type | When |
|------|------|
| `refactor` | DRY/SOLID/coupling findings |
| `fix` | Error handling/concurrency/resource leak findings |
| `perf` | Performance/scalability findings |
| `security` | Security findings |
| `chore` | Observability/testability/code quality findings |

#### Label Mapping

By max severity across all findings:
- Critical → `P1`
- High → `P2`
- Medium/Low → `P3`

If no findings worth an issue (all low/informational), skip issue creation and note in progress.

### Step 6: Update Progress

Update `.claude/arch-analysis/progress.json`:
- Increment module's `analysis_count`
- Move analyzed aspects → `aspects_covered`
- Add issue number to `issues_created`
- Add dropped findings to `findings_dropped` (with reason)
- Update `last_analyzed` timestamp
- Increment `total_cycles`
- Add to `recent_activity` log (keep last 20)

### Step 7: Report

Output a concise summary with a progress matrix:

```
## Analysis Cycle N Complete

**Module**: `internal/<module>` (analysis pass M)
**Aspects**: <aspect1>, <aspect2>
**Findings**: X Critical, Y High, Z Medium, W Low
**Issue**: <issue URL or "skipped — no actionable findings">

### Coverage Matrix

| Module | Ph1 | Ph2 | Ph3 | Ph4 | Issues | Status |
|--------|-----|-----|-----|-----|--------|--------|
| gateway | 3/3 | 2/3 | — | — | 2 | in progress |
| session | 3/3 | 3/3 | 2/2 | — | 3 | Ph3 done |
| messaging | 1/3 | — | — | — | 1 | Ph1 started |
| ... | | | | | | |

### Stats
- Total cycles: N
- Total issues: M
- Modules fully covered: A/total
- **Next target**: `internal/<next-module>` (analysis_count=N, Phase P pending)
```

The coverage matrix gives a quick visual of where each module stands across the 4 phases. Use `n/3` (or `n/2` for Ph3/4) format for in-progress phases, `—` for unstarted, and checkmark for complete.

---

## Module Discovery

**Primary source**: `CLAUDE.md` STRUCTURE section — if present, use documented module boundaries.

**Fallback**: scan `internal/`, `pkg/`, `cmd/` directories. Each subdirectory with `.go` files = one module.

### Module Grouping

Group tightly-coupled sub-packages:
- `messaging/slack/` + `messaging/feishu/` → analyzed as sub-modules under `messaging`
- `worker/claudecode/` + `worker/opencodeserver/` + `worker/pi/` → sub-modules under `worker`
- `cli/checkers/` + `cli/onboard/` → sub-modules under `cli`

The parent module (`messaging`, `worker`, `cli`) gets its own analysis pass covering shared code (bridge, interfaces, base types).

### Standard Module List (HotPlex)

```
internal/gateway     — WebSocket hub, conn, handler, bridge, API
internal/session     — State machine, store, pool, key derivation
internal/messaging   — Platform adapter, bridge, interaction, STT
  internal/messaging/slack   — Slack Socket Mode adapter
  internal/messaging/feishu  — Feishu WS adapter
internal/worker      — Base worker, proc manager
  internal/worker/opencodeserver — OCS singleton + worker
internal/config      — Viper config, hot-reload
internal/agentconfig — Agent personality/context loader
internal/security    — JWT, SSRF, path safety, command whitelist
internal/admin       — Admin API handlers
internal/aep         — AEP v1 codec
internal/cli         — Checker registry, onboard wizard
internal/skills      — Skills discovery
internal/metrics     — Prometheus counters
internal/tracing     — OpenTelemetry setup
pkg/events           — AEP envelope + event types
cmd/hotplex          — Cobra CLI entry points
```

---

## Analysis Aspects

12 aspects across 4 phases. Each phase goes deeper — Phase 1 is structural, Phase 4 is fine-grained.

### Phase 1: Architecture & Design
| # | Aspect | Focus |
|---|--------|-------|
| 1 | **SOLID** | SRP violations, interface segregation, dependency inversion, open/closed |
| 2 | **DRY** | Duplicated logic within module, cross-module patterns worth extracting |
| 3 | **Coupling** | Import graph, circular dependencies, stable dependencies principle |

### Phase 2: Reliability
| # | Aspect | Focus |
|---|--------|-------|
| 4 | **Error Handling** | Silent failures, error swallowing, sentinel errors, wrapping consistency |
| 5 | **Concurrency** | Race conditions, mutex ordering, goroutine lifecycle, channel leaks |
| 6 | **Resource Mgmt** | Connection/file/goroutine leaks, defer cleanup, shutdown paths |

### Phase 3: Performance & Scale
| # | Aspect | Focus |
|---|--------|-------|
| 7 | **Performance** | Hot paths, unnecessary allocations, N+1 patterns, buffer reuse |
| 8 | **Scalability** | Single-points-of-failure, bottlenecks at 10x/100x load |

### Phase 4: Security & Quality
| # | Aspect | Focus |
|---|--------|-------|
| 9 | **Security** | Input validation gaps, injection risks, auth/authz bypass |
| 10 | **Observability** | Structured logging gaps, metrics coverage, tracing spans |
| 11 | **Testability** | DI coverage, mockability, test gaps for error paths |
| 12 | **Code Quality** | Cyclomatic complexity, god objects, dead code, naming consistency |

---

## Progress File Schema

```json
{
  "version": 1,
  "last_updated": "2026-04-29T21:36:00+08:00",
  "total_cycles": 0,
  "modules": {
    "internal/gateway": {
      "analysis_count": 0,
      "aspects_covered": [],
      "aspects_pending": [
        "solid", "dry", "coupling",
        "error-handling", "concurrency", "resource-mgmt",
        "performance", "scalability",
        "security", "observability", "testability", "code-quality"
      ],
      "issues_created": [],
      "findings_total": 0,
      "findings_dropped": [],
      "last_analyzed": null
    }
  },
  "issues": [],
  "recent_activity": []
}
```

The `findings_dropped` array tracks Low-confidence or Low-ROI findings dropped during triage — useful for revisiting in re-analysis passes. Each entry: `{"finding": "name", "reason": "Low confidence — may be intentional"}`.

---

## AC Writing Guide

Good AC is **specific, verifiable, and testable**. Each finding's AC should:

1. **State the change**: What code/files must be modified
2. **Define success**: How to verify the fix works (test case, behavior)
3. **Specify constraints**: What must NOT change (backward compat, performance budget)

**Pattern**: For each finding, write 2-4 AC items covering the change, its verification, and boundary conditions.

**Examples**:

Bad: "Fix the error handling"
Good:
```
- [ ] `bridge.go:HandleMessage` returns error instead of logging and returning nil
- [ ] Error propagated to caller via SessionConn.Send with AEP error event
- [ ] Test added: `TestHandleMessage_ErrorPropagation` verifies error reaches client
```

Bad: "Improve performance"
Good:
```
- [ ] Replace per-message `bytes.Buffer` allocation in `WritePump` with sync.Pool
- [ ] Benchmark `BenchmarkWritePump_Throughput` shows <10ns/op improvement
- [ ] No allocation increase under `go test -count=5 -bench=.` pprof
```

Bad: "Refactor adapters to share code"
Good (inspired by issue 65 style):
```
- [ ] Extract shared adapter fields to `PlatformAdapter` base struct in `messaging/platform_adapter.go`
- [ ] Slack and Feishu adapters embed `*PlatformAdapter` instead of duplicating fields
- [ ] `make test` passes with zero regressions in both adapter test suites
- [ ] Adding a new platform adapter requires implementing 3 interfaces + 1 StreamingAPI, not 15+ files
```

---

## Edge Cases

- **No findings**: If analysis yields nothing actionable, update progress (mark aspects as covered) and skip issue creation. Note "no significant findings" in progress.
- **Cross-module findings**: If analysis reveals issues spanning multiple modules, note them in the issue but tag the primary module. Add a `cross-cutting` note.
- **Module too large** (>1500 lines): Split into sub-modules for analysis (e.g., `gateway` → `gateway/core` + `gateway/bridge` + `gateway/api`). Track sub-modules independently.
- **Re-analysis**: When all aspects are covered for a module, re-analyzing goes deeper — look for issues missed on first pass, check if previous findings were resolved, identify new patterns.
