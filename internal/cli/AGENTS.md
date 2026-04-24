# CLI Self-Service Package

## OVERVIEW
Diagnostic checker framework + interactive onboard wizard + structured terminal output. Powers `hotplex doctor`, `hotplex onboard`, and `hotplex security` subcommands.

## STRUCTURE
```
cli/
  checker.go          # Checker interface + CheckerRegistry (DefaultRegistry singleton)
  checker_test.go     # Registry tests
  checkers/
    config.go         # Config validation: YAML parse, required fields, inheritance cycle detection
    dependencies.go   # Binary dependencies: claude, opencode, python3 availability
    environment.go    # Environment: HOME, PATH, data dir writability, disk space
    messaging.go      # Messaging: Slack/Feishu token presence and format validation
    runtime.go        # Runtime: Go version, OS/arch compatibility, POSIX check
    security.go       # Security: JWT secret, admin tokens, TLS config
    stt.go            # STT: Python deps, model files, ONNX validity
    *_test.go         # Per-checker tests (table-driven)
  onboard/
    wizard.go         # Interactive CLI wizard: platform selection, credential input, config generation
    wizard_test.go    # Wizard flow tests
    templates.go      # YAML template builders for Slack/Feishu config generation
  output/
    printer.go        # Color/format printer: status icons (✓ ✗ ⚠), bold, colored headers
    printer_test.go   # Printer tests
    report.go         # Structured diagnostic report renderer: category grouping, summary stats
    report_test.go    # Report tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add new diagnostic check | `checkers/<name>.go` | Implement `Checker` interface, call `DefaultRegistry.Register()` in `init()` |
| Checker interface | `checker.go:29` | `Name()`, `Category()`, `Check(ctx) []Diagnostic` |
| Diagnostic struct | `checker.go:18` | Name, Category, Status (Pass/Warn/Fail), Message, Detail, FixHint, FixFunc |
| Registry | `checker.go:35` | `CheckerRegistry`: Register, All, ByCategory |
| Onboard wizard flow | `onboard/wizard.go` | Interactive prompts: platform → credentials → workdir → generate config |
| Config templates | `onboard/templates.go` | YAML template builders for Slack and Feishu configs |
| Output formatting | `output/printer.go` | `Printer`: StatusIcon, Bold, Color methods for terminal output |
| Report rendering | `output/report.go` | Group diagnostics by category, render summary (pass/warn/fail counts) |

## KEY PATTERNS

**Checker registration**
```go
// checkers/config.go
func init() { DefaultRegistry.Register(&ConfigChecker{}) }
```

**Diagnostic result**
- Status: `StatusPass` / `StatusWarn` / `StatusFail`
- `FixHint`: human-readable remediation instruction
- `FixFunc`: optional auto-fix callback (used by `hotplex doctor --fix`)

**Onboard wizard**
- Platform selection menu (Slack / Feishu / Skip)
- Credential input with validation (token format, app ID patterns)
- Work directory configuration
- Generates config YAML snippet from templates

**Report output**
- Category-grouped diagnostic results
- Summary line: `✓ 12 passed  ⚠ 2 warnings  ✗ 1 failed`

## ANTI-PATTERNS
- ❌ Use `fmt.Println` directly — use `output.Printer` for consistent formatting
- ❌ Skip `FixHint` on Warn/Fail diagnostics — always provide remediation guidance
- ❌ Register checkers outside `init()` — registration happens at package init time
- ❌ Access filesystem or network in `Check()` without context cancellation support
