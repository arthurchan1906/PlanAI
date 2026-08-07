# 反馈镜子快照设计稿 v1（讨论稿，未实施）

> 关联：`idea-20260807-110145-5d428d`「Agent 反馈镜子 — Q5 评估方案」
> 状态：设计讨论稿 v1（8/7）。消费者 = 修改 AIPM 的 Agent；不自动、不引入 cron/pipeline。
> 触发方式（8/7 共识）：`aipmc snapshot` 手动命令 + **按时间窗口回算**（`--since/--until` 从现有日志/DB 计算历史窗口），解决"修复前基线事后可补"。

## 1. 目标与边界

- 目的：修完一轮代码 → 跑快照 → 看 delta → 判断这轮修复是否有效（Q5 即时反馈）
- 非目标：长期自动趋势（v3 再说）、实时监控、项目维度拆分（v2，需补日志字段）
- 数据源：`~/.aipmc/logs/aipmc.log`（[LLM]/[INJECT] 行）+ 当前项目 pmai.db（session_summaries/events/discussion_log）
- 粒度：agent 维度（claude/codex/cursor/opencode）+ 总量

## 2. 指标清单 v1

### 2.1 消耗类（来源 [LLM] 日志）
| 指标 | 计算 | 方向 |
|------|------|:--:|
| calls | 窗口内 [LLM] 行数 | — |
| in_tok / out_tok | 各 agent 求和 | ↓ |
| avg_lat / p95_lat | 各 agent 均值/分位 | ↓ |
| cache_hit_rate | cache_hit/(cache_hit+cache_create)，仅统计含 cache_hit 的行（87.7% 覆盖，responses 路径缺口记录在备注） | ↑ |
| injected_rate | injected=Y 占比 | ↑ |

### 2.2 质量类（来源 DB）
| 指标 | 计算 | 方向 |
|------|------|:--:|
| l2_coverage | session_summaries 有摘要数 / 总数 | ↑（≥85%） |
| l2_nested_goal | summary 列嵌套 goal 数（B2 口径） | ↓（=0） |
| event_processed_rate | events processed_by_agent=1 / 总数（D2） | ↑ |
| event_unconsumed | consumed=0 AND processed=0 数（当前注入池） | ↓ |
| workflow_completed_rate | review_json workflow_completed=true / L2 数 | ↑ |
| correction_signals | detectUserFrustration 关键词在窗口内 user 消息命中数 | ↓（辅助信号） |

### 2.3 注入类（来源 [INJECT] 日志）
| 指标 | 计算 | 方向 |
|------|------|:--:|
| emerge_events_total | 窗口内最后一次 emerge_events 的 total | ↓ |
| action_items | 窗口内最后一次 emerge_events 的 items | ↓（≤10） |
| inject_chars | [INJECT] 行 chars 均值 | ≤800 |

## 3. JSON schema v1

```json
{
  "schema_version": 1,
  "generated_at": "2026-08-07T13:30:00+08:00",
  "window": {"since": "2026-08-07T09:00:00", "until": "2026-08-07T13:00:00"},
  "metrics": {
    "consumption": {
      "claude": {"calls": 100, "in_tok": 13500000, "out_tok": 30000,
                 "avg_lat": 7.2, "p95_lat": 21.0, "cache_hit_rate": 0.99, "injected_rate": 0.9},
      "codex":  {"calls": 40, "in_tok": 4000000, "out_tok": 8000,
                 "avg_lat": 4.1, "p95_lat": 12.3, "cache_hit_rate": 0.0, "injected_rate": 0.6},
      "totals": {"calls": 140, "in_tok": 17500000, "out_tok": 38000}
    },
    "quality": {
      "l2_coverage": 0.54, "l2_nested_goal": 0, "event_processed_rate": 0.36,
      "event_unconsumed": 20, "workflow_completed_rate": 0.20, "correction_signals": 2
    },
    "injection": {"emerge_events_total": 20, "action_items": 7, "inject_chars": 679}
  },
  "notes": {"cache_hit_coverage": 0.877}
}
```

## 4. delta 对比格式

两份快照对比输出（`aipmc snapshot --diff a.json b.json`）：

```
指标                  修复前(10:58)  修复后(13:03)  delta      方向
action_items          42            7             -83%       ✅ 改善
emerge_events_total   44            20            -55%       ✅ 改善
inject_chars          —             679           ≤800       ✅ 达标
l2_coverage           0.54          0.54          0          ⚪ 未变（待配 key）
```

规则：每项标注 ✅/❌/⚪（对照方向与目标值），输出最后附一句总结。

## 5. 回算可行性（已验证）

- [LLM] 25,953 行：agent/model/in_tok/out_tok/cache_hit(88%)/injected/lat 均可按时间戳窗口聚合
- DB：l2_coverage（65 条中 35 有摘要=53.8%）、event_processed_rate（394 中 140=35.5%）、workflow_completed（13/65=20%）、correction_signals（关键词命中）均现成可算
- 注入类：emerge_events 日志 13:03 起为新格式（含 perTypeCap/ceil），旧格式（cap=3）可兼容解析

## 6. 未决/备注

- 项目维度拆分需补 `[LLM]` 日志 project 字段 → v2
- token/session 需补 session_id → v2
- correction_signals 关键词库（当前 7 个）扩充 → 可与快照并行做
- responses 路径缺 cache_hit/injected 的行按"缺失"处理并计入 notes.cache_hit_coverage，不阻塞 v1
