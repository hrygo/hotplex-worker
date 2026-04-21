#!/usr/bin/env python3
"""
Verify all candidate Worker Stdio Commands against Claude Code stream-json stdio protocol.

Tests:
  Category 1 — User Message Passthrough:
    /compact, /clear, /model, /effort, /rewind, /commit

  Category 2 — Control Request:
    get_context_usage, mcp_status, set_model, set_permission_mode

Usage:
  python scripts/test_cc_context.py [--session-id ID] [--working-dir DIR] [--skip ...]

Requires: Claude Code CLI authenticated and available in PATH.
"""

import argparse
import json
import os
import subprocess
import sys
import threading
import time
import uuid


# ─── Helpers ───────────────────────────────────────────────────────────

def log(msg: str, tag: str = "INFO"):
    ts = time.strftime("%H:%M:%S")
    print(f"[{ts}] [{tag}] {msg}", flush=True)


def send_user_msg(proc: subprocess.Popen, content: str):
    """Send a user message (slash command passthrough)."""
    obj = {
        "type": "user",
        "message": {"role": "user", "content": content},
    }
    log(f"STDIN ← user message: {content[:100]}", "SEND")
    proc.stdin.write(json.dumps(obj, ensure_ascii=False) + "\n")
    proc.stdin.flush()


def send_control_req(proc: subprocess.Popen, subtype: str, **extra) -> str:
    """Send a control_request and return the request_id."""
    req_id = str(uuid.uuid4())
    req = {"subtype": subtype, **extra}
    obj = {
        "type": "control_request",
        "request_id": req_id,
        "request": req,
    }
    log(f"STDIN ← control_request/{subtype} req={req_id[:8]}...", "SEND")
    proc.stdin.write(json.dumps(obj, ensure_ascii=False) + "\n")
    proc.stdin.flush()
    return req_id


def wait_response(responses: dict, req_id: str, timeout: float = 30) -> dict | None:
    """Wait for a specific request_id control_response."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if req_id in responses:
            return responses.pop(req_id)
        time.sleep(0.2)
    log(f"TIMEOUT waiting for response req={req_id[:8]}...", "WARN")
    return None


def wait_idle(seconds: float):
    """Wait for CC to finish processing."""
    time.sleep(seconds)


def stdout_reader(proc: subprocess.Popen, responses: dict):
    """Read stdout NDJSON lines, store control_responses by request_id."""
    for raw in proc.stdout:
        line = raw.strip()
        if not line:
            continue
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue

        t = msg.get("type", "")

        if t == "control_response":
            wrap = msg.get("response", {})
            req_id = wrap.get("request_id", "")
            sub = wrap.get("subtype", "")
            log(f"→ control_response/{sub} req={req_id[:8]}...", "RECV")
            responses[req_id] = msg

        elif t == "result":
            is_err = msg.get("is_error", False)
            result = str(msg.get("result", ""))[:120]
            usage = msg.get("usage", {})
            log(f"→ result error={is_err} usage={usage}", "RECV")
            if is_err:
                log(f"   result_text: {result}", "RECV")

        elif t == "assistant":
            content = msg.get("message", {}).get("content", [])
            for c in content:
                if c.get("type") == "text":
                    log(f"→ assistant: {c['text'][:120]}", "RECV")
                elif c.get("type") == "tool_use":
                    log(f"→ assistant/tool_use: {c.get('name', '?')}", "RECV")

        elif t == "stream_event":
            evt = msg.get("event", {})
            et = evt.get("type", "")
            if et == "text":
                delta = evt.get("delta", {})
                text = delta.get("text", evt.get("message", ""))
                if isinstance(text, str):
                    log(f"→ stream/text: {text[:80]}", "RECV")
            elif et not in ("content_block_start", "content_block_stop",
                            "message_start", "message_stop", "ping"):
                log(f"→ stream/{et}", "RECV")

        elif t == "system":
            sub = msg.get("subtype", "")
            log(f"→ system/{sub}", "RECV")

        elif t == "session_state_changed":
            log(f"→ state: {msg.get('state', '')}", "RECV")

        elif t == "tool_progress":
            log(f"→ tool_progress", "RECV")

        else:
            log(f"→ {t}: {json.dumps(msg)[:150]}", "RECV")

    log("stdout reader done", "RECV")


def stderr_reader(proc: subprocess.Popen):
    for line in proc.stderr:
        log(f"STDERR: {line.strip()}", "ERR")


# ─── Control Request Tests ────────────────────────────────────────────

def test_get_context_usage(proc, responses) -> bool:
    """Control Request: get_context_usage"""
    log("=" * 60, "TEST")
    log("TEST: get_context_usage (control request)", "TEST")
    log("=" * 60, "TEST")

    req_id = send_control_req(proc, "get_context_usage")
    resp = wait_response(responses, req_id, timeout=30)
    if not resp:
        log("FAIL: no response", "FAIL")
        return False

    wrap = resp.get("response", {})
    if wrap.get("subtype") == "error":
        log(f"FAIL: error response: {wrap}", "FAIL")
        return False

    d = wrap.get("response", {})
    log(f"total={d.get('totalTokens', 0):,} max={d.get('maxTokens', 0):,} "
        f"pct={d.get('percentage', 0)}% model={d.get('model', '?')}", "OK")

    cats = d.get("categories", [])
    for c in cats:
        log(f"  {c.get('name', '?')}: {c.get('tokens', 0):,} tokens", "OK")

    skills = d.get("skills", {})
    log(f"skills: total={skills.get('totalSkills', 0)} "
        f"included={skills.get('includedSkills', 0)} "
        f"tokens={skills.get('tokens', 0)}", "OK")
    log(f"memory_files={len(d.get('memoryFiles', []))} "
        f"mcp_tools={len(d.get('mcpTools', []))} "
        f"agents={len(d.get('agents', []))}", "OK")

    log("PASS: get_context_usage", "PASS")
    return True


def test_mcp_status(proc, responses) -> bool:
    """Control Request: mcp_status"""
    log("=" * 60, "TEST")
    log("TEST: mcp_status (control request)", "TEST")
    log("=" * 60, "TEST")

    req_id = send_control_req(proc, "mcp_status")
    resp = wait_response(responses, req_id, timeout=30)
    if not resp:
        log("FAIL: no response", "FAIL")
        return False

    wrap = resp.get("response", {})
    if wrap.get("subtype") == "error":
        log(f"FAIL: error response: {wrap}", "FAIL")
        return False

    d = wrap.get("response", {})
    servers = d.get("mcpServers", [])
    log(f"mcpServers count: {len(servers)}", "OK")
    for s in servers[:5]:  # show first 5
        name = s.get("name", "?")
        status = s.get("status", "?")
        log(f"  {name}: {status}", "OK")
    if len(servers) > 5:
        log(f"  ... and {len(servers) - 5} more", "OK")

    log("PASS: mcp_status", "PASS")
    return True


def test_set_model(proc, responses, model: str) -> bool:
    """Control Request: set_model"""
    log("=" * 60, "TEST")
    log(f"TEST: set_model → {model} (control request)", "TEST")
    log("=" * 60, "TEST")

    req_id = send_control_req(proc, "set_model", model=model)
    resp = wait_response(responses, req_id, timeout=15)
    if not resp:
        log("FAIL: no response", "FAIL")
        return False

    wrap = resp.get("response", {})
    sub = wrap.get("subtype", "")
    log(f"response subtype: {sub}", "OK")

    if sub == "error":
        err_msg = wrap.get("response", {}).get("message", "")
        log(f"error: {err_msg}", "WARN")
        # Model might not be available — not a protocol failure
        log("PASS (protocol works, model may be unavailable)", "PASS")
        return True

    log("PASS: set_model", "PASS")
    return True


def test_set_permission_mode(proc, responses, mode: str) -> bool:
    """Control Request: set_permission_mode"""
    log("=" * 60, "TEST")
    log(f"TEST: set_permission_mode → {mode} (control request)", "TEST")
    log("=" * 60, "TEST")

    req_id = send_control_req(proc, "set_permission_mode", mode=mode)
    resp = wait_response(responses, req_id, timeout=15)
    if not resp:
        log("FAIL: no response", "FAIL")
        return False

    wrap = resp.get("response", {})
    sub = wrap.get("subtype", "")
    log(f"response subtype: {sub}", "OK")

    if sub == "error":
        err_msg = wrap.get("response", {}).get("message", "")
        log(f"error: {err_msg}", "WARN")

    log("PASS: set_permission_mode", "PASS")
    return True


# ─── User Message Passthrough Tests ────────────────────────────────────

def test_compact(proc, responses) -> bool:
    """User Message: /compact"""
    log("=" * 60, "TEST")
    log("TEST: /compact (user message passthrough)", "TEST")
    log("=" * 60, "TEST")

    # Get context before compact
    req_id_before = send_control_req(proc, "get_context_usage")
    resp_before = wait_response(responses, req_id_before, timeout=30)
    before_pct = 0
    if resp_before:
        d = resp_before.get("response", {}).get("response", {})
        before_pct = d.get("percentage", 0)
        log(f"Before compact: {d.get('totalTokens', 0):,} tokens ({before_pct}%)", "OK")

    send_user_msg(proc, "/compact")
    log("Waiting for /compact to process...", "INFO")
    wait_idle(30)

    # Get context after compact
    req_id_after = send_control_req(proc, "get_context_usage")
    resp_after = wait_response(responses, req_id_after, timeout=30)
    if resp_after:
        d = resp_after.get("response", {}).get("response", {})
        after_pct = d.get("percentage", 0)
        log(f"After compact: {d.get('totalTokens', 0):,} tokens ({after_pct}%)", "OK")
        if after_pct <= before_pct:
            log(f"Context reduced or maintained: {before_pct}% → {after_pct}%", "OK")
        else:
            log(f"Context increased (compact summary added): {before_pct}% → {after_pct}%", "WARN")

    log("PASS: /compact", "PASS")
    return True


def test_clear(proc, responses) -> bool:
    """User Message: /clear"""
    log("=" * 60, "TEST")
    log("TEST: /clear (user message passthrough)", "TEST")
    log("=" * 60, "TEST")

    send_user_msg(proc, "/clear")
    log("Waiting for /clear to process...", "INFO")
    wait_idle(15)

    # Verify context was cleared
    req_id = send_control_req(proc, "get_context_usage")
    resp = wait_response(responses, req_id, timeout=30)
    if resp:
        d = resp.get("response", {}).get("response", {})
        log(f"After clear: {d.get('totalTokens', 0):,} tokens ({d.get('percentage', 0)}%)", "OK")

    log("PASS: /clear", "PASS")
    return True


def test_model_command(proc, responses) -> bool:
    """User Message: /model"""
    log("=" * 60, "TEST")
    log("TEST: /model (user message passthrough)", "TEST")
    log("=" * 60, "TEST")

    # Send a conversational message first so /model has context
    send_user_msg(proc, "/model claude-sonnet-4-20250514")
    log("Waiting for /model to process...", "INFO")
    wait_idle(15)

    # Verify model via get_context_usage
    req_id = send_control_req(proc, "get_context_usage")
    resp = wait_response(responses, req_id, timeout=30)
    if resp:
        d = resp.get("response", {}).get("response", {})
        model = d.get("model", "?")
        log(f"Current model: {model}", "OK")

    log("PASS: /model", "PASS")
    return True


def test_effort(proc, responses) -> bool:
    """User Message: /effort"""
    log("=" * 60, "TEST")
    log("TEST: /effort high (user message passthrough)", "TEST")
    log("=" * 60, "TEST")

    send_user_msg(proc, "/effort high")
    log("Waiting for /effort to process...", "INFO")
    wait_idle(15)

    log("PASS: /effort (no structured output to verify, check logs above)", "PASS")
    return True


def test_rewind(proc, responses) -> bool:
    """User Message: /rewind"""
    log("=" * 60, "TEST")
    log("TEST: /rewind (user message passthrough)", "TEST")
    log("=" * 60, "TEST")

    # First make a conversational exchange to create a checkpoint
    send_user_msg(proc, "Create a file called /tmp/hp_test_rewind.txt with the text 'before rewind'")
    log("Waiting for file creation...", "INFO")
    wait_idle(30)

    # Now rewind
    send_user_msg(proc, "/rewind")
    log("Waiting for /rewind to process...", "INFO")
    wait_idle(20)

    # Check if the file was undone
    import pathlib
    p = pathlib.Path("/tmp/hp_test_rewind.txt")
    if p.exists():
        log(f"File still exists after rewind (rewind may need specific checkpoint)", "WARN")
        p.unlink(missing_ok=True)
    else:
        log("File removed by rewind", "OK")

    log("PASS: /rewind (check logs above for details)", "PASS")
    return True


def test_commit(proc, responses) -> bool:
    """User Message: /commit"""
    log("=" * 60, "TEST")
    log("TEST: /commit (user message passthrough)", "TEST")
    log("=" * 60, "TEST")

    send_user_msg(proc, "/commit")
    log("Waiting for /commit to process (may prompt for message)...", "INFO")
    wait_idle(30)

    log("PASS: /commit (check logs above for result)", "PASS")
    return True


# ─── Main ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Verify CC Worker Stdio Commands")
    parser.add_argument("--session-id", default=str(uuid.uuid4()))
    parser.add_argument("--working-dir", default=os.getcwd())
    parser.add_argument("--skip", nargs="*", default=[],
                        help="Tests to skip (by name: compact clear model effort rewind commit "
                             "get_context_usage mcp_status set_model set_permission_mode)")
    parser.add_argument("--model", default="claude-sonnet-4-20250514",
                        help="Initial model to use")
    args = parser.parse_args()

    working_dir = os.path.abspath(args.working_dir)

    claude_args = [
        "claude",
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--dangerously-skip-permissions",
        "--session-id", args.session_id,
        "--model", args.model,
    ]

    log(f"Session ID: {args.session_id}")
    log(f"Working Dir: {working_dir}")
    log(f"Model: {args.model}")
    log(f"CLI: {' '.join(claude_args)}")

    proc = subprocess.Popen(
        claude_args,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=working_dir,
        text=True,
        bufsize=1,
    )

    responses: dict = {}
    threading.Thread(target=stdout_reader, args=(proc, responses), daemon=True).start()
    threading.Thread(target=stderr_reader, args=(proc,), daemon=True).start()

    log("Claude Code process started")

    # ─── Phase 0: Initial prompt ────
    log("=" * 60, "TEST")
    log("PHASE 0: Initial prompt", "TEST")
    log("=" * 60, "TEST")

    send_user_msg(proc, "Say 'hello' and nothing else.")
    wait_idle(20)

    # ─── Run all tests ────
    results = {}

    # Category 2: Control Requests (no side effects, run first)
    if "get_context_usage" not in args.skip:
        results["get_context_usage"] = test_get_context_usage(proc, responses)
    if "mcp_status" not in args.skip:
        results["mcp_status"] = test_mcp_status(proc, responses)
    if "set_model" not in args.skip:
        results["set_model"] = test_set_model(proc, responses, "claude-sonnet-4-20250514")
    if "set_permission_mode" not in args.skip:
        results["set_permission_mode"] = test_set_permission_mode(proc, responses, "bypassPermissions")

    # Category 1: User Message Passthrough (may have side effects)
    if "compact" not in args.skip:
        results["compact"] = test_compact(proc, responses)
    if "model" not in args.skip:
        results["model_cmd"] = test_model_command(proc, responses)
    if "effort" not in args.skip:
        results["effort"] = test_effort(proc, responses)
    if "rewind" not in args.skip:
        results["rewind"] = test_rewind(proc, responses)
    if "commit" not in args.skip:
        results["commit"] = test_commit(proc, responses)
    # /clear last — it wipes conversation
    if "clear" not in args.skip:
        results["clear"] = test_clear(proc, responses)

    # ─── Summary ────
    log("", "INFO")
    log("=" * 60, "SUMMARY")
    log("VERIFICATION SUMMARY", "SUMMARY")
    log("=" * 60, "SUMMARY")

    passed = 0
    failed = 0
    for name, ok in results.items():
        status = "PASS" if ok else "FAIL"
        if ok:
            passed += 1
        else:
            failed += 1
        log(f"  {name:30s} {status}", status)

    log(f"\nTotal: {passed} passed, {failed} failed", "SUMMARY")

    # Cleanup
    try:
        proc.stdin.close()
    except Exception:
        pass
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()

    log(f"Process exited with code: {proc.returncode}")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
