---
name: hotplex-issue-manager
description: HotPlex issue batch management and consolidated PR delivery. Analyzes issues, calculates ROI (Impact×Urgency×Effort), selects 1-5 high-priority issues, implements them in ONE branch, and delivers as a SINGLE merged PR. Use whenever: working on HotPlex issues, prioritizing backlog, planning batch fixes, or implementing multiple related issues together. This skill is specifically designed for consolidating multiple issue fixes into one PR — different from traditional one-issue-per-PR workflows.
compatibility: Requires gh CLI, Go 1.26+, golangci-lint, make
---

# HotPlex Issue Manager

Batch issue management workflow for HotPlex that transforms scattered GitHub issues into **one consolidated pull request**. This skill analyzes, prioritizes, and implements multiple issues together, reducing merge conflicts and review overhead.

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

**Issue Quality Checklist**:
- [ ] Title uses conventional commit format: `scope: description`
- [ ] Detailed description of problem or feature
- [ ] Bugs include: reproduction steps, expected vs actual
- [ ] Features include: acceptance criteria, use cases
- [ ] Environment/context provided
- [ ] No duplicates (link related issues)

If issue lacks clarity, add `needs-triage` label or request clarification via comments.

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

**Selection Criteria**:

1. **High ROI** — prioritize maximum impact
2. **Cohesion** — issues should relate to each other
   - Same module (e.g., all messaging issues)
   - Same layer (e.g., all adapter refactors)
   - Related domain (e.g., all performance issues)
3. **No blocking dependencies** — all can be implemented independently
4. **Manageable scope** — total effort should be 1-3 days
5. **Strategic balance** — mix quick wins and important fixes

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

### 4.1 Prepare Repository

```bash
cd /home/hotplex/.hotplex/workspace/hotplex
git fetch origin main
git checkout main
git pull origin main
```

### 4.2 Create Batch Branch

Naming convention: `batch/<theme>-issues-<numbers>`

```bash
git checkout -b batch/messaging-cli-fixes-issues-90-78-89-88
```

**Why descriptive names matter**:
- Easy to understand what branch contains
- Easy to find in git history
- Clear PR title before you even write it

### 4.3 Implement Issues Sequentially

For each issue:

#### Step 1: Understand Issue

```bash
gh issue view <number> --comments
```

Read carefully. Understand the problem. If unclear, ask in issue comments before implementing.

#### Step 2: Implement Fix

Follow HotPlex development standards:

- **Go 1.26+** — Use latest language features
- **golangci-lint** — Run frequently, fix issues immediately
- **Test FIRST** — Write tests before implementation (TDD when possible)
- **Table-driven tests** — Standard Go pattern
- **≥80% coverage** — Higher for security/critical paths

#### Step 3: Commit with Conventional Commits

```bash
git commit -m "refactor(messaging): extract BaseAdapter to eliminate duplication

- Extract common adapter logic into BaseAdapter struct
- Reduce ~300 lines of duplication between Slack and Feishu
- Improve testability and maintainability
- Add table-driven tests for adapter methods

Fixes #88"
```

**Commit message structure**:
- Type: `refactor`, `fix`, `feat`, `perf`, `docs`
- Scope: `messaging`, `cli`, `webchat`, etc.
- Subject: brief description (imperative mood)
- Body: detailed explanation of changes
- Footer: reference issue (Fixes #XX)

**Why one commit per issue**:
- Atomic changes — each commit is independently valid
- Clear history — git bisect works
- Easy to revert — if one fix has problems, can revert just that commit
- Logical grouping — related changes stay together

#### Step 4: Verify

```bash
make lint
make test
go test -coverprofile=/tmp/coverage.out ./...
```

**Don't push after each commit** — implement all issues first, then push once.

### 4.4 Final Integration Testing

After all issues implemented:

```bash
# Run full test suite
make test

# Run linter
make lint

# Build project
make build

# Smoke test
./hotplex version
./hotplex --help
```

**Why integration testing matters**: Unit tests pass doesn't mean system works. Integration tests catch:
- Module interaction bugs
- Configuration issues
- Runtime problems
- Performance regressions

### 4.5 Push Batch Branch

```bash
git push origin batch/messaging-cli-fixes-issues-90-78-89-88
```

### 4.6 Create Consolidated PR

Create **ONE PR** that closes all selected issues:

```bash
gh pr create \
  --base main \
  --title "Batch: fixes and improvements for issues #90, #78, #89, #88" \
  --body "## Summary

This PR implements 4 high-priority issues in a consolidated batch:

- **#90**: Add `hotplex update` subcommand for self-update
- **#78**: Fix error handling and concurrency in messaging adapters
- **#89**: Optimize webchat bundle with code splitting and virtualization
- **#88**: Refactor messaging adapters to extract BaseAdapter

## Fixes

Closes #90, #78, #89, #88

## Changes by Issue

### #88 — Refactor messaging adapters

**Problem**: Slack and Feishu adapters have ~300 lines of duplicated code.

**Solution**: Extract common logic into BaseAdapter struct.

**Changes**:
- Extract BaseAdapter with common connection, message handling, events
- Reduce duplication from ~300 lines to ~50 lines
- Add comprehensive table-driven tests
- Improve error handling consistency

**Impact**: Cleaner code, easier to add new adapters, better testability.

### #78 — Fix messaging error handling

**Problem**: Async message handlers don't propagate errors properly. Race condition in ChatQueue.

**Solution**: Add proper error channels, fix WaitGroup usage.

**Changes**:
- Add error propagation in async message handlers
- Fix race condition in ChatQueue WaitGroup (Add() called after Close())
- Improve error recovery and logging
- Add tests for error scenarios

**Impact**: More reliable messaging, fewer silent failures.

### #90 — Add CLI update subcommand

**Problem**: Users must manually download and install updates.

**Solution**: Add `hotplex update` command that fetches latest release.

**Changes**:
- Implement `hotplex update` subcommand
- Fetch latest release from GitHub API
- Verify signature before replacing binary
- Add tests for update logic
- Document update workflow

**Impact**: Better user experience, easier upgrades.

### #89 — Optimize webchat performance

**Problem**: Large bundle size and slow message rendering.

**Solution**: Code splitting + message virtualization + streaming optimization.

**Changes**:
- Implement code splitting for 40% bundle size reduction
- Add message virtualization for large conversation history
- Optimize streaming delta updates
- Lazy load non-critical components

**Impact**: Faster initial load, smoother chat experience.

## Testing

- [x] Unit tests pass (all packages)
- [x] Integration tests pass (messaging, webchat, CLI)
- [x] Linter passes (`make lint`)
- [x] Manual testing completed:
  - [x] CLI update command tested locally
  - [x] Messaging adapters tested with Slack and Feishu
  - [x] Webchat performance verified (bundle size 40% reduction)
- [x] Coverage ≥80% (actual: 82%)

## Performance Impact

- **Webchat**: Bundle size 40% reduction, initial load 2x faster
- **Messaging**: Reduced memory allocations, better error handling
- **CLI**: New self-update capability

## Breaking Changes

None. All changes are backward compatible.

## Checklist

- [x] Code follows HotPlex style guidelines
- [x] All commits use Conventional Commits format
- [x] Linter passes (`make lint`)
- [x] Tests pass (`make test`)
- [x] Coverage ≥80%
- [x] Documentation updated (godoc comments)
- [x] No merge conflicts with main

## Commits

This PR contains 4 atomic commits, one per issue:
1. refactor(messaging): extract BaseAdapter to eliminate duplication
2. fix(messaging): error handling and concurrency findings
3. feat(cli): add hotplex update subcommand
4. perf(webchat): bundle code split and message virtualization
"
```

**Why comprehensive PR descriptions matter**:
- Reviewer understands what you changed and why
- Future you (or others) can understand the history
- Easier to review — changes organized by issue
- Easier to test — know what to test

### 4.7 Monitor and Address Feedback

1. **Wait for CI** — GitHub Actions runs automatically
2. **Review comments** — Respond to feedback thoughtfully
3. **Make updates** — Push new commits to same branch
4. **Request merge** — Once approved and CI passes

**Important**: This is a **batch PR** — all issues ship together. Don't split into multiple PRs after the fact.

### 4.8 Track Progress

Track in `/tmp/pr_tracking.md`:

```markdown
# HotPlex Batch PR Status

## Batch PR: #XX — Batch: fixes for issues #90, #78, #89, #88

| Issue | Type | Status | Commit | ROI |
|-------|------|--------|---------|-----|
| #90 | enhancement | ✅ Implemented | a1b2c3d | 72 |
| #78 | bug | ✅ Implemented | e4f5g6h | 68 |
| #89 | performance | ✅ Implemented | i7j8k9l | 45 |
| #88 | refactor | ✅ Implemented | m0n1o2p | 28 |

## PR Status

- **PR Number**: #XX
- **Branch**: batch/messaging-cli-fixes-issues-90-78-89-88
- **CI Status**: ✅ Passed
- **Review Status**: 👀 In Review
- **Merge Status**: ⏳ Pending

## Timeline

- Created: 2026-05-01 19:00
- CI complete: 2026-05-01 19:15
- Review requested: 2026-05-01 19:20
- Approved: 2026-05-02 10:30
- Merged: 2026-05-02 11:00 ✅
```

## Best Practices

### Issue Selection
- **Cohesion over quantity** — 3 cohesive issues > 5 unrelated ones
- **Manageable scope** — If >3 days work, split into multiple batches
- **Test thoroughly** — More issues = more complex testing
- **Clear communication** — PR description should explain each issue

### Implementation
- **Sequential commits** — One logical commit per issue
- **Atomic changes** — Each commit independently valid
- **Test frequently** — Run tests after each commit
- **Clean history** — Rebase if commits become messy

### PR Hygiene
- **Comprehensive description** — List all issues and changes
- **Link all issues** — Use "Closes #X, #Y, #Z" format
- **Checklist included** — Show what was tested and verified
- **Be responsive** — Address review feedback promptly

## Common Pitfalls

### Pitfall 1: Selecting Unrelated Issues

**Bad**: Batch a CLI feature, a webchat perf fix, and a messaging refactor

**Why bad**: No cohesion, hard to review, hard to test

**Good**: Batch all messaging adapter fixes, or all CLI improvements

### Pitfall 2: Too Many Issues

**Bad**: 8 issues in one batch

**Why bad**: 
- PR becomes huge and hard to review
- Testing becomes complex
- If one issue has problems, blocks all others

**Good**: 2-5 issues max, depending on complexity

### Pitfall 3: Ignoring Dependencies

**Bad**: Implement issue B that depends on issue A, but implement B first

**Why bad**: Won't work, wastes time

**Good**: Check dependencies, implement in order (refactors before fixes)

### Pitfall 4: Not Testing Integration

**Bad**: Only run unit tests for each issue separately

**Why bad**: Miss integration bugs between issues

**Good**: Run full test suite after all issues implemented

### Pitfall 5: Vague PR Description

**Bad**: "Fixes several issues"

**Why bad**: Reviewer has no idea what you changed

**Good**: Detailed description listing each issue and its changes

## Output Artifacts

The skill produces these artifacts:

1. `/tmp/hotplex_issues.json` — Raw issue data
2. `/tmp/issue_analysis.md` — Detailed analysis per issue
3. `/tmp/issue_ranking.md` — Prioritized list with ROI scores
4. `/tmp/implementation_plan.md` — Batch implementation plan
5. `/tmp/pr_tracking.md` — Single PR status tracker
6. **ONE merged PR** — Final deliverable containing all issue fixes

## Example Session

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
12. Create ONE PR:
    - Title: "Batch: fixes and improvements for issues #90, #78, #89, #88"
    - Closes #90, #78, #89, #88
13. Wait for CI → fix any issues
14. Address review feedback
15. PR merged ✅
16. Result: One merge, 4 issues resolved
```

## Troubleshooting

**Problem**: Can't implement issue A because it depends on issue B

**Solution**: 
- Check dependencies before selecting issues
- Implement dependencies first in the batch
- Or defer to next batch

**Problem**: Test failures after implementing all issues

**Solution**:
- Run tests after each commit, not just at end
- Isolate which commit introduced failure
- Fix that commit, don't just add more commits on top

**Problem**: CI fails for batch PR

**Solution**:
- Check GitHub Actions logs
- Ensure Go version matches (1.26)
- Check all dependencies installed
- If one issue's changes cause CI fail, remove that issue from batch

**Problem**: Review feedback asks to split PR

**Solution**:
- Explain benefits of batch PR
- If reviewer insists, split into logical batches
- But prefer keeping batch if issues are cohesive

**Problem**: Batch becomes too large (>5 issues or >3 days)

**Solution**:
- Split into multiple batches by theme
- Example: "messaging fixes batch 1", "messaging fixes batch 2"

## Key Insight

**Traditional workflow**: One issue → One branch → One PR → One merge
**This skill**: Multiple issues → One branch → One PR → One merge

**Benefits**:
- ✅ Reduced merge conflicts
- ✅ Faster review (one holistic review vs many fragmented ones)
- ✅ Comprehensive testing (integration tested together)
- ✅ Single deployment (coordinated release)
- ✅ Clean history (logical grouping)

**When to use**: When you have 2-5 related issues that can be implemented together in 1-3 days.

**When NOT to use**: When issues are unrelated, or would take >3 days, or have complex dependencies.
