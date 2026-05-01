---
name: hotplex-update
description: HotPlex 二进制更新、发布和服务重启标准化流程。构建、安装、服务重启、验证，完整错误处理和回滚机制。**使用此 skill**：更新 HotPlex、安装新版本、重启服务、回滚版本、服务升级。支持用户级和系统级服务，跨平台兼容（Linux/macOS/Windows）。
---

# HotPlex Update & Service Restart Workflow

## Overview

This skill provides a **standardized, error-safe workflow** for updating HotPlex to a new binary version and restarting the service. It ensures the latest compiled code is deployed with minimal downtime.

## Prerequisites

- `hotplex` CLI installed and configured
- `make` and `go` 1.26+ installed
- Systemd user-level service enabled (`hotplex service install --level user`)
- Write permissions to `/home/hotplex/.local/bin/`

## When to Use This Skill

Invoke this skill when:
- User says "install new version", "update binary", "deploy latest code"
- User says "restart service with new changes"
- After building new code with `make build`
- After pulling latest changes from git
- **Any scenario involving binary updates and service restart**

## Workflow Steps

### Step 1: Build New Binary

Compile the latest source code:

```bash
make build
```

**Expected Output:**
```
Building...
  ✓ bin/hotplex-linux-amd64
```

**Error Handling:**
- If build fails: Fix compilation errors first, then retry
- Check for: `go build` errors, missing dependencies, syntax issues

---

### Step 2: Verify Binary Timestamp

Confirm the new binary was just built:

```bash
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**Expected Output:**
- `./bin/hotplex-linux-amd64`: Recent timestamp (just now)
- `/home/hotplex/.local/bin/hotplex`: Older timestamp (previous version)

**What This Tells Us:**
- Confirms we have a newer binary to deploy
- Shows the version gap we're about to close

---

### Step 3: Stop Service

**CRITICAL:** Must stop service before replacing binary to avoid "Text file busy" error.

```bash
hotplex service stop
```

**Expected Output:**
```
✓ Stopped service (user)
```

**Error Handling:**
- If service not running: Proceed to next step (idempotent)
- If service fails to stop: Check `hotplex service status` and `journalctl --user -u hotplex`

**Wait for Cleanup:**
```bash
sleep 2
```

**Why Wait:** Systemd may take 1-2 seconds to fully release the binary file locks.

---

### Step 4: Replace Binary

Copy the newly built binary to system location:

```bash
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**Expected Output:** (silent on success)

**Error Handling:**
- If `Text file busy`: Service didn't stop fully. Go back to Step 3 and wait longer.
- If `Permission denied`: Check write permissions to `/home/hotplex/.local/bin/`

**Verify Replacement:**
```bash
ls -lh /home/hotplex/.local/bin/hotplex
```

**Confirm:** Timestamp should be recent (just now), not the old timestamp.

---

### Step 5: Start Service

Start the service with the new binary:

```bash
hotplex service start
```

**Expected Output:**
```
✓ Service started (user)
```

**Error Handling:**
- If service fails to start: Check logs with `hotplex service logs`
- Common issues: Port conflicts (8888/9999), config errors, missing dependencies

---

### Step 6: Verify Service Status

Confirm service is running with the new binary:

```bash
hotplex service status
```

**Expected Output:**
```
✓ hotplex (user) active
    PID: <new PID>
    Unit: /home/hotplex/.config/systemd/user/hotplex.service
```

**Key Indicators:**
- Status: `active` (not `failed` or `inactive`)
- PID: Different from pre-update PID (confirms restart)
- Unit: Correct systemd user service path

---

### Step 7: Verify Service Health

Check service logs to ensure clean startup:

```bash
sleep 2 && hotplex service logs | tail -20
```

**Expected Output:**
```
HOTPLEX GATEWAY
Unified AI Coding Agent Access Layer
────────────────────────────────────────────────────────────
Version    v1.3.0
Gateway    http://:8888
Adapters   feishu ✓  slack ✗
{"time":"...","level":"INFO","msg":"feishu: starting WebSocket connection"...}
```

**Success Indicators:**
- ✅ Banner displays correctly
- ✅ No error messages in last 20 lines
- ✅ Feishu adapter shows "connected" (or "starting")
- ✅ Gateway listening on port 8888

**Error Indicators:**
- ❌ "panic", "fatal", "error" in logs
- ❌ Adapter connection failures
- ❌ Port binding errors

**On Errors:**
- Check full logs: `hotplex service logs -n 100`
- Check system journal: `journalctl --user -u hotplex -n 50`
- Rollback: Keep old binary backup before replacing

---

### Step 8: Functional Verification (Optional but Recommended)

If the update includes specific new features, verify them:

**For Security Policy Updates:**
```bash
# Test cd command in Feishu
/cd ~/.hotplex/workspace/hotplex
# Should succeed with new security policy
```

**For Error Message Updates:**
```bash
# Test invalid directory in Feishu
/cd /etc/myapp
# Should show detailed error message
```

---

## Rollback Procedure (If Update Fails)

If the new binary has issues, roll back to the previous version:

### 1. Stop Service
```bash
hotplex service stop
```

### 2. Restore Previous Binary
```bash
# If you have a backup:
cp /path/to/backup/hotplex /home/hotplex/.local/bin/hotplex

# Or rebuild from previous commit:
git checkout <previous-commit>
make build
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

### 3. Restart Service
```bash
hotplex service start
```

### 4. Verify Rollback
```bash
hotplex service status
hotplex service logs | tail -20
```

---

## Best Practices

### 1. Backup Before Replace
```bash
cp /home/hotplex/.local/bin/hotplex /tmp/hotplex.backup.$(date +%s)
```

### 2. Use `cp -f` Force Flag
Prevents "Text file busy" errors when service didn't fully stop.

### 3. Always Wait After Stop
The `sleep 2` after `service stop` prevents file lock issues.

### 4. Verify Timestamps
Comparing timestamps confirms you're deploying the right version.

### 5. Check Logs After Start
Don't assume success—verify clean startup in logs.

---

## Troubleshooting

### Issue: "Text file busy" when copying binary
**Cause:** Service still running or file locks not released
**Solution:**
```bash
hotplex service stop
sleep 3  # Wait longer
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

### Issue: Service fails to start after update
**Cause:** New binary has runtime errors
**Solution:** Check logs, rollback to previous version, fix issue, rebuild

### Issue: Old version still running after update
**Cause:** Binary replacement failed or systemd cached old binary
**Solution:**
```bash
# Verify binary timestamp
ls -lh /home/hotplex/.local/bin/hotplex

# If timestamp is old, repeat Step 4
# If timestamp is new but old PID, restart systemd
systemctl --user daemon-reload
hotplex service restart
```

### Issue: New features not working
**Cause:** Service not fully restarted or config not loaded
**Solution:**
```bash
# Full restart (not just start)
hotplex service restart

# Verify config loaded
hotplex service logs | grep "security\|allowed"
```

---

## Quick Reference Command Sequence

For experienced users, the complete workflow:

```bash
# Build
make build

# Verify
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# Stop and wait
hotplex service stop
sleep 2

# Replace
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# Start
hotplex service start

# Verify
hotplex service status
sleep 2 && hotplex service logs | tail -20
```

---

## Notes

- **Downtime:** Typically 3-5 seconds (stop + replace + start)
- **Impact:** All active sessions are terminated during restart
- **Safety:** Always backup previous binary before replacing
- **Logging:** All service operations are logged to systemd journal
- **User-Level:** Uses systemd user service, no root required
