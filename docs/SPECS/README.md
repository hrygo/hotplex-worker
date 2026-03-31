# SPECS 目录索引

> 规范文档集中管理

## 文档列表

| 文件 | 描述 | 版本 | 状态 |
|------|------|------|------|
| [Acceptance-Criteria.md](./Acceptance-Criteria.md) | 157 条验收标准完整定义 | v1.0 | 草稿 |
| [TRACEABILITY-MATRIX.md](./TRACEABILITY-MATRIX.md) | HotPlex Worker 功能实现与代码溯源矩阵，157 条 AC 与代码对齐状态评估 | v1.0 | 活动 |
| [AC-Tracking-Matrix.csv](./AC-Tracking-Matrix.csv) | 验收状态跟踪矩阵（CSV，机器可读） | v1.0 | 草稿 |

## 快速查询

### 按优先级查看待办
```bash
# 查看所有 P0 AC
grep ",P0,TODO," docs/SPECS/AC-Tracking-Matrix.csv

# 查看某区域的 P0
grep "AEP v1 协议,P0,TODO" docs/SPECS/AC-Tracking-Matrix.csv

# 统计进度
grep -c ",P0,PASS," docs/SPECS/AC-Tracking-Matrix.csv
grep -c ",P0,TODO," docs/SPECS/AC-Tracking-Matrix.csv
```

### 状态分布
```bash
# TODO / IN_PROGRESS / PASS / FAIL 数量
awk -F',' '{print $5}' docs/SPECS/AC-Tracking-Matrix.csv | sort | uniq -c
```

## 关联规范文档

- **协议规范**: `../architecture/AEP-v1-Protocol.md`, `../architecture/AEP-v1-Appendix.md`
- **架构设计**: `../architecture/Worker-Gateway-Design.md`, `../architecture/Message-Persistence.md`
- **安全设计**: `../security/Security-Authentication.md`, `../security/SSRF-Protection.md`, `../security/Env-Whitelist-Strategy.md`, `../security/AI-Tool-Policy.md`, `../security/Security-InputValidation.md`
- **管理设计**: `../management/Admin-API-Design.md`, `../management/Config-Management.md`, `../management/Observability-Design.md`, `../management/Resource-Management.md`
- **测试策略**: `../testing/Testing-Strategy.md`

## 更新日志

| 日期 | 版本 | 更新内容 |
|------|------|----------|
| 2026-03-31 | v1.0 | 初始版本：157 条 AC，3 个文件（MD 定义 + MD 跟踪 + CSV 跟踪） |
