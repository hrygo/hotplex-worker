# 进度文件模式与成功指标

## 进度文件 Schema

```json
{
  "version": 2,
  "last_updated": "2026-04-29T21:36:00+08:00",
  "total_cycles": 0,
  "modules": {
    "internal/gateway": {
      "analysis_count": 0,
      "aspects_covered": [],
      "aspects_pending": [
        "solid", "dry", "coupling",
        "error-handling", "concurrency", "resource-mgmt",
        "performance", "scalability",
        "security", "observability", "testability", "code-quality"
      ],
      "issues_created": [],
      "findings_total": 0,
      "findings_dropped": [],
      "last_analyzed": null
    }
  },
  "issues": [],
  "audited_issues": [],
  "recent_activity": []
}
```

`findings_dropped` 数组跟踪分诊期间丢弃的低置信度或低 ROI 发现，以及去重检查中判为 duplicate 的发现。每个条目：`{"finding": "name", "reason": "Low confidence — may be intentional"}` 或 `{"finding": "name", "reason": "Duplicate of issue N — same file/location overlap"}`。

`audited_issues` 数组跟踪已审计的 issue，避免重复审计。每个条目：`{"number": 265, "verdict": "valid", "action": "confirmed", "audited_at": "2026-05-07T08:00:00+08:00"}`。

---

## 边缘情况处理

### 无发现

**场景**：分析完成后没有产生可操作的结果

**推荐处理**：更新进度（将方面标记为已覆盖）并跳过 issue 创建。在进度中注明"无重要发现"。

**为什么这是好事**：说明模块在这方面很健康。不需要强制创建 issue。

### 跨模块发现

**场景**：分析揭示跨越多个模块的问题

**推荐处理**：在 issue 中注明所有受影响的模块，但标记主模块。添加 `cross-cutting` 注释以便后续跟踪。

**为什么标记主模块**：保持进度文件简单，同时确保问题不会在裂缝中遗漏。

### 模块太大（>1500 行）

**场景**：单个模块过大，难以在一次分析中覆盖

**推荐处理**：将拆分为子模块进行分析（例如，`gateway` → `gateway/core` + `gateway/bridge` + `gateway/api`）。独立跟踪子模块。

**为什么拆分**：大型模块的分析质量下降。拆分后可以更深入地关注每个部分。

### 重新分析

**场景**：模块的所有方面都已覆盖，需要重新分析

**推荐处理**：重新分析会更深入 — 查找首次通过中遗漏的问题，检查以前的发现是否已解决，识别新模式。

**为什么重新分析有价值**：代码库在演化，以前的发现可能已解决，新问题可能出现。重新分析是健康检查。

---

## 成功指标

如何判断架构分析是否有效？

### 好的信号

- **Issue 创建率**：60-80% 的分析周期创建 issue（不过多，也不过少）
- **Issue 解决率**：创建的 issue 在 2-4 周内解决或纳入路线图
- **Issue 有效率**：审计发现 ≥70% 的 issue 仍然 valid（低误报率）
- **审计回收率**：每轮审计关闭 10-30% 的存量 issue（保持 backlog 健康）
- **去重拦截率**：10-25% 的候选发现被步骤 4.8 拦截（说明去重机制在正常工作）
- **发现质量**：审查者反馈"有用的发现"、"可操作的问题"
- **重复分析价值**：第二次分析发现新问题或确认旧问题已解决
- **覆盖进展**：覆盖矩阵显示持续进展，没有模块被遗漏

### 需要调整的信号

- **太多低质量 issue**：收紧置信度 + ROI 分诊阈值
- **太少 issue**：放宽分诊阈值，或检查是否过于保守
- **重复的误报**：提高置信度要求，添加更多上下文检查
- **重复创建 issue**：加强步骤 4.8 的去重检查，降低重叠判定阈值
- **模块分析卡住**：检查进度文件，可能需要手动调整模块边界
- **Issue 长期未解决**：重新评估 ROI 评估，可能创建的优先级错误
- **审计关闭率过高**（>50%）：分析质量有问题，需要提高分诊标准
- **审计关闭率过低**（<10%）：阈值过高，或审计过于保守
- **去重拦截率过高**（>40%）：说明分析没有深入，总在同一问题上打转

### 持续改进

**定期回顾**（建议每 2-4 周）：
- 检查创建的 issue 的状态
- 收集审查者和实施者的反馈
- 调整分诊阈值和方面选择逻辑
- 更新模块边界和分组

**为什么需要持续改进**：架构分析不是一劳永逸的过程。代码库演化，团队标准变化，分析技能应该随之调整。
