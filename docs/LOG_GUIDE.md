# 日志查询指南（E1 观测基线）

> 目的：沉淀日志 tag 清单与查询命令，让每次修复都能用日志复测 `docs/EVALUATION.md` 中的指标。
> 版本：2026-08-14（20MB 自动归档 + project 标签 + BOOT 版本标记 + sanitize）

## 0. 日志生命周期（8/14 起）

- **归档**：`~/.aipmc/logs/aipmc.log` 超过 20MB 自动归档为 `aipmc.log.<YYYYMMDD_HHMMSS>`，保留最近 7 份；写入口 `u.LogShared` 持锁旋转，多进程安全（输掉 rename 竞态的进程跳过本轮）。
- **版本锚点**：serve/proxy 每次启动写一行 `[BOOT] version=<git sha> project=<name> pid=<pid>`——任何日志段都能映射回具体提交与项目。版本由 `build.sh` 经 `-X aipmc/u.BuildVersion` 注入。
- **project 标签**：serve 进程的行尾带 `project=<项目名>`（`SetLogProject` 注入）；proxy/hook 的行无此标签（跨项目，按请求归属是 v2 范围）。
- **清洗**：写入口把非法 UTF-8 与 C0/C1 控制字节（含 NEL 0x85）替换为 `?`，根治 BSD grep 把日志当二进制的问题。
- **指标口径**：`aipmc metrics` 的日志类指标只扫当前文件；归档前的历史窗口用 `aipmc.log.*` 复核。

## 1. 日志位置

| 内容 | 路径 | 说明 |
|------|------|------|
| 共享日志（PIPELINE/INJECT/LLM/HOOK/EMERGE/FTS5/MCP 等） | `~/.aipmc/logs/aipmc.log` | `u.LogShared()` 统一写入，所有进程共享 |
| Web/Proxy 启动日志 | `~/.aipmc/logs/restart-*.log` | serve/proxy 的 stdout/stderr |
| Proxy 流量捕获 | 内存环形缓冲，`curl http://127.0.0.1:19530/__proxy/capture?per_page=N` | 请求/响应样本，不进文件 |
| 项目数据库 | `<项目>/.pmai/data/pmai.db` | 结构化数据（events/session_summaries/fts5_index 等） |

## 2. Tag 清单（按组件）

### PIPELINE（B1→L2→L3 自动评审）
- 关键字段：`session=`, `src=`, `intent=`, `score=`, `files=`, `entities=`, `L2=ok|skip`
- 跳过原因：`L2 skip session=... reason=text_too_short len=N` / `L2 summarize error`
- 查询：
  - `rg "PIPELINE.*L2" ~/.aipmc/logs/aipmc.log | tail -20`（最近 L2 结果）
  - `rg "PIPELINE.*reason=" ~/.aipmc/logs/aipmc.log | grep -oE "reason=[a-z_]+" | sort | uniq -c | sort -rn`（无摘要原因分布 → B1 覆盖率根因）

### INJECT（Q3 注入）
- 关键字段：`agent=`, `goals=`, `warnings=`, `actions=`, `file=`, `guidelines=`, `chars=`, `suppressed=N reason=...`, `hash=`
- 注入率：`injected=Y|N`（在 LLM 行）
- 查询：
  - `rg "INJECT.*agent=claude" ~/.aipmc/logs/aipmc.log | tail -5`（最近一次注入明细）
  - `rg -c "injected=Y" ~/.aipmc/logs/aipmc.log; rg -c "injected=N" ~/.aipmc/logs/aipmc.log`（C1 注入率）
  - `rg "suppressed=" ~/.aipmc/logs/aipmc.log | grep -oE "reason=[a-z_]+" | sort | uniq -c | sort -rn`（C3 抑制原因分布）

### LLM（Q1→Q4 链路）
- 关键字段：`agent=`, `model=`, `in_tok=`, `out_tok=`, `cache_hit=`, `cache_create=`, `injected=`, `lat=`
- 查询：`rg "\[LLM\]" ~/.aipmc/logs/aipmc.log | tail -10`

### HOOK（L0 捕获）— Phase 1.7 修复后
- 新增失败可见化：`panic src=claude|codex|gemini|opencode`、`json_parse_err src=...`
- 查询：`rg "HOOK" ~/.aipmc/logs/aipmc.log | tail -20`（正常应无 panic/json_parse_err 以外的行）
- 复测 A1：`rg -c "HOOK" ~/.aipmc/logs/aipmc.log` 应随会话增长，且 `panic|json_parse_err` 出现即可定位失败 Agent

### EMERGE / RECONCILE / GITSYNC（B6 事件产出）
- `EMERGE orphans=N stale=N hotspot=N`：各事件类型产出数
- `RECONCILE project=... auto_linked=N tentative=N`：B4 自动链接数
- 复测 B6：事件重复率 = events 表中 `type + entity_id` 非唯一行占比（见第 4 节 SQL）

### FTS5（搜索质量）— Phase 1.3 修复后
- `rebuild insert err type=... id=...`：重建失败（数据库锁/约束）可见化
- 查询：`rg "FTS5" ~/.aipmc/logs/aipmc.log | tail -10`

### MCP / MCP-ERR（D1 工具使用）
- `MCP tool=... status=OK|ERR`；`MCP-ERR tool=... error=...`
- 查询：`rg -c "MCP.*status=OK" ~/.aipmc/logs/aipmc.log; rg "MCP-ERR" ~/.aipmc/logs/aipmc.log | tail -10`

### YIELD / GUIDELINES / DONE-GATE
- `YIELD agent=... signals=...`：上下文过大时让出
- `GUIDELINES loaded N chars`：guidelines 注入确认
- `DONE-GATE pass task=...`：done 门禁通过记录

## 3. BSD grep 注意事项（macOS 默认工具链）

- **不要用 `grep -P`（PCRE）**：macOS 自带 BSD grep 不支持，会报 `grep: -P not supported`。用 `grep -E`（ERE）或直接 `rg`（推荐，仓库内已大量使用）。
- `rg` 默认大小写敏感；`rg -i` 忽略大小写；`rg -o` 只输出匹配片段，配合 `sort | uniq -c | sort -rn` 做分布统计。
- 时间过滤：8/12 起日志行首为 `[YYYY-MM-DD HH:MM:SS]`（历史行仅 `[HH:MM:SS]` 无日期，`--window`/按日期过滤只对带日期行生效）。当天行：`rg "^\[$(date +%F)" ~/.aipmc/logs/aipmc.log`；窗口统计：`aipmc metrics --window 24h`。
- **非法 UTF-8 陷阱（8/12 实测，根因已修）**：aipmc.log 有 1857 条非法 UTF-8 行（约 0.7%），**全部是 `TruncateStr`/`truncArg` 按字节截断中文 rune 的产物**（每行都含 `...`，97% 以 `...` 结尾）。8/12 已改为 rune 边界回退，新日志不再产生；存量非法行需等日志轮转。注意：`file` 报的 "NEL line terminators" 是**误报**——实测日志 0 个真正 NEL 字符（`0xC2 0x85`），它把截断序列里的裸 `0x85` 连续字节误读成了 NEL。BSD 工具在默认 locale 下遇非法 UTF-8 会报 `illegal byte sequence` 或漏匹配：
  - 查询一律加 `LC_ALL=C`；`grep` 再加 `-a`（按文本处理）：`LC_ALL=C grep -a "pattern" ~/.aipmc/logs/aipmc.log`
  - 不要用 `strings`/`ugrep` 做全量行匹配（会把截断字节/0x85 当特殊字符，计数失真）。

## 4. 从数据库复测关键指标（sqlite3 只读）

```bash
DB=<项目>/.pmai/data/pmai.db
# B6 事件重复率（Phase 1.1 后应≈0）
sqlite3 "file:$DB?mode=ro" "SELECT type, COUNT(*)-COUNT(DISTINCT entity_id) AS dup FROM events GROUP BY type HAVING dup>0;"
# B1 L2 覆盖率（≥85% 验收线；8/12 口径修正：分母=discussion_log 去重 session_id，
# 排除空/unknown；分子=这些 session 中至少有一条非空 summary 的 session 数。
# 旧口径 session_summaries 行数作分母会高估——ED 实测 58% vs 真实 34%。输出由
# `aipmc metrics` B1 行直接给出，SQL 复测保持同口径）
sqlite3 "file:$DB?mode=ro" "SELECT (SELECT COUNT(DISTINCT session_id) FROM discussion_log WHERE session_id!='' AND session_id!='unknown'), (SELECT COUNT(DISTINCT s.session_id) FROM session_summaries s JOIN discussion_log d ON d.session_id=s.session_id WHERE s.summary!='' AND d.session_id!='' AND d.session_id!='unknown');"
# B2 嵌套 goal 残留（Phase 1.4 后应=0）
sqlite3 "file:$DB?mode=ro" "SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%\"goal\":\"{%';"
# FTS5 覆盖（plan/bug 应>0，Phase 1.3 后）
sqlite3 "file:$DB?mode=ro" "SELECT entity_type, COUNT(*) FROM fts5_index GROUP BY entity_type ORDER BY 2 DESC;"
# D2 事件消费率（注意：mark_consumed 为「已读」语义，2.3 修复后区分已处理）
sqlite3 "file:$DB?mode=ro" "SELECT ROUND(100.0*SUM(consumed_by_agent)/COUNT(*),1) FROM events;"
```

> 注意：DB 为 WAL 模式（2026-08-07 起），只读查询请用 `file:$DB?mode=ro`，否则沙箱/无写权限环境会报 `unable to open database file`。
