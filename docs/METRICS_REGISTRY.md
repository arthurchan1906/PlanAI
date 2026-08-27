# 指标注册表（Metrics Registry）

`aipmc metrics` 输出的每个指标在此登记——编号、数据源、窗口、计算口径、目标。
本表是口径的**单一事实来源**：修改 `metrics.go` 输出须同步本表；
`metrics_registry_test.go` 强制「输出键 == 注册表键」一一对应且无重复，
防止 8/26「l2_coverage 40.1 vs 52.8」式口径分叉再次发生（同名指标两套算法）。

## 窗口约定

| 窗口 | 含义 | 适用 |
|------|------|------|
| 全表 | DB 机制健康现状（point-in-time） | B1/B2/B6/E6/E7/P0/H2 metadata |
| 随 `--since` | 验收/诊断/行为类行，随 since 回算 | D2 / F1 / H2 rel_path(filetools) / E5 update_status |
| 日志 | `~/.aipmc/logs/aipmc.log` 当前文件（20MB 归档），可选 `--window` | B8/C1/C2/C3/E5 mcp/E8/E9/E3 |

## 注册表

| 编号 | 指标 | 数据源 | 窗口 | 计算口径 | 目标 | 8/27 实测 |
|------|------|--------|------|----------|------|-----------|
| B1 | summary_coverage | DB | 全表 | distinct session（discussion_log 去重，排除空/unknown）中有非空 summary 的占比；原 l2_coverage 改名消歧（8/27） | ≥85% | 39.2% (65/166) |
| B2 | l2_nested_goal | DB | 全表 | summary 列 goal 值是嵌套 JSON 的条数 | =0 | 0 |
| B2 | l2_md_block | DB | 全表 | summary 含 ```json 代码块的条数 | =0 | 0 |
| B6 | event_dup_rate | DB | 全表 | 1 − distinct(type\|entity_type\|entity_id) / events 总数 | <10% | 15.3% |
| D2 | event_processed_rate(可行动) | DB | 随 `--since` | 可行动事件（commit_orphan/mcp_error/hotspot_untracked）processed_by_agent=1 / 可行动总数；免处理参考事件（tentative_link/task_created/plan_created）不计分母（8/27 统一，防虚假拉低） | ≥40% | 29.6% (88/297) |
| F1 | 事件→动作漏斗 | DB | 随 `--since` | 免处理/可行动/已处理三口径拆分 + 按类型处理分布诊断（fmt 行） | 参考 | 免处理=234 可行动=297 已处理=88 |
| E6 | workflow_score | DB | 全表 | session_summaries quality_score>0 的平均分（启发式规则分，100 起扣） | ≥60 | 49.2 (127/129) |
| E7 | task_completion_rate | DB | 全表 | tasks done /（done+todo+in_progress+blocked+paused） | >80% | 80.4% (115/143) |
| P0 | orphan_rate | DB | 全表 | commits task_id 为空占比（P0 采集管道完整性子项） | <10% | 3.2% (7/217) |
| P0 | hash_traceability | DB | 全表 | commits 非空 commit_hash 占比 | >90% | 100.0% |
| P0 | hash_uniqueness | DB | 全表 | 重复 commit_hash 行数 | =0 | 0 |
| H2 | metadata_health | DB | 全表 | metadata valid=非空且 json_valid；分母排除空串（对话消息本应空）；invalid 必须 0 | valid≥99.5% invalid=0 | valid=100.0% invalid=0 |
| H2 | rel_path_coverage(filetools) | DB | 随 `--since` | filetools（claude edit/new_file/read/write；codex apply_patch/Write/Read）中 rel_path 非空占比；分母只计项目内路径（F4 口径，项目外不可能有 rel_path） | claude≥90% codex 按决策19 | claude=84.4% |
| H2 | rel_path_coverage(bash) | DB | 全表 | bash 高置信模式才写 rel_path（决策 19 接受漏检） | 参考 | claude=13.5% codex=26.1% |
| B8 | hook_error(agent) | 日志 | 日志 | hook agent 路径错误计数 | 计数 | 6 |
| B8 | hook_error(post-commit) | 日志 | 日志 | hook post-commit 路径错误计数 | 计数 | 3 |
| C1 | inject_rate | 日志 | 日志 | [INJECT] injected=Y 占比 | 参考 | 91.5% |
| C1 | inject_coverage | 日志 | 日志 | 注入条数（去重后）覆盖会话数 | ≥80% | 100.0% |
| C2 | file_parse_ok_rate | 日志 | 日志 | 文件路径解析成功占比 | ≥90% | 100.0% |
| C3 | suppressed(char_limit) | 日志 | 日志 | 字符预算裁剪占比 | <30% | 29.1% |
| C3 | action_items(最新emerge) | 日志 | 日志 | 最后一次 emerge_events 的 items | ≤10 | 7/50 |
| E5 | mcp_success_rate | 日志 | 日志 | [MCP] status=OK 占比 | ≥95% | 98.0% |
| E5 | mcp_calls | 日志 | 日志 | [MCP] 调用总数 | 参考 | 2356 |
| E5 | mcp_err_reason | 日志 | 日志 | [MCP-ERR] reason 分类分布 | 参考 | system_fault=41 |
| E5 | mcp_read/write | 日志 | 日志 | 读/写工具调用数（isWriteTool 分类） | 参考 | 1614/742 |
| E5 | mcp 工具分布 | 日志 | 日志 | Top8 工具调用分布（fmt 行） | 参考 | read_discussions=660 |
| E5 | mcp 按agent | 日志 | 日志 | 按 src 拆分调用数（fmt 行） | 参考 | codex-cli=1830 |
| E5 | update_status 显式率(窗口) | DB | 随 `--since` | 窗口内显式声明 session（agent_status explicit=1，JOIN discussion_log）/ 窗口活跃 session（users>0）；B0.5 重定义（8/27），旧行级口径 2/158 低估；`--since all` 等价旧口径可回溯；已知边界：8/24 上午 7 次 [MCP] 调用未落 explicit 库（M 线口径注意点） | 参考 | 2/40 (5.0%) |
| E8 | pipeline_health | 日志 | 日志 | L3 session 处理量 + reconcile 成功率 | ≥98% | 100.0% |
| E8 | review_error | 日志 | 日志 | review 错误计数 | 计数 | 1 |
| E9 | done_gate | 日志 | 日志 | done-gate pass/reject 分布（reject 带 reason） | 参考 | pass=58 reject=2 |
| E3 | cache_hit_rate | 日志 | 日志 | cache_hit/(cache_hit+cache_create)，仅含 cache_hit 行 | ≥90% | 91.1% |

## 变更记录

- 8/27：B1 原 l2_coverage 改名 summary_coverage（与 L2 确认器消歧）
- 8/27：D2 三口径统一 → 可行动口径（免处理事件不计分母）
- 8/27：E5 update_status 显式率 → 窗口 session 口径（B0.5 重定义落地）
