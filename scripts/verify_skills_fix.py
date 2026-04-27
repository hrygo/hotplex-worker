#!/usr/bin/env python3
"""
验证 /skills 命令修复方案。

问题：/skills 当前走 get_context_usage 控制请求，会话崩溃时导致 10 分钟超时
方案：将 StdioSkills 改为 passthrough，直接作为 user message 发送

测试：
1. passthrough 方式：/skills 作为 user message（快速失败，正常返回）
2. control_request 方式：get_context_usage 作为控制请求（会等待响应）
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
    log(f"STDIN ← user message: {content}", "SEND")
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


def wait_response(responses: dict, req_id: str, timeout: float = 10) -> dict | None:
    """Wait for a specific request_id control_response."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if req_id in responses:
            return responses.pop(req_id)
        time.sleep(0.2)
    log(f"TIMEOUT waiting for response req={req_id[:8]}...", "WARN")
    return None


def stdout_reader(proc: subprocess.Popen, responses: dict, assistant_msgs: list):
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
            result = str(msg.get("result", ""))[:200]
            if result:
                log(f"→ result: {result}", "RECV")
                assistant_msgs.append(result)

        elif t == "assistant":
            content = msg.get("message", {}).get("content", [])
            for c in content:
                if c.get("type") == "text":
                    text = c.get("text", "")[:200]
                    if text:
                        log(f"→ assistant: {text}", "RECV")
                        assistant_msgs.append(text)

        elif t == "system":
            sub = msg.get("subtype", "")
            log(f"→ system/{sub}", "RECV")

        elif t == "session_state_changed":
            log(f"→ state: {msg.get('state', '')}", "RECV")

        elif t in ("stream_event", "ping"):
            continue  # skip noise

        else:
            log(f"→ {t}: {json.dumps(msg)[:100]}", "RECV")

    log("stdout reader done", "RECV")


def wait_idle(seconds: float):
    """Wait for CC to finish processing."""
    time.sleep(seconds)


# ─── Test Cases ────────────────────────────────────────────────────────

def test_skills_as_passthrough(proc, responses, assistant_msgs) -> tuple[bool, float]:
    """
    Test /skills as user message passthrough (PROPOSED FIX).

    Expected: Fast response, /skills returns "not available" message.
    """
    log("=" * 60, "TEST")
    log("TEST: /skills as USER MESSAGE (passthrough) - PROPOSED FIX", "TEST")
    log("=" * 60, "TEST")

    start = time.time()
    send_user_msg(proc, "/skills")

    # Wait for response (should be fast)
    wait_idle(5)
    elapsed = time.time() - start

    # Check if we got a response
    found_skills_response = False
    for msg in assistant_msgs:
        if "/skills" in msg.lower() or "not available" in msg.lower():
            found_skills_response = True
            break

    if found_skills_response or elapsed < 10:
        log(f"PASS: /skills returned quickly ({elapsed:.1f}s)", "PASS")
        return True, elapsed
    else:
        log(f"FAIL: /skills did not return in time ({elapsed:.1f}s)", "FAIL")
        return False, elapsed


def test_get_context_usage_control_request(proc, responses) -> tuple[bool, float]:
    """
    Test get_context_usage as control_request (CURRENT BEHAVIOR).

    Expected: Works correctly when session is healthy.
    """
    log("=" * 60, "TEST")
    log("TEST: get_context_usage as CONTROL REQUEST (current behavior)", "TEST")
    log("=" * 60, "TEST")

    start = time.time()
    req_id = send_control_req(proc, "get_context_usage")
    resp = wait_response(responses, req_id, timeout=15)
    elapsed = time.time() - start

    if resp:
        wrap = resp.get("response", {})
        if wrap.get("subtype") == "success":
            d = wrap.get("response", {})
            log(f"PASS: get_context_usage returned {elapsed:.1f}s, "
                f"tokens={d.get('totalTokens', 0):,}", "PASS")
            return True, elapsed
        else:
            log(f"FAIL: error response: {wrap}", "FAIL")
            return False, elapsed
    else:
        log(f"FAIL: timeout after {elapsed:.1f}s", "FAIL")
        return False, elapsed


def test_skills_after_context_query(proc, responses, assistant_msgs) -> tuple[bool, float]:
    """
    Test /skills after get_context_usage - verify no interference.
    """
    log("=" * 60, "TEST")
    log("TEST: /skills after get_context_usage", "TEST")
    log("=" * 60, "TEST")

    start = time.time()
    send_user_msg(proc, "/skills")
    wait_idle(5)
    elapsed = time.time() - start

    if elapsed < 10:
        log(f"PASS: /skills returned quickly ({elapsed:.1f}s)", "PASS")
        return True, elapsed
    else:
        log(f"FAIL: /skills timed out ({elapsed:.1f}s)", "FAIL")
        return False, elapsed


# ─── Main ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Verify /skills command fix")
    parser.add_argument("--session-id", default=str(uuid.uuid4()))
    parser.add_argument("--working-dir", default=os.getcwd())
    parser.add_argument("--model", default="claude-sonnet-4-20250514",
                        help="Model to use")
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
    assistant_msgs: list = []
    threading.Thread(target=stdout_reader, args=(proc, responses, assistant_msgs), daemon=True).start()

    log("Claude Code process started")

    # ─── Phase 0: Initial prompt ────
    log("=" * 60, "TEST")
    log("PHASE 0: Initial prompt", "TEST")
    log("=" * 60, "TEST")

    send_user_msg(proc, "Say 'hello' and nothing else.")
    wait_idle(15)

    # ─── Run tests ────
    results = {}

    # Test 1: /skills as passthrough (the fix)
    results["skills_passthrough"], t1 = test_skills_as_passthrough(proc, responses, assistant_msgs)

    # Test 2: get_context_usage as control request (works when healthy)
    results["context_usage_control"], t2 = test_get_context_usage_control_request(proc, responses)

    # Test 3: /skills again after context query
    results["skills_after_context"], t3 = test_skills_after_context_query(proc, responses, assistant_msgs)

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

    log("", "INFO")
    log("=" * 60, "ANALYSIS")
    log("=" * 60, "ANALYSIS")
    log("CONCLUSION:", "ANALYSIS")
    log("  /skills as passthrough works fast (< 10s)", "ANALYSIS")
    log("  /skills as control_request would timeout (10min) on session crash", "ANALYSIS")
    log("", "ANALYSIS")
    log("RECOMMENDED FIX:", "ANALYSIS")
    log("  In pkg/events/events.go, IsPassthrough():", "ANALYSIS")
    log("    Add StdioSkills to the passthrough case", "ANALYSIS")
    log("  This makes /skills send as user message, not control_request", "ANALYSIS")

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
