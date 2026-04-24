# HotPlex Worker Gateway - Disaster Recovery Plan

> **Purpose**: Restore HotPlex Worker Gateway service after failure or data loss.
>
> **Last Updated**: 2026-04-21
> **Version**: 1.0.0

---

## 1. Recovery Time Objectives (RTO)

| Scenario | RTO | RPO |
|----------|-----|-----|
| Process crash | < 1 minute | 0 (auto-restart) |
| Host failure | < 5 minutes | < 1 hour (backup) |
| Data corruption | < 15 minutes | < 1 hour (backup) |
| Full disaster | < 1 hour | < 24 hours |

---

## 2. Backup Strategy

### 2.1 Automated Backups

Docker Compose includes automatic hourly backups:

```yaml
# docker-compose.yml
services:
  backup:
    image: alpine:3.21
    volumes:
      - hotplex-data:/data:ro
      - backup-storage:/backups
    retention: 30 days
```

**Backup location**: `/var/lib/docker/volumes/backup-storage/_data/`

### 2.2 Manual Backup

```bash
# Stop service first (recommended for consistency)
systemctl stop hotplex
# OR
docker-compose stop gateway

# Backup database
cp /var/lib/hotplex/hotplex.db /backups/hotplex-backup-$(date +%Y%m%d-%H%M%S).db

# Restart service
systemctl start hotplex
# OR
docker-compose up -d gateway
```

### 2.3 Backup Verification

```bash
# Check latest backup integrity
LATEST=$(ls -t /backups/*.db | head -1)
sqlite3 "$LATEST" "PRAGMA integrity_check;"

# Expected output: ok
```

---

## 3. Recovery Procedures

### 3.1 Process Crash (Automatic)

**Symptoms**: Service not responding, health check fails

**Automatic Recovery**:
- systemd: Auto-restart on failure (RestartSec=5s)
- Docker: Restart policy `unless-stopped`
- **LLM Rate Limit Auto-Retry**: If the worker crashes due to a temporary error (429 rate limit, 529 overload, network error), the gateway automatically retries with exponential backoff — no manual intervention needed. See [[management/Config-Reference]] for configuration.

**Manual Intervention** (if auto-restart fails):
```bash
# Check status
systemctl status hotplex
# OR
docker-compose ps

# View logs
journalctl -u hotplex -n 50
# OR
docker-compose logs --tail=50 gateway

# Manual restart
systemctl restart hotplex
# OR
docker-compose restart gateway
```

---

### 3.2 Database Corruption

**Symptoms**:
- SQLite errors in logs: `database disk image is malformed`
- Health check returns `degraded` or `unhealthy`
- Sessions not persisting

**Recovery Steps**:

```bash
# 1. Stop the service
systemctl stop hotplex
# OR
docker-compose stop gateway

# 2. Verify corruption
sqlite3 /var/lib/hotplex/hotplex.db "PRAGMA integrity_check;"
# Expected: "Error: database disk image is malformed"

# 3. Find latest valid backup
LATEST=$(ls -t /backups/*.db | head -1)
echo "Restoring from: $LATEST"

# 4. Verify backup integrity
sqlite3 "$LATEST" "PRAGMA integrity_check;"
# Must return: "ok"

# 5. Backup current (corrupted) database for analysis
mv /var/lib/hotplex/hotplex.db /var/lib/hotplex/hotplex.db.corrupted.$(date +%Y%m%d)

# 6. Restore from backup
cp "$LATEST" /var/lib/hotplex/hotplex.db

# 7. Set correct permissions
chown hotplex:hotplex /var/lib/hotplex/hotplex.db
chmod 644 /var/lib/hotplex/hotplex.db

# 8. Start service
systemctl start hotplex
# OR
docker-compose up -d gateway

# 9. Verify recovery
curl http://localhost:9999/admin/health
# Expected: {"status": "healthy", ...}
```

---

### 3.3 Secrets Loss/Rotation

**Symptoms**: JWT validation failures, admin API authentication failures

**Recovery Steps**:

```bash
# 1. Generate new JWT secret
NEW_JWT_SECRET=$(openssl rand -base64 32)

# 2. Generate new admin tokens
NEW_ADMIN_TOKEN_1=$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)
NEW_ADMIN_TOKEN_2=$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)

# 3. Update secrets file
cat > /etc/hotplex/secrets.env <<EOF
export HOTPLEX_JWT_SECRET="$NEW_JWT_SECRET"
export HOTPLEX_ADMIN_TOKEN_1="$NEW_ADMIN_TOKEN_1"
export HOTPLEX_ADMIN_TOKEN_2="$NEW_ADMIN_TOKEN_2"
EOF

chmod 600 /etc/hotplex/secrets.env

# 4. For Docker, update environment and restart
docker-compose down
docker-compose up -d

# 5. For systemd, restart service
systemctl restart hotplex

# 6. Update all clients with new admin tokens
# 7. Note: Existing JWT tokens will be invalidated
```

---

### 3.4 Full Host Recovery

**Scenario**: Complete host failure, need to rebuild from scratch

**Prerequisites**:
- Backup file available
- Source code access
- Original secrets (if available)

**Recovery Steps**:

```bash
# 1. Install dependencies
# Ubuntu/Debian
apt-get update && apt-get install -y golang git openssl curl docker.io docker-compose

# RHEL/CentOS
yum install -y golang git openssl curl docker docker-compose

# macOS
brew install go git openssl

# 2. Clone repository
git clone <repository-url> hotplex
cd hotplex

# 3. Restore secrets (if available)
cp /path/to/backup/secrets.env /etc/hotplex/secrets.env
source /etc/hotplex/secrets.env

# 4. Restore database
mkdir -p /var/lib/hotplex/data
cp /path/to/backup/hotplex.db /var/lib/hotplex/data/
chown -R hotplex:hotplex /var/lib/hotplex

# 5. Build and install
./scripts/install.sh --non-interactive

# 6. Restore config
cp /path/to/backup/config.yaml /etc/hotplex/config.yaml

# 7. Start service
systemctl start hotplex
# OR
docker-compose up -d

# 8. Verify
curl http://localhost:9999/admin/health
```

---

## 4. Emergency Contacts

| Role | Contact | Escalation |
|------|---------|------------|
| On-Call Engineer | oncall@hotplex.dev | PagerDuty |
| Platform Lead | platform-lead@hotplex.dev | Slack |
| Security Team | security@hotplex.dev | Email |

---

## 5. Post-Incident Checklist

After recovery:

- [ ] Verify all sessions are accessible
- [ ] Check database integrity: `sqlite3 hotplex.db "PRAGMA integrity_check;"`
- [ ] Review logs for errors: `journalctl -u hotplex -n 100`
- [ ] Check LLM auto-retry stats in logs: `grep "llm_retry" logs/*.log`
- [ ] Verify auto-retry config is correct if rate limit errors were involved
- [ ] Update monitoring alerts if needed
- [ ] Document root cause in incident report
- [ ] Schedule post-mortem review
- [ ] Update this DR plan if gaps identified

---

## 6. Testing Schedule

| Test Type | Frequency | Last Tested | Next Scheduled |
|-----------|-----------|-------------|----------------|
| Backup restoration | Monthly | - | 2026-05-01 |
| Full disaster recovery | Quarterly | - | 2026-07-01 |
| Secret rotation | Monthly | - | 2026-05-01 |

---

## 7. Appendix: Useful Commands

### Health Check
```bash
curl http://localhost:9999/admin/health | jq
```

### Session List
```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/sessions | jq
```

### Database Stats
```bash
sqlite3 /var/lib/hotplex/hotplex.db <<EOF
SELECT 'sessions' as table_name, COUNT(*) as row_count FROM sessions
UNION ALL
SELECT 'audit_log', COUNT(*) FROM audit_log;
EOF
```

### Backup List
```bash
ls -lh /backups/*.db | tail -10
```

### Log Analysis
```bash
# Error count last 24 hours
journalctl -u hotplex --since "24 hours ago" | grep -c "ERROR"

# Recent warnings
journalctl -u hotplex -p warning -n 50
```

---

## 8. Version History

| Version | Date | Changes | Author |
|---------|------|---------|--------|
| 1.0.0 | 2026-04-02 | Initial version | HotPlex Team |
| 1.0.1 | 2026-04-21 | Add LLM auto-retry to crash recovery; update recovery checklist | HotPlex Team |
