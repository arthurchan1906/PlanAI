# PlanAI 代码审查与改进建议

> 基于 Hermes Agent 架构实践的外部评估 | 2026-06-06

---

## 1. 总体评价

PlanAI 是一个**设计哲学极其成熟**的项目。它知道自己不做什么（不加 LLM、不替 Agent 做决策），知道自己该做什么（高密度上下文注入、结构化反思提示）。在这个定位上，它已经做得很好了。

| 维度 | 评分 | 说明 |
|---|---|---|
| **设计哲学** | ⭐⭐⭐⭐⭐ | 简报非命令、不内置 LLM、渐进实施——每个决策都有充分理由 |
| **代码质量** | ⭐⭐⭐⭐ | Go 代码干净，结构清晰 |
| **数据模型** | ⭐⭐⭐⭐⭐ | Vision→Roadmap→Plan→Task→Commit→Bug 层级 + Thread 回溯性聚合 |
| **搜索** | ⭐⭐ | 纯线性扫描 O(n)，数据量大后性能堪忧 |
| **分析引擎** | ⭐⭐⭐⭐ | 8 种检测覆盖全面，相似度算法可进一步精细 |
| **MCP 实现** | ⭐⭐⭐⭐ | 17 tools + reflection + related_context，设计超前 |
| **会话连续性** | ⭐⭐ | 每次启动从零开始，缺少跨会话记忆 |
| **可部署性** | ⭐⭐⭐⭐⭐ | Go 单文件 + embed 前端，真正的零依赖部署 |

---

## 2. 改进建议（按优先级排序）

### 2.1 高优先级（改动小、收益大）

#### 建议 1：用 FTS5 替代线性搜索

**现状**：`search.go` 的 `matchScore()` 是朴素的 `strings.Contains` 遍历所有实体。

**方案**：SQLite 内置 FTS5（无需额外依赖）：

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
    entity_type, entity_id, title, content, status
);
```

每次 CRUD 时同步更新 FTS5 索引，搜索时用 BM25 排序。Hermes Agent 的 `memory_manager.py` 就用 FTS5 做全文本搜索。

**收益**：搜索速度从 O(n) 降到 O(log n)，BM25 排序比简单匹配计数准确得多。

---

#### 建议 2：用 Trigram 相似度替代 Word-level Jaccard

**现状**：`analyze.go:titleSimilarity()` 基于词级 Jaccard，对中文和短文本效果差。

**方案**：用 Go 标准库就能实现 trigram 相似度：

```go
func trigramSimilarity(a, b string) float64 {
    aGrams := extractTrigrams(strings.ToLower(a))
    bGrams := extractTrigrams(strings.ToLower(b))
    if len(aGrams) == 0 || len(bGrams) == 0 {
        return 0
    }
    intersection := 0
    for g := range aGrams {
        if bGrams[g] {
            intersection++
        }
    }
    return float64(intersection) / float64(len(aGrams)+len(bGrams)-intersection)
}
```

**收益**：对中文、缩写、拼写变体的匹配更鲁棒。零外部依赖。

---

#### 建议 3：简报增加优先级分层

**现状**：`BuildBriefing()` 平铺所有信息，可能超过 Agent 的注意力预算。

**方案**：借鉴 Hermes Agent `prompt_builder.py` 的三层结构，每条信息附带可执行动作：

```markdown
## ⚠️ 需要立即行动
- [Drift] commit-abc 改了 session.go 但 task-auth-02 scope 只有 auth.go
  → aipm link add --source commit-abc --target task-session-03

## 📋 应该知道
- [新决策] Decision #44: API 错误格式统一用 RFC 7807

## 💡 参考信息
- 3 个 plan 进度正常，无风险
```

**收益**：Agent 不会被信息淹没，紧急事项不会淹没在背景信息中。

---

### 2.2 中优先级（改动中等，收益持续）

#### 建议 4：添加跨会话记忆

**现状**：每次 `aipmc start` 从零开始，Agent 不记得上次做了什么决策。

**方案**：新增轻量记忆表：

```sql
CREATE TABLE agent_memory (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    key TEXT NOT NULL,          -- "当前焦点" / "上次决策" / "发现的风险"
    value TEXT NOT NULL,
    created_at TEXT NOT NULL,
    ttl_hours INTEGER DEFAULT 72
);
```

Agent 每次 `aipm_get_briefing` 时自动注入"上次会话摘要"。

**收益**：Agent 跨会话拥有连续性。Hermes Agent 的 `memory_manager.py` 就是这个模式。

---

#### 建议 5：MCP 工具分组（Toolset）

**现状**：17 个 MCP tool 全部平铺注册，Agent 看到一长串。

**方案**：借鉴 Hermes `toolsets.py` 的分组模式：

```go
var toolsets = map[string]Toolset{
    "briefing":    {"简报", "会话开始时的上下文注入",
        []string{"aipm_get_briefing", "aipm_mark_consumed"}},
    "task_mgmt":   {"任务管理", "task CRUD",
        []string{"aipm_create_task", "aipm_update_task_status", "aipm_append_task_note"}},
    "recording":   {"记录", "commit/bug/decision 记录",
        []string{"aipm_record_commit", "aipm_record_bug", "aipm_record_decision"}},
    "thread_mgmt": {"线索", "线程聚合与分析",
        []string{"aipm_create_thread", "aipm_add_to_thread", "aipm_suggest_threads"}},
    "linking":     {"关联", "实体关系管理",
        []string{"aipm_link_entities", "aipm_search_context"}},
}
```

**收益**：Agent 可以按需选择加载哪些工具，减少 tool list 噪声。

---

#### 建议 6：Thread 建议增加结构化特征

**现状**：`analyzeThreadSuggestions()` 纯算法聚类，准确度有限。

**方案**：不急于加 LLM。先增加结构化特征提升算法准确率：

```go
type ThreadCandidate struct {
    SharedFiles    []string  // 多个 commit 改了同样的文件
    SharedKeywords []string  // commit message 中出现相同术语
    TimeProximity  bool      // 在同一天内
    SamePlan       bool      // 不同 task 同 plan
    CrossPlanFiles bool      // 不同 plan 的 task 改了同目录
    Confidence     float64
}
```

**收益**：算法准确度提升，减少 Agent 误判。

---

### 2.3 低优先级（改进体验，不紧急）

#### 建议 7：Web UI 加 WebSocket 实时推送

**现状**：`events` 表有 `consumed_by_agent` 字段，但 PM 端无实时通知。

**方案**：Go 标准库 `golang.org/x/net/websocket` 就能实现极简推送。

**收益**：PM 不再需要手动刷新看变更。

---

#### 建议 8：skill.go 动态生成

**现状**：`skill.go` 是 `const skillMD` 字符串常量。

**方案**：让 skill 内容从数据库动态生成——如果 Agent 经常忘记某步，自动加重提醒。

**收益**：Skill 从静态文档变成自适应的行为引导。

---

## 3. 不建议做的事

基于对代码和设计文档的理解，以下事情**不应该做**：

1. **不要加内置 LLM** — DESIGN.md Decision 1 完全正确。PlanAI 的价值在"让 Agent 变聪明"，不是在内部跑 LLM
2. **不要改 Go→Python** — 单文件部署是 PlanAI 的核心竞争力。Go 的并发模型和编译特性在这个场景下比 Python 更合适
3. **不要加复杂的权限系统** — 目前单用户场景够了。如果将来需要多用户，先加 `--user` flag，不要引入 OAuth
4. **不要引入外部向量数据库** — SQLite + FTS5 在这个规模下完全够用。Chroma/Weaviate/Pinecone 是过度工程

---

*审查人：Claude (based on Hermes Agent architecture patterns)*
*审查日期：2026-06-06*
