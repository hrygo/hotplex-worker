# Claude Code Worker Adapter

## OVERVIEW
Claude Code binary adapter using stdio transport (`claude --print --session-id`). Manages session lifecycle via `--resume`, NDJSON parsing of Claude's streaming JSON output, AEP event mapping, prompt file injection (B/C channels), and tool permission auto-approval.

## STRUCTURE
```
claudecode/
  worker.go       # Worker struct: Start/Resume/Input/Terminate, CLI arg construction, session file management (855 lines)
  parser.go       # Parser: Claude JSON streaming → WorkerEvent (11 event types) (385 lines)
  mapper.go       # Mapper: WorkerEvent → AEP Envelope conversion (288 lines)
  control.go      # Control request routing: context_usage, mcp_status, set_model, permissions (218 lines)
  types.go        # WorkerEvent, StreamPayload, ToolCallPayload, ResultPayload, ControlSubtype constants (77 lines)
  test_helpers.go # Test utilities (113 lines)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Worker lifecycle | `worker.go:96` Worker struct | Embeds `base.BaseWorker` |
| Start new session | `worker.go:149` Start() | Builds CLI args, starts process, reads output |
| Resume session | `worker.go:155` Resume() | `--resume` flag + existing session ID |
| CLI arg construction | `worker.go:241` buildCLIArgs() | `claude --print --session-id --output-format stream-json` + flags |
| Input delivery | `worker.go:335` Input() | Writes to stdin via base.SessionConn |
| Process output reading | `worker.go:569` readOutput() | Line-by-line parse via Parser → Mapper → trySend |
| Prompt file injection | `worker.go:809` writePromptFile() | Temp file for `--append-system-prompt-file` (Windows-safe) |
| Session file management | `worker.go:494` sessionFileGlobs() | `~/.claude/projects/**/{session-id}.json` |
| Has session files check | `worker.go:505` HasSessionFiles() | For resume eligibility |
| Tool auto-approve | `worker.go:67` autoApproveTool() | Permission auto-response for safe tools |
| NDJSON parsing | `parser.go:101` ParseLine() | Routes to parseStreamEvent/Assistant/ToolProgress/Result/Control |
| AEP event mapping | `mapper.go:44` Map() | WorkerEvent → AEP Envelope with seq allocation |
| Session state mapping | `mapper.go:31` statusToSessionState() | Claude status strings → SessionState |
| Control request routing | `control.go` | context_usage, mcp_status, set_model, set_permission, skills |

## KEY PATTERNS

**CLI invocation**
```
claude --print --session-id <id> --output-format stream-json \
  --append-system-prompt-file <temp-file> \
  --max-turns <n> --model <model> --allowedTools <tools> \
  --resume (for resume)
```

**Output parsing pipeline**
```
Process stdout → Parser.ParseLine() → WorkerEvent → Mapper.Map() → AEP Envelope → trySend(hub)
```

**Parser event types (11)**
- `stream` (thinking/text/code/image/tool_use), `assistant`, `tool_progress`, `tool_result`, `result`, `control`, `system`, `session_state`, `interrupt`

**Session resume logic**
- `HasSessionFiles()` checks `~/.claude/projects/**/{session-id}.json`
- Resume only if session files exist AND `--resume` flag supported
- On resume failure: falls back to fresh start

**Prompt file injection (Windows-safe)**
- System prompt written to temp file via `writePromptFile()`
- Passed via `--append-system-prompt-file` (avoids cmd.exe truncation on Windows)
- Cleaned up on session termination via `cleanupPromptFiles()`

**Tool permission auto-approval**
- `permissionAutoApprove` map: safe tools auto-approved without user interaction
- `permissionPrompt` map: tools requiring explicit user approval
- Configured via `autoApproveTool()` based on tool name patterns

**Mapper: Claude-specific → AEP translation**
- `stream` (thinking) → AEP `reasoning` event
- `stream` (text/code) → AEP `message.start/delta/end` sequence
- `tool_use` → AEP `tool_call`
- `tool_result` → AEP `tool_result`
- `result` → AEP `done` (with success/stats)
- `system` → AEP `step` or raw event
- `session_state` → AEP `state`

## ANTI-PATTERNS
- ❌ Use inline `--system-prompt` arg — use `--append-system-prompt-file` (cmd.exe truncation on Windows)
- ❌ Resume without checking `HasSessionFiles()` — would create broken session
- ❌ Block on `trySend()` — uses non-blocking select with backpressure awareness
- ❌ Propagate nested agent env vars — `claudeCodeEnvBlocklist` strips them for security
- ❌ Skip `cleanupPromptFiles()` on termination — temp files leak
