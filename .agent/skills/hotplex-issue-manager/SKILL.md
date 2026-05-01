---
name: hotplex-issue-manager
description: HotPlex issue batch management and consolidated PR delivery. TRIGGER THIS SKILL when: managing HotPlex issues, prioritizing backlog, planning batch fixes, implementing multiple related issues together, needing to consolidate several fixes into one PR, calculating ROI for issue prioritization, or wanting to reduce merge conflicts and review overhead. This skill transforms scattered GitHub issues into ONE consolidated PR — a deliberate alternative to traditional one-issue-per-PR workflows that often create merge conflicts and review fatigue.
compatibility: Requires gh CLI, Go 1.26+, golangci-lint, make
---

# HotPlex Issue Manager

Batch issue management workflow for HotPlex that transforms scattered GitHub issues into **one consolidated pull request**. This skill analyzes, prioritizes, and implements multiple issues together, reducing merge conflicts and review overhead.

## Common Pitfalls (Read This First!)

Before diving into the workflow, avoid these frequent mistakes that derail batch PRs:

**❌ Selecting unrelated issues together**
- Mixing CLI features, webchat fixes, and messaging refactors in one batch
- **Why this fails**: No thematic cohesion makes reviews painful and testing complex
- **✅ Better approach**: Group by domain — all messaging fixes, or all CLI improvements

**❌ Overloading batches (too many issues)**
- Trying to cram 8+ issues into "one mega PR"
- **Why this fails**: Creates unreviewable monsters; if one issue blocks, all block
- **✅ Better approach**: 2-5 issues max, or split into themed batches (messaging batch 1, batch 2)

**❌ Ignoring dependencies between issues**
- Implementing issue B that depends on issue A's refactor
- **Why this fails**: Code won't work, wastes implementation time
- **✅ Better approach**: Check dependencies first, implement refactors before fixes

**❌ Only running unit tests per issue**
- Testing each fix in isolation, never together
- **Why this fails**: Misses integration bugs where issues interact
- **✅ Better approach**: Run full test suite after all issues implemented

**❌ Vague PR descriptions**
- Writing "Fixes several issues" as the PR description
- **Why this fails**: Reviewers can't understand what changed or why
- **✅ Better approach**: Document each issue separately with problem/solution/impact

## Why Batch PRs Matter

Traditional one-issue-per-PR workflows create problems:
- Multiple merge conflicts when touching similar code
- Review fatigue from seeing many small related PRs
- Deployment overhead from many individual merges
- Fragmented git history

**Batch PRs solve these** by grouping related fixes:
- One branch = one merge conflict resolution
- One review = holistic understanding of changes
- One deployment = coordinated release
- Clean history = logical grouping of related work

## Workflow Overview

Four phases guide you from raw issues to merged PR:

1. **Analysis & Verification** — Understand what issues actually need
2. **Prioritization** — Score issues by ROI to focus on high-impact work
3. **Selection** — Choose 1-5 issues that work well together
4. **Implementation & Delivery** — Build and deliver one consolidated PR

## Phase 1: Analysis & Verification

### 1.1 Fetch Issues

```bash
cd /home/hotplex/.hotplex/workspace/hotplex
gh issue list --limit 100 --state open \
  --json number,title,body,labels,state,author,createdAt,comments \
  > /tmp/hotplex_issues.json
```

### 1.2 Analyze Each Issue

For each issue, check:

**Completeness** — Can you implement this from the description?
- Clear problem statement or feature request?
- Reproduction steps (for bugs)?
- Acceptance criteria (for features)?
- Environment/context details?

**Validity** — Is this a real issue?
- Can it be reproduced?
- Is the feature scoped and actionable?
- Or is it vague, needs clarification?

**Duplication** — Has this been reported?
- Search for similar issues by keywords
- Check if already fixed in another branch
- Link duplicates instead of implementing

**Technical Feasibility** — Can this be implemented?
- Fits within architecture?
- Has blocking dependencies?
- Requires research/unknowns?

### 1.3 Apply Labels

Labels help categorize and prioritize issues:

**Type Labels** (choose one that best fits):
- `bug` — Broken functionality, crashes, incorrect behavior
- `enhancement` — New features or capabilities
- `documentation` — Docs, README, examples
- `performance` — Speed, memory, optimization
- `refactor` — Code quality, DRY, SOLID improvements
- `security` — Vulnerabilities, security hardening

**Domain Labels** (can apply multiple):
- `architecture` — Design patterns, coupling, separation of concerns
- `race-condition` — Concurrency bugs, data races
- `goroutine` — Goroutine leaks, lifecycle management
- `resource-leak` — Memory leaks, file descriptor leaks
- `reliability` — Availability, error handling, recoverability

```bash
gh issue edit <number> --add-label "bug,architecture"
gh issue edit <number> --remove-label "enhancement"
```

**Issue Quality Checklist** (consider these before implementing):
- [ ] Title uses conventional commit format: `scope: description`
- [ ] Detailed description of problem or feature
- [ ] Bugs include: reproduction steps, expected vs actual
- [ ] Features include: acceptance criteria, use cases
- [ ] Environment/context provided
- [ ] No duplicates (link related issues)

**Why this matters**: Implementing unclear issues wastes time — you'll discover edge cases mid-work or build the wrong thing. If issue lacks clarity, add `needs-triage` label or request clarification via comments before starting implementation.

## Phase 2: Prioritization & Scoring

### 2.1 ROI Scoring System

ROI (Return on Investment) helps focus on high-impact work. Score each issue 1-10 on three dimensions:

**Impact (I)**: How much does this matter to users?
- 10: Critical bug affecting all users, security issue, data loss
- 7-8: High-impact bug, major performance win, highly-requested feature
- 5-6: Medium impact, noticeable improvement
- 3-4: Low impact, minor improvement
- 1-2: Nice to have, minimal user-facing effect

**Urgency (U)**: How time-sensitive is this?
- 10: Production outage, security vulnerability, blocking release
- 7-8: Affecting many users daily, degrading experience
- 5-6: Should fix soon, but not urgent
- 3-4: Whenever we get to it
- 1-2: No deadline

**Effort (E)**: How complex to implement? (inverted — higher = easier)
- 10: Trivial (1-2 hours, simple fix, well-understood)
- 7-8: Easy (half day, minimal complexity)
- 5-6: Medium (1-2 days, some complexity)
- 3-4: Hard (3-5 days, significant complexity)
- 1-2: Very hard (1+ week, research required, architectural changes)

**ROI Formula**:
```
ROI = (Impact × Urgency × Effort) / 100
```

**Why this formula works**:
- High-impact issues get priority (Impact numerator)
- Urgent issues get priority (Urgency numerator)
- Easy fixes get priority (Effort numerator — inverted scoring)
- Result: prioritize work that delivers maximum value fast

### 2.2 Assign Priority Labels

Based on ROI score and issue type:

- **P1** (Critical): ROI ≥ 50, OR security/crash/data-loss issues
- **P2** (High): ROI 30-49, OR high-impact bugs
- **P3** (Medium): ROI 15-29, OR technical debt/refactoring

```bash
gh issue edit <number> --add-label "P1"  # Critical
gh issue edit <number> --add-label "P2"  # High
gh issue edit <number> --add-label "P3"  # Medium
```

### 2.3 Check Dependencies

Issues often depend on each other:

```bash
# Find referenced issues in body
gh issue view <number> --json body --jq '.body' | grep -o '#[0-9]\+'
```

Build dependency graph. Issues blocked by unresolved dependencies should be marked `blocked` and deprioritized — you can't implement them yet anyway.

### 2.4 Generate Ranked List

Create `/tmp/issue_ranking.md`:

```markdown
# HotPlex Issues — Prioritized by ROI

## P1 — Critical (ROI 50+)
- [ ] #90 — feat(cli): add `hotplex update` subcommand (ROI: 72)
  - Impact: 8 (highly-requested feature)
  - Urgency: 9 (users asking for it)
  - Effort: 10 (trivial, 4 hours)
  
- [ ] #78 — fix(messaging): error handling (ROI: 68)
  - Impact: 8 (affects many users)
  - Urgency: 8 (daily occurrences)
  - Effort: 9 (easy, 6 hours)

## P2 — High Priority (ROI 30-49)
- [ ] #89 — perf(webchat): bundle code split (ROI: 45)
  - Impact: 7 (performance win for all webchat users)
  - Urgency: 6 (degradation over time)
  - Effort: 8 (medium, 1 day)
```

## Phase 3: Selection

### 3.1 Choose Issues for Batch PR

Select 1-5 issues that will be implemented together in one PR. **Selection quality matters more than quantity** — better to ship 3 issues cleanly than 5 issues messily.

**Selection Criteria** (prioritize in this order):

1. **High ROI** — prioritize maximum impact
   - **Why**: You want to deliver the most value with your time investment
2. **Cohesion** — issues should relate to each other
   - Same module (e.g., all messaging issues)
   - Same layer (e.g., all adapter refactors)
   - Related domain (e.g., all performance issues)
   - **Why**: Cohesive batches are easier to review, test, and understand
3. **No blocking dependencies** — all can be implemented independently
   - **Why**: Dependencies complicate implementation order and can block progress
4. **Manageable scope** — total effort should be 1-3 days
   - **Why**: Larger batches become unreviewable and riskier to ship
5. **Strategic balance** — mix quick wins and important fixes
   - **Why**: Quick wins build momentum while important fixes deliver long-term value

**Why cohesion matters**: Implementing unrelated issues in one PR makes:
- Code review harder (reviewer context-switches)
- Testing harder (need to test unrelated things)
- Rollback harder (can't revert one fix without reverting others)
- Understanding harder (git history tells no clear story)

**Selection Strategies**:

**Conservative** (2 issues, low risk):
- Top 1 P1 + 1 high-ROI P2
- Quick wins, thoroughly tested
- Good when: unsure, want to validate workflow

**Balanced** (3-4 issues, mixed complexity):
- 1 P1 + 2 P2 + 1 P3
- Good balance of impact and effort
- Good when: confident in issues, have 2-3 days

**Aggressive** (5 issues, maximize throughput):
- All P1 + top P2 issues
- Only if total effort ≤ 3 days AND high cohesion
- Good when: issues are simple and highly related

### 3.2 Create Implementation Plan

Document the batch in `/tmp/implementation_plan.md`:

```markdown
# HotPlex Batch Implementation Plan

## Selected Issues (4 issues, Total ROI: 198)

1. **#90** — feat(cli): add `hotplex update` subcommand (ROI: 72)
   - Type: enhancement
   - Scope: CLI
   - Effort: 4 hours
   
2. **#78** — fix(messaging): error handling (ROI: 68)
   - Type: bug
   - Scope: messaging/adapters
   - Effort: 6 hours
   
3. **#89** — perf(webchat): bundle code split (ROI: 45)
   - Type: performance
   - Scope: webchat
   - Effort: 8 hours
   
4. **#88** — refactor(messaging): extract BaseAdapter (ROI: 28)
   - Type: refactor
   - Scope: messaging/adapters
   - Effort: 4 hours

## Cohesion Analysis

- #88 and #78: Both messaging adapters, highly cohesive
- #90: CLI work, independent but low effort
- #89: Webchat performance, independent

**Risk**: Medium — mixing modules (messaging + CLI + webchat)

## Implementation Order

1. #88 (refactor) — Foundation, other issues may depend on clean structure
2. #78 (bug fix) — Depends on #88 refactoring
3. #90 (feature) — Independent, can be done anytime
4. #89 (performance) — Independent, can be done anytime

## Branch Strategy

- Branch: `batch/messaging-cli-webchat-fixes-issues-90-78-89-88`
- Base: `main`
- All fixes in one branch
- One commit per issue

## Testing Strategy

- Unit tests for each fix
- Integration tests for messaging changes
- Manual testing for CLI and webchat
- Coverage target: ≥80%

## Timeline

- Total effort: 22 hours (~3 days)
- Implementation: 18 hours
- Testing: 4 hours
```

## Phase 4: Implementation & Delivery

**📚 Detailed implementation guide**: See `references/implementation-guide.md` for complete Phase 4 details including:
- Step-by-step repository preparation
- Branch creation and naming conventions
- Sequential issue implementation workflow
- Conventional commit message templates
- Integration testing procedures
- PR creation with complete template

**Quick overview**:

1. **Prepare repository** — Fetch latest main, ensure clean state
2. **Create batch branch** — `batch/<theme>-issues-<numbers>`
3. **Implement issues sequentially** — One commit per issue, following HotPlex standards
4. **Final integration testing** — Full test suite, linter, build verification
5. **Push and create PR** — ONE consolidated PR with complete description

**Key implementation standards**:
- **Go 1.26+** with latest language features
- **golangci-lint** — run frequently, fix immediately
- **Test FIRST** — write tests before implementation (TDD)
- **≥80% coverage** — higher for security/critical paths
- **Conventional commits** — type(scope): description format
- **Atomic commits** — each commit independently valid

## Best Practices

### Cohesion over Quantity

Better to ship 3 cohesive issues than 5 unrelated ones. Cohesive batches tell a clear story and are easier to review.

### Test FIRST

Write tests before implementation. Tests serve as executable specs and prevent regressions.

### One Commit Per Issue

Each commit should be atomic and independently valid. This enables:
- Easy git bisect for debugging
- Selective revert if needed
- Clear git history

### Run Full Test Suite

Don't rely on unit tests alone. Run integration tests after all issues implemented to catch interaction bugs.

### Document Changes Clearly

PR description should separate changes by issue with Problem/Solution/Impact structure. This helps reviewers understand what changed and why.

## Output Artifacts

Batch issue management produces these artifacts:

1. `/tmp/hotplex_issues.json` — Raw issue data
2. `/tmp/issue_analysis.md` — Detailed analysis per issue
3. `/tmp/issue_ranking.md` — Prioritized list with ROI scores
4. `/tmp/implementation_plan.md` — Batch implementation plan
5. `/tmp/pr_tracking.md` — Single PR status tracker
6. **ONE merged PR** — Final deliverable containing all issue fixes

## Example Session

**📚 Complete walkthrough**: See `references/example-session.md` for a full example session showing:
- Real issue analysis and ROI calculation
- Cohesive batch selection (4 issues, ROI 278)
- Sequential implementation with commit messages
- Complete PR description template
- Final results and time savings (30% faster than traditional workflow)

**Quick preview**:
```
User: "分析 HotPlex issues 并交付最高优先级的修复"

1. Fetch 20 open issues
2. Analyze, categorize, score ROI
3. Select top 4: #90 (CLI update), #78 (messaging errors), 
   #89 (webchat perf), #88 (adapter refactor)
4. Verify cohesion: 3 messaging issues + 1 CLI issue = acceptable
5. Create branch: batch/messaging-cli-fixes-issues-90-78-89-88
6. Implement #88 → commit (refactor first)
7. Implement #78 → commit (fix depends on refactor)
8. Implement #90 → commit (independent feature)
9. Implement #89 → commit (independent perf)
10. Run all tests → verify integration
11. Push branch
12. Create ONE PR closing all 4 issues
13. CI passes, review, merge ✅
Result: One merge, 4 issues resolved, 30% time savings
```

## Troubleshooting

**📚 Complete troubleshooting guide**: See `references/troubleshooting.md` for detailed solutions to common problems:

1. **Can't implement issue A because it depends on issue B**
   - Check dependencies before selecting
   - Implement dependencies first in batch
   - Or defer to next batch

2. **Test failures after implementing all issues**
   - Run tests after each commit, not just at end
   - Use git bisect to find problematic commit
   - Fix that commit specifically

3. **CI fails for batch PR**
   - Check GitHub Actions logs
   - Ensure Go version matches (1.26)
   - If one issue causes failure, remove from batch

4. **Review feedback asks to split PR**
   - Explain benefits of batch PR
   - If reviewer insists, split into logical batches
   - But prefer keeping batch if issues are cohesive

5. **Batch becomes too large (>5 issues or >3 days)**
   - Split into multiple batches by theme
   - Example: "messaging fixes batch 1", "messaging fixes batch 2"

## Key Insight

**Traditional workflow**: One issue → One branch → One PR → One merge
**This skill**: Multiple issues → One branch → One PR → One merge

**Why this pattern works**:
- ✅ **Reduced merge conflicts** — One conflict resolution instead of many
- ✅ **Faster review** — One holistic review vs many fragmented ones
- ✅ **Comprehensive testing** — Integration tested together, catches interaction bugs
- ✅ **Single deployment** — Coordinated release, easier to rollback if needed
- ✅ **Clean history** — Logical grouping tells a coherent story

**When to use this skill**:
- You have 2-5 related issues that can be implemented together in 1-3 days
- Issues touch similar code areas (same module, layer, or domain)
- You want to reduce merge conflicts and review overhead
- You need to prioritize issues by ROI and focus on high-impact work

**When NOT to use this skill**:
- Issues are completely unrelated (different modules, no thematic connection)
- Total effort would exceed 3 days
- Issues have complex, hard-to-resolve dependencies
- You need immediate hotfix for a single critical issue

**Remember**: This skill is a tool, not a mandate. Use it when it makes sense for your situation.
