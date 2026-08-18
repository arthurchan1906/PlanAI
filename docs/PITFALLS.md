# AIPMC 陷阱记录

> 遇到过的问题，写下来，以后遇到直接定位。

## Go map range 顺序随机（2026-08-18）

**症状**：deepseek prefix cache 命中率从 90%+ 掉到 ~60%；日志中 `[INJECT]` 每请求触发、
`chars=` 恒定但内容在变；`[LLM]` 断点固定在某 token 水位（4608/4480）。

**根因**：`for k := range map` 的遍历顺序每次随机。注入路径 `resolveFileContext`
内层 `for tid, tag := range tasks`（`map[string]string`）构建 `assoc` 切片 →
`fullHash = hashString("%v"(fileAssoc))` 顺序敏感 + `buildContextBlock` 200B 子预算
按切片顺序截断（11 个匹配随机存活 2 个）→ 每请求注入内容不同 → SP 抖动 →
prefix cache 在 system prompt 末尾断裂。

**规则**：凡 map → slice → 参与 **hash / 序列化 / 截断 / 拼接输出**，必须先排序。
模式：`sort.Strings(slice)`（见 `buildFileAssoc`）。

**相关历史**：
- 2026-08-18 修复：`288147e`（assoc 排序 + same_content 跳过仍注入同一块）
- 2026-06-28 `de13363`：map 相关但属另一类（len(map) != max key → range 替代，键完整性）

**检查清单**（改代码时自查）：
1. `range` 的变量是 map 还是 slice？map → 结果是否进 hash/字符串/数组比较？
2. 输出顺序是否需要稳定（日志、SP 注入、缓存 key）？
3. 已审计安全点：`metrics.go` 输出均先 sort 或聚合无关顺序；`store/discussion.go`
   `ids` 为 slice；`discussion_dedup.go` 的 `seen` 仅作集合查询。
