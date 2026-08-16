# EVAL PIPELINE 设计:行动测量评估管线

> 状态:设计定稿(2026-08-16)
> 前置文档:`docs/HARNESS_ROADMAP.md`(M1-M5 归因指标)、`docs/MEASUREMENT_LOOP.md`(五层测量框架)
> 本设计实现 HARNESS_ROADMAP 的评测闭环(P1-2)与 MEASUREMENT_LOOP 的 L-O/L-A 层的**可执行管线**。

## 1. 背景与目标

### 1.1 问题

`discussion_log` 有 11,562 条记录(6 个 agent 来源),但:
- **范围不可用**:单个 session 可达 1,194 行、跨 2 天、74 条 user 消息,而真正的意图只有 ~15-20 个。user 消息大多是对话型/指令型("继续"、"执行"、"查看claude的意见"),不是新意图。
- **行为不可读**:metadata 四套格式并存(`_type=post_tool` 新格式 / `type=bash` 旧格式 / `hook_event_name` gemini / `conversation_id` cursor),无法直接结构化分析。
- **验证不可信**:commits 表 `test_status/review_status` 为 agent 自报(Goodhart 极端形态),系统侧验证为零。

### 1.2 目标

建立一条从原始 discussion 记录到结构化评估报告的管线:

1. **范围切分**:把随机 user 意图流切分成可评估的**意图段(episode)**——评测的基本单位。
2. **行为分析**:从记录文本+metadata 系统提取 agent 行为信号(操作层/结果层/语义层)。
3. **两套报告**:
   - **产出质量报告**(用户视角):意图达成率、自证检测、质量评估 → 供用户抽查与信任决策。
   - **归因报告**(harness 视角):注入命中、知识复用、工具采纳 → M1-M5 延续。

### 1.3 已定决策(2026-08-16 讨论收敛)

| # | 决策点 | 结论 |
|---|---|---|
| D1 | 评测用途 | 两者都要,分两个报告(产出质量 + 归因),共享公共底座 |
| D2 | 切分正确性信任 | 先建标注集校准(15-20 段人工确认边界)再全量 |
| D3 | 意图达成成功标准 | 标注集上用户判"达成/未达成/部分"为 ground truth,LLM few-shot 学习后全量跑,低置信段标出 |
| D4 | LLM 成本策略 | 近期窗口(默认近 30 天)全量 + 历史每周抽样;归因报告用窗口内对照替代历史基线 |
| D5 | 运行形态 | Go 实现为 `aipmc eval` 子命令(复用 ai.Client / DB 层) |
| D6 | 标注交互 | 文件标注(JSON/CSV),非交互式 CLI |

## 2. 总体架构:八阶段管线

```
阶段0 数据准备   四格式分型解析 + 乱码容忍 + 窗口裁剪
  ↓
阶段1 回合化     user 消息开回合,其后所有记录归入(确定性规则)
  ↓
阶段2 意图分类   LLM 批处理:user 消息 → 任务型/对话型/指令型/状态型
  ↓
阶段3 意图段切分  强制边界(任务型) + 行为佐证(文件突变/commit/时间间隔)
  ↓
阶段4 标注集校准  15-20 段人工标注 → 切分金标 + 达成标准 ground truth
  ↓
阶段5 行为提取     metadata → 操作层/结果层信号(纯代码,确定性)
  ↓
阶段6 语义分析    LLM 学标注集标准 → 意图摘要/达成判定/置信度
  ↓
阶段7 报告        [产出质量报告] + [归因报告] + 待复核清单
  ↓
阶段8 增量        episode_id → JSON 缓存,重跑只处理新段
```

### 数据流

```
discussion_log ──┐
commits ────────┼─→ eval 管道 → episode 表/JSON ──→ 报告
events ──────────┘           ↑
session_summaries ───────────┘
```

## 3. 阶段设计

### 3.1 阶段 0:数据准备

- **四格式分型解析器** `parse_tool_record(metadata) → ToolRecord`:

| 格式 | 判别 | 关键字段 |
|---|---|---|
| claude-code 新 | `_type=post_tool` | tool_name / tool_input / exit_code / cwd / model |
| 旧通用 | `type=bash` | command / exit_code / stdout |
| gemini | `hook_event_name` | prompt / transcript_path / cwd |
| cursor | `conversation_id` | generation_id / workspace_roots |

  归一化结构:`{tool, command, files[], exit_code, output, model, cwd}`。
- **乱码容忍**:GBK 解码失败行标记 `quality=degraded`,不丢弃、降权参与语义分析。
- **窗口**:`--since` 参数(默认 30 天);历史段按周抽样(每周 ≤2 段),仅进产出质量基线展示,不进归因报告。

### 3.2 阶段 1:回合化(turn)

- 定义:一条 user 消息 + 其后所有 assistant/tool 记录(按 `created_at` 升序),直到下一条 user 消息。
- 实现:单表扫描 + 游标,确定性,零 LLM 成本。
- 产出:`Turn{user_msg, records[], start, end}`。

### 3.3 阶段 2:user 意图分类

- 调用 `ai.Client.ChatJSON`(现有 chatModel,JSON 强制输出模式)。
- 分类:

| 类型 | 语义 | 切分作用 |
|---|---|---|
| 任务型 | 新意图(实施/调研/运维请求) | 开新段(强制边界) |
| 对话型 | 延续讨论("继续查看claude的意见") | 并入当前段 |
| 指令型 | 状态/约束变更("不要开干"、"推送到远程") | 并入当前段 |
| 状态型 | 信息告知("Claude有了新回复") | 并入当前段 |

- 兜底规则:≤8 字且含"继续/执行/查看/推送/不要/暂时"等关键词 → 直接判非任务型(省 LLM 调用)。
- 输出:`{type, confidence}`;低置信(<0.6)回退到兜底规则。

### 3.4 阶段 3:意图段切分(episode)

混合信号,按强度排序:

| 信号 | 规则 | 角色 |
|---|---|---|
| 任务型 user 消息 | 开新段 | 强制边界 |
| 时间间隔 | 段内出现 commit 后 gap > 60min | 弱边界 |
| 文件集合突变 | 相邻回合引用文件 Jaccard < 0.2 | 佐证 |
| commit 发生 | 段内 commit 是段结束锚点 | 佐证 |

- 所有阈值为**校准参数**(见 3.5),标注集阶段用真实数据调整。
- 产出:`Episode{id, session_id, agent, intent_text, turns[], files, start, end, commits[]}`。
- episode 与 task 的绑定:段内 commit 若有 `task_id` 关联,记录之(可选 ground truth 锚点)。

### 3.5 阶段 4:标注集校准(人工)

- **抽样**:按 source(claude-code/codex-cli/opencode/cursor)× 意图类型(实施/调研/运维)分层抽 15-20 段。
- **段卡片**:每段渲染 `{user 消息序列, 工具统计, 文件集合, 时间跨度, 当前切分边界}`。
- **标注文件**(JSON,`eval/calibration/calibration.json`):
  ```json
  {
    "episode_id": "ep-2026-08-14-001",
    "boundary_ok": true,          // 切分边界是否准确;false 时给 correct_boundary
    "correct_boundary": null,
    "achievement": "achieved",     // achieved / partial / not_achieved
    "reason": "用户要求提交git,段内有commit且推送成功",
    "notes": ""
  }
  ```
- 标注产出:① 切分规则金标(调参数);② 达成标准 ground truth(供阶段 6 few-shot)。

### 3.6 阶段 5:行为提取(纯代码)

每段产出结构化行为对象:

```go
type EpisodeBehavior struct {
    ToolUsage    map[string]int      // bash/edit/read/mcp/... 计数
    CmdSemantics map[string]int      // build/test/git/query/deploy/other 分类
    Files        struct{ Read, Write []string }
    OutOfScopeFiles float64          // 段外文件占比(相对 cwd 判定)
    ExitCode     struct{ Failures, Retries, RetrySuccess int }
    Verification struct{ RanBuild, RanTest, RanVet, HasCommit bool }
    TextSignals  struct{ ClaimedDone, ClaimedTestPassed int }  // 从 assistant 文本提取
}
```

- 命令语义分类:bash command 前缀/关键词匹配(build→`go build|npm|make`,test→`go test|pytest|npm test` 等)。
- **自证检测**:`TextSignals.ClaimedTestPassed > 0 && !Verification.RanTest` → `self_claim_without_proof` 标记。

### 3.7 阶段 6:语义分析(LLM few-shot)

- Prompt 结构:系统说明 + 标注集示例(段卡片+用户判定+理由,≤8 例)+ 当前段卡片 + 行为提取结果。
- 输出(JSON):
  ```json
  {
    "intent_summary": "排查 cursor 未建立 hook 的原因并修复",
    "achievement": "achieved",
    "evidence": ["段内运行了 hook install 命令", "exit_code=0", "有相关 commit"],
    "confidence": 0.85
  }
  ```
- `confidence < 0.7` → 自动进"待人工复核清单"。

### 3.8 阶段 7:两套报告

**产出质量报告**(`eval report --kind quality`):
- 段级卡片列表(按 agent/时间/达成状态过滤)
- 聚合:达成率分布、自证率(声称 vs 实测 gap)、失败重试率、按 agent 对比
- 待复核清单(低置信段 + 自证无实测段)

**归因报告**(`eval report --kind attribution`,M1-M5 延续):
- 窗口内对照(inject vs suppressed,复用 HARNESS_ROADMAP §2 口径)
- 注入命中率、知识复用率、简报信噪比(近 7 天)
- 注:历史基线缺位由窗口内前后段对比替代(D4)

### 3.9 阶段 8:增量

- 结果缓存:`eval/cache/<episode_id>.json`(段行为 + 语义结果)。
- 重跑:只处理新段(created_at > 上次水位)与标注变更影响的段。

## 4. 实现规划

### 4.1 代码结构

```
eval/
  parse.go        // 四格式 metadata 分型解析
  turn.go         // 阶段1 回合化
  classify.go     // 阶段2 意图分类(LLM + 兜底规则)
  segment.go      // 阶段3 意图段切分
  extract.go      // 阶段5 行为提取(命令语义/自证检测)
  analyze.go      // 阶段6 语义分析(LLM few-shot)
  report.go       // 阶段7 报告渲染(JSON + 人类可读)
  cache.go        // 阶段8 增量缓存
  calibration.go  // 阶段4 标注集生成/导入
main.go           // + case "eval": eval --since --kind --export-calibration
```

### 4.2 复用

| 现成组件 | 用途 |
|---|---|
| `ai.Client`(ChatJSON) | 阶段 2/6 LLM 调用 |
| `db` 层(`db.Open`/查询) | 读取 discussion_log/commits/events |
| `metrics.go` INJECT 日志解析器 | 归因报告 M2/M4 数据源 |
| `docs/HARNESS_ROADMAP.md` M1-M5 口径 | 归因报告指标定义 |
| `u.LogShared` | eval 运行日志 |

### 4.3 测试策略(fixture)

- fixture:合成 discussion_log 行(四格式各若干)+ 标注样例,进 CI(`eval/*_test.go`)。
- 覆盖:parse 分型、turn 切分、segment 边界、extract 自证检测、report 聚合数学。
- LLM 相关:mock ai.Client(fixture 固定响应),CI 不依赖真实 API(与 HARNESS_ROADMAP 口径一致)。

### 4.4 落地顺序

| 步骤 | 内容 | 依赖 |
|---|---|---|
| S1 | eval/ 骨架 + parse.go + fixture | 无 |
| S2 | turn + classify + segment + 参数默认值 | S1 |
| S3 | extract(自证检测核心) | S2 |
| S4 | calibration 生成/导入 + 人工标注一轮(15-20 段) | S2 |
| S5 | analyze(用标注集 few-shot)+ cache | S3+S4 |
| S6 | report 两套 + `aipmc eval` 接线 | S5 |
| S7 | 参数校准(用标注集金标调 3.4 阈值) | S4 |

## 5. 验收标准

1. `aipmc eval --since 30d` 在真实库上可运行,输出两套报告 JSON。
2. 标注集 15-20 段边界修正率 ≤ 20%(超过则切分规则需迭代)。
3. 自证检测在标注集上的精确率/召回率报告(标注集为准)。
4. `go test ./eval/...` 全绿(CI,含 fixture)。
5. 达成判定与人工复核清单:抽 10 个低置信段人工复核,判定一致率 ≥ 80%。

## 6. 风险与开放问题

| 风险 | 应对 |
|---|---|
| 切分边界在真实意图上主观性大 | 标注集先建信任;报告标注"边界置信度" |
| 旧格式 metadata 占比高(新格式覆盖率曾低至 0.2%) | 降权 degraded 行;近期窗口优先(D4) |
| LLM 判定漂移 | few-shot 固定标注集;低置信段强制人工复核 |
| 意图段内无 commit 的段(调研/讨论类)产出难量化 | 达成标准按意图类型分层(实施=产物+验证;调研=结论质量) |
| 归因报告无历史基线 | 窗口内对照(D4),标注局限 |
