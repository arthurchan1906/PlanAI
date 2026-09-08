# 行为三维度基线(B11/B10) — 2026-09-08

> 归属:task-20260814-175921-4ff2f9(B10/B11) · 前置路2 论证(docs/BEHAVIOR_ROUTE2_ARGUMENT.md)
> 命令:`aipmc metrics --behavior [--since <iso>] [--json]` · 只读、一次性报告(非持续服务)
> 目的:给「行为侧路2 A/B」做前置的可复算信号 + 三维度行为基线,替代需人工 D1 双标的自发率

---

## 数字(全量)

| 维度 | 数值 | 口径 |
|---|---|---|
| 解析覆盖率(B10) | **98.0%** | 能将 `role='tool'` 行解析成 `{ts,session,agent,tool,status}` 的比例(≥90% 达标) |
| 会话 / 调用 | 25 / 950 | `#aipm_*` MCP 调用(排除 🔧/👁 原生工具行) |
| **历史检索意识** | **70.5%**(670/950) | 检索类 aipm_* 调用占比(search/get/read/briefing/trace) |
| **计划性** | **48.0%**(12/25) | 用过 aipm plan/task 工具的会话占比 |
| **盲试检测** | **2.9%**(13/455) | `[MCP]` 日志 `status=ERR` 占比(代理信号,见下) |

**后窗口 8/29-9/3**:**检索 52.6%(175/333)、计划 66.7%(4/6)、覆盖 99.1%、会话 6/调用 333**——与 §0 M 线窗口对齐。

## 方法与口径

- **B10 结构化事件流**:解析 `discussion_log role='tool'` 行的 tool 记号,兼容真实格式
  `🛠 mcp__aipm__aipm_<tool>`(claude-code)与 `📡 aipm_<tool>`;非 aipm 工具(🔧/👁/📝/🆕)排除。
  字段 `{ts, session_id, agent, tool, status}`。
- **B11 三维度**:
  - 历史检索意识 = 检索类调用 / 全部 aipm 调用(检索类含 search/get/read/briefing/trace 等)。
  - 计划性 = 使用过 plan/task 工具的去重会话 / 总会话(按 session 去重,非调用计数)。
  - 盲试检测 = `[MCP]` 日志 `status=ERR` / aipm 调用(代理信号;discussion_log 不带工具状态)。

## 口径边界(引用必读)

1. **盲试检测与其余两维不同源**:检索意识/计划性来自 `discussion_log`(有 session,可复算);
   盲试来自 `~/.aipmc/logs/aipmc.log` `[MCP]` 行(无 session,只到当前日志文件,约 8/29 起)。
   **勿将三维合并成单一行为分**。
2. **非因果**:本基线是方向性描述(同 9/2 路1 口径),不构成「行为 → 结果」证明;
   引用不得当作改善证据(模板 §0)。
3. **排除 auto**:用该基线做验收时必须只取人工 `review_status`,排除 `auto`(9/2 已实锤 auto 章假象)。
4. **未含持久化**:`--behavior` 不落库(只读),需留档请自行重跑或 `--json` 保存。

## 与路2 的关系

本基线是路2 A/B(同 agent skill 开/关)的**可复算前置信号**;先在无干预下记录此基线,
A/B 后再跑同一命令比对 delta,即可在「排除 auto、分列双目标」口径下检验强制净增量。
