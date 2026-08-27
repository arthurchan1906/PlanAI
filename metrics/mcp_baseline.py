#!/usr/bin/env python3
"""M 线产出物 2（8/27）：MCP 调用测量采集脚本。

数据源：~/.aipmc/logs/aipmc.log + aipmc.log.* 归档（[MCP] 日志逐调用记录，
P1a 决策 decision-20260827-131338-c95787：aipm 精确计数以 [MCP] 日志为权威）。

数量 4 项（[MCP] 日志口径）：
  total_calls      窗口内 [MCP] tool= 行数（排除 panic 等非 tool 行）
  retrieval_ratio  读/查类调用 / 全部 aipm_* 调用（检索占比）
  diversity        唯一工具数（含 Shannon 熵）
  deep_verify_ratio 深度查证工具调用占比（= 深度查证类 / 全部 aipm_* 调用）。
                   注意：这是「深度查证工具使用占比」的如实描述，不是「自发率」——
                   D1 自发率需 50-100 条人工标注 ≥80% 一致性后才可上（Claude 8/27
                   审核 Challenge 1：工具类别 ≠ 自发意图，8/27 的 get_decision 多为
                   任务驱动）

基线窗口 8/14-8/26（8/27 点破日单独列出，供反事实对比）。
输出：metrics/mcp_baseline_<date>.json + 控制台表格。
"""
import json, math, re, sys
from collections import Counter
from pathlib import Path

LOGS_DIR = Path.home() / ".aipmc" / "logs"
OUT = Path(__file__).parent / "mcp_baseline_2026-08-27.json"

# 读/查类（检索占比分子）
RETRIEVAL = {
    "aipm_read_discussions", "aipm_search_context", "aipm_search_discussions",
    "aipm_list_tasks", "aipm_get_task", "aipm_list_commits", "aipm_get_briefing",
    "aipm_list_plans", "aipm_get_commit", "aipm_get_plan", "aipm_smart_search",
    "aipm_list_sessions", "aipm_get_bug", "aipm_get_decision", "aipm_trace_context",
    "aipm_list_bugs", "aipm_list_decisions", "aipm_daily_review", "aipm_analyze",
    "aipm_suggest_threads",
}
# 深度查证类（深度查证占比分子：为结论/决策查依据；≠自发意图）
PROACTIVE = {
    "aipm_search_context", "aipm_smart_search", "aipm_get_decision",
    "aipm_get_plan", "aipm_get_bug", "aipm_get_task", "aipm_get_commit",
}

MCP_RE = re.compile(r"\[(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})\] \[MCP\] tool=(\S+) status=(\S+) src=(\S+)")

def load_calls():
    calls = []
    files = sorted(LOGS_DIR.glob("aipmc.log*"))
    for f in files:
        if f.name.endswith(".log") or ".log." in f.name:
            with open(f, errors="replace") as fh:
                for line in fh:
                    m = MCP_RE.search(line)
                    if m:
                        calls.append({
                            "date": m.group(1), "time": m.group(2),
                            "tool": m.group(3), "status": m.group(4), "src": m.group(5),
                        })
    return calls

def shannon(counts):
    total = sum(counts.values())
    if total == 0:
        return 0.0
    return -sum((c / total) * math.log2(c / total) for c in counts.values())

def window_stats(calls, label):
    aipm_calls = [c for c in calls if c["tool"].startswith("aipm")]
    total = len(aipm_calls)
    retr = sum(1 for c in aipm_calls if c["tool"] in RETRIEVAL)
    proact = sum(1 for c in aipm_calls if c["tool"] in PROACTIVE)
    by_tool = Counter(c["tool"] for c in aipm_calls)
    return {
        "window": label,
        "total_calls": total,
        "retrieval_ratio": round(retr / total, 4) if total else 0,
        "diversity_unique_tools": len(by_tool),
        "diversity_shannon": round(shannon(by_tool), 3),
        "deep_verify_ratio": round(proact / total, 4) if total else 0,
        "vision_calls": sum(1 for c in calls if c["tool"] == "aipmc_vision"),
        "test_tool_calls": sum(1 for c in calls if c["tool"] == "test_tool"),
    }

def main():
    calls = load_calls()
    base = [c for c in calls if c["date"] <= "2026-08-26" and c["date"] >= "2026-08-14"]
    point = [c for c in calls if c["date"] == "2026-08-27"]
    stats = {
        "generated_at": "2026-08-27T14:40:00+08:00",
        "source": "[MCP] 日志（aipmc.log + 归档）",
        "baseline": window_stats(base, "8/14-8/26"),
        "point_day": window_stats(point, "8/27"),
    }
    # 按天 + 按工具 top + 按 src + 按 status
    stats["by_day"] = {d: len([c for c in calls if c["date"] == d]) for d in sorted({c["date"] for c in calls})}
    # 日志缺口与窗口外数据（如实标注，防日均口径误读）
    stats["log_gap_days"] = sorted(d for d in ("2026-08-15", "2026-08-16", "2026-08-21", "2026-08-22", "2026-08-23") if d not in stats["by_day"])
    stats["pre_window_8-12_8-13_calls"] = len([c for c in calls if c["date"] in ("2026-08-12", "2026-08-13")])
    active_days = [d for d in stats["by_day"] if "2026-08-14" <= d <= "2026-08-26"]
    stats["baseline_daily_avg_active_days"] = round(stats["baseline"]["total_calls"] / len(active_days), 1) if active_days else 0
    stats["baseline_active_days"] = len(active_days)
    stats["by_tool_top"] = dict(Counter(c["tool"] for c in calls).most_common(15))
    stats["by_src"] = dict(Counter(c["src"] for c in calls))
    stats["by_status"] = dict(Counter(c["status"] for c in calls))

    OUT.write_text(json.dumps(stats, ensure_ascii=False, indent=2))
    print("== M 线基线（[MCP] 日志口径）==")
    for w in ("baseline", "point_day"):
        s = stats[w]
        print(f"{s['window']}: 总调用={s['total_calls']} 检索占比={s['retrieval_ratio']:.1%} "
              f"多样性={s['diversity_unique_tools']}工具(H={s['diversity_shannon']}) 深度查证占比={s['deep_verify_ratio']:.1%}")
    print("按天:", json.dumps(stats["by_day"]))
    print(f"日志缺口日(无任何活动记录): {stats['log_gap_days']}  8/12-13(窗口外): {stats['pre_window_8-12_8-13_calls']}")
    print(f"基线有数据日: {stats['baseline_active_days']} 天, 日均(按活跃日): {stats['baseline_daily_avg_active_days']}")
    print("按 src:", json.dumps(stats["by_src"]))
    print("按 status:", json.dumps(stats["by_status"]))
    print("Top 工具:", json.dumps(stats["by_tool_top"], ensure_ascii=False))
    print("输出:", OUT)

if __name__ == "__main__":
    main()
