# 路2 A/B：aipm-usage skill 安装/移除（干预开/关）

干预对象 = `aipm-usage` skill。Claude Code 从 `~/.claude/skills/<name>/SKILL.md` 加载个人 skill。

| 窗口 | 操作 |
|---|---|
| **OFF（before 对照）** | 确保 `~/.claude/skills/aipm-usage` **不存在**（或在真实会话开始前移除）。 |
| **ON（after 干预）** | 拷贝本目录到 `~/.claude/skills/aipm-usage/`，再开新的 claude 会话。 |

```
# ON（装）
mkdir -p ~/.claude/skills/aipm-usage
cp skills/aipm-usage/SKILL.md ~/.claude/skills/aipm-usage/
# OFF（卸）
rm -rf ~/.claude/skills/aipm-usage
```

**口径红线（METRICS_REPORT §0 / decision-20260908）：**
- before/after 唯一差异 = 本 skill 开/关；工具/描述/注入/项目/启动方式全冻结。
- 固定窗口可复算（`--since/--until`）；禁 all 窗口。
- 结果排除 `review_status=auto`，分列双目标（纯自发↑ + 含半自发 15%→30%）；禁止用执行率当成绩。
- 无干预标注的任何 after 数据，不得进入 A/B 比较表（`ab_compare.py --intervention off` 会拦截）。
