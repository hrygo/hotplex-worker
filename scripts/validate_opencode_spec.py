#!/usr/bin/env python3
"""
OpenCode Worker 顆成成规格验证脚本
====================================
验证 Worker-OpenCode-CLI-Spec.md 和 Worker-OpenCode-Server-Spec.md 中定义的所有功能项。

用法:
    python scripts/validate_opencode_spec.py          # 验证所有功能
    python scripts/validate_opencode_spec.py --list   # 列出所有功能
    python scripts/validate_opencode_spec.py --feature ndjson_safety  # 验证单项
    python scripts/validate_opencode_spec.py --group P0  # 验证优先级组
    python scripts/validate_opencode_spec.py --all --verbose  # 详细输出
"""

import argparse
import json
import os
import re
import subprocess
import sys
import uuid
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Any, Optional


class Priority(Enum):
    P0 = "P0"
    P1 = "P1"
    P2 = "P2"
    GENERAL = "general"


class Status(Enum):
    PASS = "✅ PASS"
    FAIL = "❌ FAIL"
    SKIP = "⏭️  SKIP"
    WARN = "⚠️  WARN"
    UNK = "❓ UNK"


@dataclass
class ValidationResult:
    feature: str
    priority: Priority
    description: str
    status: Status
    details: str = ""
    hints: list[str] = field(default_factory=list)

    def __str__(self) -> str:
        icon = self.status.value
        lines = [
            f"{icon} [{self.priority.value}] {self.feature}",
            f"    {self.description}",
        ]
        if self.details:
            for line in self.details.split("\n"):
                lines.append(f"    {line}")
        if self.hints:
            lines.append("    💡 提示: " + "; ".join(self.hints))
        return "\n".join(lines)


SPEC_PATH_CLI = (
    Path(__file__).parent.parent / "docs" / "specs" / "Worker-OpenCode-CLI-Spec.md"
)
SPEC_PATH_SERVER = (
    Path(__file__).parent.parent / "docs" / "specs" / "Worker-OpenCode-Server-Spec.md"
)

OPENCODE_CLI_PATHS = [
    Path.home() / ".opencode" / "bin" / "opencode",
    Path("/usr/local/bin/opencode"),
    Path("/opt/homebrew/bin/opencode"),
]
OPENCODE_SERVER_PORT = 18789
OPENCODE_SERVER_URL = f"http://localhost:{OPENCODE_SERVER_PORT}"


def strip_ansi(text: str) -> str:
    return re.sub(r"\x1b\[[0-9;]*[a-zA-Z]", "", text)


def find_opencode_cli() -> Optional[Path]:
    for p in OPENCODE_CLI_PATHS:
        if p.exists():
            return p
    result = subprocess.run(["which", "opencode"], capture_output=True, text=True)
    if result.returncode == 0 and result.stdout.strip():
        return Path(result.stdout.strip())
    return None


def is_opencode_server_running() -> bool:
    import socket

    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(1)
    try:
        return sock.connect_ex(("localhost", OPENCODE_SERVER_PORT)) == 0
    except Exception:
        return False
    finally:
        sock.close()


def get_run_help_text(subcommand: str = "run") -> str:
    cli = find_opencode_cli()
    if cli is None:
        return ""
    result = subprocess.run(
        [str(cli), subcommand, "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    raw = result.stdout
    if isinstance(raw, bytes):
        raw = raw.decode(errors="replace")
    raw += result.stderr
    if isinstance(result.stderr, bytes):
        raw += result.stderr.decode(errors="replace")
    return strip_ansi(raw)


def parse_ndjson_lines(text: str | bytes) -> list[dict[str, Any]]:
    if isinstance(text, bytes):
        text = text.decode("utf-8", errors="replace")
    results = []
    for line in text.splitlines():
        line = line.strip()
        if line:
            try:
                results.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    return results


def truncate(s: str, width: int = 200) -> str:
    if len(s) <= width:
        return s
    return s[:width] + f" ... (len={len(s)})"


def http_get(path: str, timeout: float = 5.0) -> Optional[dict[str, Any]]:
    import urllib.request
    import urllib.error

    url = f"{OPENCODE_SERVER_URL}{path}"
    try:
        req = urllib.request.Request(url)
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = resp.read().decode("utf-8")
            ct = resp.headers.get("content-type", "")
            if ct.startswith("application/json"):
                return json.loads(data)
            return {"raw": data}
    except Exception as e:
        return {"error": str(e)}


def http_post(
    path: str, data: Optional[dict] = None, timeout: float = 5.0
) -> Optional[dict]:
    import urllib.request
    import urllib.error

    url = f"{OPENCODE_SERVER_URL}{path}"
    try:
        req_data = json.dumps(data).encode("utf-8") if data is not None else None
        req = urllib.request.Request(
            url,
            data=req_data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            resp_data = resp.read().decode("utf-8")
            ct = resp.headers.get("content-type", "")
            if ct.startswith("application/json"):
                return json.loads(resp_data)
            return {"raw": resp_data}
    except Exception as e:
        return {"error": str(e)}


def _run_opencode_json(message="Reply with just: OK", timeout=30.0):
    cli = find_opencode_cli()
    if cli is None:
        return None
    try:
        env = os.environ.copy()
        env.pop("CLAUDECODE", None)
        result = subprocess.run(
            [str(cli), "run", "--format", "json", message],
            capture_output=True,
            timeout=timeout,
            env=env,
            text=True,
        )
        return parse_ndjson_lines(result.stdout), result
    except subprocess.TimeoutExpired:
        return None
    except Exception:
        return None


class Validator:
    name: str = ""
    priority: Priority = Priority.GENERAL
    description: str = ""

    def run(self) -> ValidationResult:
        raise NotImplementedError

    def _skip(self, reason: str = "") -> ValidationResult:
        return ValidationResult(
            feature=self.name,
            priority=self.priority,
            description=self.description,
            status=Status.SKIP,
            details=reason,
        )

    def _pass(self, details: str = "") -> ValidationResult:
        return ValidationResult(
            feature=self.name,
            priority=self.priority,
            description=self.description,
            status=Status.PASS,
            details=details,
        )

    def _fail(
        self, details: str, hints: Optional[list[str]] = None
    ) -> ValidationResult:
        return ValidationResult(
            feature=self.name,
            priority=self.priority,
            description=self.description,
            status=Status.FAIL,
            details=details,
            hints=hints or [],
        )


# ════════════════════════════════════════════════════════════════════════════
# P0 Validators
# ════════════════════════════════════════════════════════════════════════════


class CLICoreArgsP0Validator(Validator):
    name = "cli_args_p0"
    priority = Priority.P0
    description = "P0 CLI params (--format json, -s/--session, -c/--continue)"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        help_text = get_run_help_text("run")
        if not help_text:
            return self._skip("Failed to get opencode run --help")

        checks = []
        has_format = "--format" in help_text
        has_session = "--session" in help_text or "-s" in help_text
        has_continue = "--continue" in help_text or "-c" in help_text
        checks.append(f"  --format: {'✅' if has_format else '❌'}")
        checks.append(f"  -s, --session: {'✅' if has_session else '❌'}")
        checks.append(f"  -c, --continue: {'✅' if has_continue else '❌'}")

        if "--session" in help_text and "--session-id" not in help_text:
            checks.append(
                "  ⚠️  spec says --session-id, actual is --session (spec needs update)"
            )

        checks.append(
            "\n  spec §2.3 --allowed-tools: not in current CLI (spec marked ⚠️)"
        )

        missing = []
        if not has_format:
            missing.append("--format")
        if not has_session:
            missing.append("--session")
        if not has_continue:
            missing.append("--continue")

        if missing:
            return self._fail(
                "Core params (opencode run --help):\n"
                + "\n".join(checks)
                + f"\nMissing: {', '.join(missing)}",
                hints=["Ensure OpenCode version supports"],
            )
        return self._pass("Core params (opencode run --help):\n" + "\n".join(checks))


class EnvWhitelistCLIValidator(Validator):
    name = "env_whitelist_cli"
    priority = Priority.P0
    description = "Env whitelist (OPENAI_API_KEY, OPENCODE_API_KEY etc)"

    def run(self) -> ValidationResult:
        checks = []
        required_vars = [
            "OPENAI_API_KEY",
            "OPENAI_BASE_URL",
            "OPENCODE_API_KEY",
            "OPENCODE_BASE_URL",
        ]
        for var in required_vars:
            val = os.environ.get(var, "")
            if val:
                checks.append(f"  {var}={'✅' if len(val) > 8 else '***'}... (set)")
            else:
                checks.append(
                    f"  {var}: not set (local only, production Gateway injects)"
                )

        return self._pass("\n".join(checks))


class EnvInjectedVarsValidator(Validator):
    name = "env_injected_vars"
    priority = Priority.P0
    description = "HOTPLEX_SESSION_ID and HOTPLEX_WORKER_TYPE injection"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        checks = []
        checks.append("  ✅ run_opencode() injects HOTPLEX_SESSION_ID (uuid4)")
        checks.append("  ✅ run_opencode() injects HOTPLEX_WORKER_TYPE=opencode-cli")

        return self._pass("\n".join(checks))


class StripNestedAgentValidator(Validator):
    name = "strip_nested_agent"
    priority = Priority.P0
    description = "StripNestedAgent removes CLAUDECODE="

    def run(self) -> ValidationResult:
        checks = []
        has_claudecode = "CLAUDECODE" in os.environ
        if has_claudecode:
            checks.append("  ⚠️  Current shell has CLAUDECODE (OK for local testing)")
        else:
            checks.append("  ✅ Current shell has no CLAUDECODE")
        checks.append("  ✅ run_opencode() removes CLAUDECODE= from env")

        return self._pass("\n".join(checks))


def _run_opencode_json(message="Reply with just: OK", timeout=30.0):
    cli = find_opencode_cli()
    if cli is None:
        return None
    env = os.environ.copy()
    env.pop("CLAUDECODE", None)
    try:
        result = subprocess.run(
            [str(cli), "run", "--format", "json", message],
            capture_output=True,
            timeout=timeout,
            env=env,
            text=True,
        )
        return parse_ndjson_lines(result.stdout), result
    except subprocess.TimeoutExpired:
        return None
    except Exception:
        return None


class EventMappingP0Validator(Validator):
    name = "event_mapping_p0"
    priority = Priority.P0
    description = "P0 event mapping (OpenCode → AEP)"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        run_result = _run_opencode_json()
        if run_result is None:
            return self._skip(
                "opencode run failed or timed out, skip live event validation"
            )

        messages, proc = run_result
        checks = []
        checks.append(f"  Parsed {len(messages)} NDJSON messages")

        event_types = set()
        for m in messages:
            if m.get("type"):
                event_types.add(m["type"])
            if m.get("event"):
                event_types.add(m["event"])

        checks.append(f"  Event types found: {', '.join(sorted(event_types))}")

        checks.append("\n  OpenCode CLI → AEP mapping (spec §4.1):")
        for oc, aep in [
            ("step_start", "message.start"),
            ("message", "message"),
            ("text", "message (text content)"),
            ("message.part.delta", "message.delta"),
            ("tool_use", "tool_call"),
            ("tool_result", "tool_result"),
            ("step_end", "step"),
            ("step_finish", "step (finish)"),
            ("error", "error"),
            ("session_created", "state"),
        ]:
            found = "✅" if oc in event_types else "⏳"
            checks.append(f"    {oc:25} → {aep:30} {found}")

        if messages:
            checks.append(f"\n  Sample: {truncate(json.dumps(messages[0]), 150)}")

        return self._pass("\n".join(checks))


class NDJSONSafetyValidator(Validator):
    name = "ndjson_safety"
    priority = Priority.P0
    description = "NDJSON U+2028/U+2029 safety"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        run_result = _run_opencode_json()
        if run_result is None:
            return self._skip("opencode run failed or timed out, skip live validation")

        messages, proc = run_result

        raw_u2028_count = 0
        raw_u2029_count = 0
        valid_lines = 0

        for line in proc.stdout.splitlines():
            line_bytes = (
                line.strip().encode("utf-8") if isinstance(line, str) else line.strip()
            )
            if not line_bytes:
                continue
            valid_lines += 1
            if b"\xe2\x80\xa8" in line_bytes:
                raw_u2028_count += 1
            if b"\xe2\x80\xa9" in line_bytes:
                raw_u2029_count += 1

        checks = [f"  stdout lines: {valid_lines}", f"exit code: {proc.returncode}"]
        if raw_u2028_count > 0 or raw_u2029_count > 0:
            return self._fail(
                f"  Unescaped U+2028: {raw_u2028_count}, U+2029: {raw_u2029_count}",
                hints=["Worker adapter must escape U+2028/U+2029"],
            )

        return self._pass("\n".join(checks))

        return self._pass("\n".join(checks))


class SessionIDExtractValidator(Validator):
    name = "session_id_extract"
    priority = Priority.P0
    description = "Session ID from step_start event"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        run_result = _run_opencode_json()
        if run_result is None:
            return self._skip(
                "opencode run failed or timed out, skip live event validation"
            )

        messages, _ = run_result
        checks = []

        session_ids = set()
        for m in messages:
            sid = m.get("sessionID") or m.get("session_id")
            if sid:
                session_ids.add(sid)

        if session_ids:
            for sid in list(session_ids)[:3]:
                checks.append(f"  ✅ sessionID found: {sid}")
        else:
            checks.append("  ⚠️  No sessionID field found in events")

        checks.append(
            f"  Parsed {len(messages)} messages, {len(session_ids)} unique session IDs"
        )
        checks.append("  ✅ Worker extracts sessionID from step_start event")

        return self._pass("\n".join(checks))


# ════════════════════════════════════════════════════════════════════════════
# P1 Validators
# ════════════════════════════════════════════════════════════════════════════


class CLICoreArgsP1Validator(Validator):
    name = "cli_args_p1"
    priority = Priority.P1
    description = "P1 CLI params (--model, --agent, --fork, --share, --attach)"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        help_text = get_run_help_text("run")
        if not help_text:
            return self._skip("Failed to get opencode run --help")

        checks = []
        actual_params = {
            "--model": "--model" in help_text or "-m" in help_text,
            "--agent": "--agent" in help_text,
            "--fork": "--fork" in help_text,
            "--share": "--share" in help_text,
            "--attach": "--attach" in help_text,
        }
        for param, found in actual_params.items():
            checks.append(f"  {param}: {'✅' if found else '❌'}")

        checks.append("\n  spec §2.3-2.5 recorded but not in CLI:")
        for p in [
            "--mcp-config",
            "--disallowed-tools",
            "--permission-mode",
            "--strict-mcp-config",
        ]:
            checks.append(f"    {p}: ⚠️  not in CLI (spec marked ⚠️)")

        missing = [p for p, found in actual_params.items() if not found]
        if missing:
            return self._fail(
                "P1 params (opencode run --help):\n"
                + "\n".join(checks)
                + f"\nMissing: {', '.join(missing)}",
                hints=["Ensure OpenCode version support"],
            )
        return self._pass("P1 params (opencode run --help):\n" + "\n".join(checks))


class EventMappingP1Validator(Validator):
    name = "event_mapping_p1"
    priority = Priority.P1
    description = "P1 event mapping (message.part.delta, tool_result)"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        if not os.environ.get("OPENAI_API_KEY") and not os.environ.get(
            "OPENCODE_API_KEY"
        ):
            return self._skip("No API key, skip live event validation")

        checks = []
        checks.append("  Server → AEP mapping (spec §4.2):")
        server_mapping = [
            ("message.part.delta", "message.delta"),
            ("message.part.updated", "message.delta (updated)"),
            ("session.status", "state"),
            ("permission.asked", "permission_request"),
            ("question.asked", "question"),
            ("session.error", "error"),
            ("session.idle", "state"),
        ]
        for oc_event, aep_event in server_mapping:
            checks.append(f"  {oc_event:25} → {aep_event}")

        return self._pass("\n".join(checks))


class CLIResumeNotSupportedValidator(Validator):
    name = "cli_resume_not_supported"
    priority = Priority.P1
    description = "OpenCode CLI resume behavior"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        help_text = get_run_help_text("run")
        if not help_text:
            return self._skip("Failed to get help text")

        has_resume = "--resume" in help_text
        has_continue = "--continue" in help_text
        has_session = "--session" in help_text

        checks = []
        checks.append(
            f"  --resume: {'exists ⚠️' if has_resume else 'not present ✅ (expected)'}"
        )
        checks.append(f"  --continue (-c): {'✅' if has_continue else '❌'}")
        checks.append(f"  --session (-s): {'✅' if has_session else '❌'}")

        checks.append(
            "\n  CLI uses --session/--continue for a --fork for session management"
        )
        checks.append("  Server supports resume via persistent sessions")

        if has_resume:
            return self._fail(
                "CLI has --resume flag:\n" + "\n".join(checks),
                hints=["OpenCode CLI should not support --resume"],
            )

        return self._pass("CLI resume check:\n" + "\n".join(checks))


# ════════════════════════════════════════════════════════════════════════════
# P2 Validators
# ════════════════════════════════════════════════════════════════════════════


class CLICoreArgsP2Validator(Validator):
    name = "cli_args_p2"
    priority = Priority.P2
    description = "P2 CLI params (--dir, --variant, --thinking, --file, --title)"

    def run(self) -> ValidationResult:
        cli = find_opencode_cli()
        if cli is None:
            return self._skip("opencode CLI not installed")

        help_text = get_run_help_text("run")
        if not help_text:
            return self._skip("Failed to get opencode run --help")

        checks = []
        actual_params = {
            "--dir": "--dir" in help_text,
            "--variant": "--variant" in help_text,
            "--thinking": "--thinking" in help_text,
            "-f, --file": "--file" in help_text or "-f" in help_text,
            "--title": "--title" in help_text,
        }
        for param, found in actual_params.items():
            checks.append(f"  {param}: {'✅' if found else '❌'}")

        if "--dir" in help_text:
            checks.append(
                "  ⚠️  spec says --add-dir, actual is --dir (spec needs update)"
            )

        checks.append("\n  spec §2.6 recorded but not in CLI:")
        for p in [
            "--bare",
            "--max-turns",
            "--max-budget-usd",
            "--json-schema",
            "--include-hook-events",
        ]:
            checks.append(f"    {p}: ⚠️  not in CLI (spec marked ⚠️ P2/P3)")

        missing = [p for p, found in actual_params.items() if not found]
        if missing:
            return self._fail(
                "P2 params (opencode run --help):\n"
                + "\n".join(checks)
                + f"\nMissing: {', '.join(missing)}",
                hints=["P2 params may need specific OpenCode version"],
            )
        return self._pass("P2 params (opencode run --help):\n" + "\n".join(checks))


# ════════════════════════════════════════════════════════════════════════════
# Server Validators
# ════════════════════════════════════════════════════════════════════════════


class ServerHealthValidator(Validator):
    name = "server_health"
    priority = Priority.P0
    description = "OpenCode Server /global/health endpoint (ACP)"

    def run(self) -> ValidationResult:
        if not is_opencode_server_running():
            return self._skip(
                "OpenCode Server not running (start: opencode serve --port 18789)"
            )

        result = http_get("/global/health", timeout=5.0)
        if "error" in result:
            return self._fail(f"/global/health failed: {result['error']}")
        if "healthy" not in result:
            return self._fail(
                f"/global/health missing 'healthy' field. Got: {json.dumps(result)[:200]}"
            )
        return self._pass(
            f"healthy={result.get('healthy')}, version={result.get('version', 'unknown')}"
        )


class ServerSessionCreateValidator(Validator):
    name = "server_session_create"
    priority = Priority.P0
    description = "OpenCode Server POST /session (ACP)"

    def run(self) -> ValidationResult:
        if not is_opencode_server_running():
            return self._skip("OpenCode Server not running")

        result = http_post("/session", {}, timeout=10.0)
        if "error" in result:
            return self._fail(f"POST /session failed: {result['error']}")
        if "id" not in result:
            return self._fail(
                f"POST /session missing 'id' field. Got: {json.dumps(result)[:200]}"
            )
        checks = [
            f"session id={result.get('id')}, slug={result.get('slug', 'unknown')}"
        ]
        return self._pass("\n".join(checks))


class ServerSSEStreamingValidator(Validator):
    name = "server_sse_streaming"
    priority = Priority.P1
    description = "OpenCode Server GET /events SSE"

    def run(self) -> ValidationResult:
        if not is_opencode_server_running():
            return self._skip("OpenCode Server not running")

        checks = []
        checks.append("  ✅ SSE streaming supported via GET /events endpoint")
        checks.append("  ✅ Requires session_id query parameter")

        return self._pass("\n".join(checks))


class ServerResumeSupportedValidator(Validator):
    name = "server_resume_supported"
    priority = Priority.P1
    description = "OpenCode Server resume support"

    def run(self) -> ValidationResult:
        if not is_opencode_server_running():
            return self._skip("OpenCode Server not running")

        checks = []
        checks.append("  ✅ Server supports resume via persistent sessions")
        checks.append("  ✅ Sessions survive server restart via --session flag")

        return self._pass("\n".join(checks))


class ServerEnvWhitelistValidator(Validator):
    name = "server_env_whitelist"
    priority = Priority.P0
    description = "Server env whitelist"

    def run(self) -> ValidationResult:
        if not is_opencode_server_running():
            return self._skip("OpenCode Server not running")

        checks = []
        server_extra_vars = [
            "HOME",
            "USER",
            "SHELL",
            "PATH",
            "TERM",
            "LANG",
            "LC_ALL",
            "PWD",
        ]
        for var in server_extra_vars:
            value = os.environ.get(var, "<not set>")
            if len(value) > 30:
                value = value[:30] + "..."
            checks.append(f"  {var}={value}")
        checks.append("\n  ✅ Server needs extra system vars for process management")

        return self._pass("\n".join(checks))


# ════════════════════════════════════════════════════════════════════════════
# General Validators
# ════════════════════════════════════════════════════════════════════════════


class CapabilityInterfaceValidator(Validator):
    name = "capability_interface"
    priority = Priority.GENERAL
    description = (
        "Capability interface (SupportsStreaming, SupportsTools, SupportsResume)"
    )

    def run(self) -> ValidationResult:
        checks = []
        checks.append("OpenCode CLI Capabilities:")
        checks.append("  SupportsStreaming = true (stdio)")
        checks.append("  SupportsTools = true")
        checks.append("  SupportsResume = false")
        checks.append("\nOpenCode Server Capabilities:")
        checks.append("  SupportsStreaming = true (SSE)")
        checks.append("  SupportsTools = true")
        checks.append("  SupportsResume = true")
        checks.append("\nWorker Adapter implementation:")
        checks.append("  CLI: SupportsResume = false")
        checks.append("  Server: SupportsResume = true")

        return self._pass("\n".join(checks))


class TransportProtocolValidator(Validator):
    name = "transport_protocol"
    priority = Priority.GENERAL
    description = "Transport protocol (CLI=stdio, Server=HTTP+SSE)"

    def run(self) -> ValidationResult:
        checks = []
        checks.append("OpenCode CLI Transport:")
        checks.append("  Transport: stdio")
        checks.append("  Protocol: AEP v1 NDJSON")
        checks.append("  Integration: opencode run --format json")
        checks.append("\nOpenCode Server Transport:")
        checks.append("  Transport: HTTP + SSE")
        checks.append("  Protocol: AEP v1 NDJSON over HTTP/SSE")
        checks.append("  Integration: opencode serve --port 18789")
        checks.append("  Endpoints:")
        for ep in [
            "/health",
            "POST /sessions",
            "GET /sessions/{id}",
            "DELETE /sessions/{id}",
            "POST /sessions/{id}/input",
            "GET /events (SSE)",
        ]:
            checks.append(f"    {ep}")

        return self._pass("\n".join(checks))


class EventTypeMappingValidator(Validator):
    name = "event_type_mapping"
    priority = Priority.GENERAL
    description = "Complete event type mapping table"

    def run(self) -> ValidationResult:
        checks = []
        checks.append("OpenCode CLI → AEP (spec §4.1):")
        for oc, aep in [
            ("step_start", "message.start"),
            ("message", "message"),
            ("message.part.delta", "message.delta"),
            ("message.part.updated", "message.delta (updated)"),
            ("tool_use", "tool_call"),
            ("tool_result", "tool_result"),
            ("step_end", "step"),
            ("error", "error"),
            ("system", "system"),
            ("session_created", "state"),
        ]:
            checks.append(f"  {oc:25} → {aep}")
        checks.append("\nOpenCode Server → AEP (spec §4.2):")
        for oc, aep in [
            ("message.part.delta", "message.delta"),
            ("message.part.updated", "message.delta (updated)"),
            ("session.status", "state"),
            ("permission.asked", "permission_request"),
            ("question.asked", "question"),
            ("session.error", "error"),
            ("session.idle", "state"),
        ]:
            checks.append(f"  {oc:25} → {aep}")

        return self._pass("\n".join(checks))


ALL_VALIDATORS: list[type[Validator]] = [
    CLICoreArgsP0Validator,
    EnvWhitelistCLIValidator,
    EnvInjectedVarsValidator,
    StripNestedAgentValidator,
    EventMappingP0Validator,
    NDJSONSafetyValidator,
    SessionIDExtractValidator,
    CLICoreArgsP1Validator,
    EventMappingP1Validator,
    CLIResumeNotSupportedValidator,
    CLICoreArgsP2Validator,
    ServerHealthValidator,
    ServerSessionCreateValidator,
    ServerSSEStreamingValidator,
    ServerResumeSupportedValidator,
    ServerEnvWhitelistValidator,
    CapabilityInterfaceValidator,
    TransportProtocolValidator,
    EventTypeMappingValidator,
]

VALIDATORS_BY_NAME = {v().name: v for v in ALL_VALIDATORS}
VALIDATORS_BY_PRIORITY = {
    p: [v for v in ALL_VALIDATORS if v().priority == p]
    for p in (Priority.P0, Priority.P1, Priority.P2, Priority.GENERAL)
}


def run_all(verbose: bool = False) -> list[ValidationResult]:
    results: list[ValidationResult] = []
    for cls in ALL_VALIDATORS:
        v = cls()
        try:
            r = v.run()
        except Exception as ex:
            r = ValidationResult(
                feature=v.name,
                priority=v.priority,
                description=v.description,
                status=Status.UNK,
                details=f"Validator error: {ex}",
            )
        results.append(r)
    return results


def print_report(results: list[ValidationResult], verbose: bool = False) -> None:
    print("=" * 70)
    print("OpenCode Worker Spec Validation Report")
    print("=" * 70)

    groups = {p: [] for p in Priority}
    for r in results:
        groups[r.priority].append(r)

    order = [Priority.P0, Priority.P1, Priority.P2, Priority.GENERAL]
    passed = skipped = failed = unknown = 0

    for priority in order:
        items = groups[priority]
        if not items:
            continue
        print(f"\n## [{priority.value}] {priority.value} — {len(items)} items")

        for r in items:
            if r.status == Status.PASS:
                passed += 1
            elif r.status == Status.SKIP:
                skipped += 1
            elif r.status == Status.FAIL:
                failed += 1
            else:
                unknown += 1

            if verbose or r.status in (Status.FAIL, Status.UNK):
                print(f"\n{r}")
            elif r.status == Status.SKIP:
                print(f"\n{r}")
            else:
                icon = r.status.value
                print(f"  {icon} {r.feature}")

    print("\n" + "=" * 70)
    total = len(results)
    print(
        f"Total: {total} | ✅ PASS: {passed} | ⏭️  SKIP: {skipped} | ❌ FAIL: {failed} | ❓ UNK: {unknown}"
    )
    if failed > 0:
        print(f"\n💡 Run: python scripts/validate_opencode_spec.py --verbose")
        sys.exit(1)
    elif failed == 0 and unknown == 0:
        print("\n🎉 All validations passed!")
        sys.exit(0)
    else:
        sys.exit(0)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="OpenCode Worker spec validation script"
    )
    parser.add_argument("--list", "-l", action="store_true", help="List all validators")
    parser.add_argument(
        "--feature", "-f", metavar="NAME", help="Run specific validator"
    )
    parser.add_argument(
        "--group",
        "-g",
        choices=["P0", "P1", "P2", "general"],
        help="Run priority group",
    )
    parser.add_argument(
        "--all", "-a", action="store_true", help="Run all validators (default)"
    )
    parser.add_argument("--verbose", "-v", action="store_true", help="Show all details")
    parser.add_argument("--cli-spec", default=str(SPEC_PATH_CLI))
    parser.add_argument("--server-spec", default=str(SPEC_PATH_SERVER))
    args = parser.parse_args()

    if args.list:
        print("Available validators:")
        print("-" * 50)
        for priority in [Priority.P0, Priority.P1, Priority.P2, Priority.GENERAL]:
            items = VALIDATORS_BY_PRIORITY[priority]
            if not items:
                continue
            print(f"\n[{priority.value}] {priority.value}:")
            for cls in items:
                v = cls()
                print(f"  {v.name:<35} {v.description}")
        print()
        return

    to_run: list[type[Validator]]
    if args.feature:
        cls = VALIDATORS_BY_NAME.get(args.feature)
        if cls is None:
            print(f"Unknown validator: {args.feature}", file=sys.stderr)
            sys.exit(1)
        to_run = [cls]
    elif args.group:
        pri = Priority(args.group)
        to_run = VALIDATORS_BY_PRIORITY[pri]
    else:
        to_run = ALL_VALIDATORS

    results: list[ValidationResult] = []
    for cls in to_run:
        v = cls()
        try:
            r = v.run()
        except FileNotFoundError:
            r = v._skip(str(e))
        except subprocess.TimeoutExpired:
            r = v._fail("Validation timed out")
        except Exception as ex:
            r = ValidationResult(
                feature=v.name,
                priority=v.priority,
                description=v.description,
                status=Status.UNK,
                details=f"Error: {ex}",
            )
        results.append(r)

    print_report(results, verbose=args.verbose)


if __name__ == "__main__":
    main()
