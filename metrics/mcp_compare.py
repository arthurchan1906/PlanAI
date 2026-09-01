#!/usr/bin/env python3
"""M 线产出物 3 预脚本（9/3+ 数据窗口开启后回测）。

比较「后窗口」与「冻结基线 8/14-8/26」的同口径 MCP 数量/质量指标，
并附带 M0 per-session 漏录率（供 bug-158f8e 跨天大样本复核）。

口径沿用 mcp_baseline.py（[MCP] 日志为权威计数源，P1a decision-20260827-131338-c95787）。
M0 漏录率由 Go 权威工具产出（aipmc metrics --baseline --since ...），本脚本解析其输出并合并，
保证与 ad748b 收口时口径一致。

口径说明（bug-20260901-141137-acfabb 已修复）：aipmc metrics --baseline 现按
~/.aipmc/projects.json 注册的全部项目库 + 当前项目库聚合 discussion_log 后再与全局 [LLM]
日志对账，跨项目 session 不再误报漏录。注意：仅当所有活跃项目都在注册表内时漏录率才准确；
若某活跃项目未注册，其 session 仍会被计为漏录。

用法:
  python3 metrics/mcp_compare.py                     # since 默认 2026-08-29（排除 8/27-8/28 点破头2天）
  python3 metrics/mcp_compare.py --since 2026-08-29 --until 2026-09-03
  python3 metrics/mcp_compare.py --no-m0             # 只比 MCP 指标，不调 Go 工具
"""
import argparse, json, re, subprocess, sys
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from mcp_baseline import load_calls, window_stats, Counter

HERE = Path(__file__).parent
BASE_JSON = HERE / "mcp_baseline_2026-08-27.json"
DEFAULT_SINCE = "2026-08-29"      # 排除 8/27-8/28 点破效应（见 METRICS_BASELINE §4/§6）
DEFAULT_AIPMC = "dist/aipmc"

# 采集 Go 工具 metrics --baseline 输出的关键区块
M0_COVER_RE = re.compile(r"^\s*(\S+)\s+总 (\d+) \| 带 session (\d+) \| 空 session (\d+) \| 探针 (\d+)")
M0_UNDER_RE = re.compile(r"^\s*(\S+)\s+(\d+)/(\d+) session 漏录（([\d.]+)%）")
M0_ORPHAN_RE = re.compile(r"^\s*(\S+)\s+(\d+)/(\d+) session 无对应 LLM 请求")


def bounded_calls(calls, since, until):
    return [c for c in calls if since <= c["date"] <= until]


def active_days(calls, since, until):
    return sorted({c["date"] for c in calls if since <= c["date"] <= until})


def run_m0(aipmc, since):
    """解析 aipmc metrics --baseline --since 的 M0 块；失败返回 {} + reason。"""
    try:
        r = subprocess.run(
            [aipmc, "metrics", "--baseline", "--since", f"{since}T00:00:00", "--skip_write"],
            capture_output=True, text=True, timeout=180,
        )
    except FileNotFoundError:
        return {"error": f"aipmc 二进制不存在: {aipmc}"}
    except Exception as e:  # noqa: BLE001
        return {"error": str(e)}
    if r.returncode != 0:
        return {"error": r.stderr.strip()[-400:]}

    m0 = {"_cmd": f"{aipmc} metrics --baseline --since {since}T00:00:00 --skip_write"}
    for line in r.stdout.splitlines():
        m = M0_COVER_RE.search(line)
        if m:
            m0.setdefault("llm_session_coverage", {})[m.group(1)] = {
                "total": int(m.group(2)), "with_session": int(m.group(3)),
                "empty_session": int(m.group(4)), "probe": int(m.group(5)),
            }
        m = M0_UNDER_RE.search(line)
        if m:
            m0.setdefault("underreport", {})[m.group(1)] = {
                "missing": int(m.group(2)), "total": int(m.group(3)),
                "missing_rate": round(float(m.group(4)) / 100, 4),
            }
        m = M0_ORPHAN_RE.search(line)
        if m:
            m0.setdefault("reverse_orphan", {})[m.group(1)] = {
                "missing": int(m.group(2)), "total": int(m.group(3)),
            }
    return m0


def pct(a, b):
    if b in (None, 0):
        return None
    return round((a / b - 1) * 100, 1)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--since", default=DEFAULT_SINCE,
                    help=f"对比窗口起始日 YYYY-MM-DD（默认 {DEFAULT_SINCE}，排除点破头2天）")
    ap.add_argument("--until", default=None, help="对比窗口结束日（默认=日志最新日）")
    ap.add_argument("--aipmc", default=DEFAULT_AIPMC, help="aipmc 二进制路径")
    ap.add_argument("--base-json", default=str(BASE_JSON))
    ap.add_argument("--out", default=None, help="输出 JSON 路径（默认 metrics/mcp_compare_<since>_<until>.json）")
    ap.add_argument("--no-m0", action="store_true", help="跳过 M0 漏录率（不调 Go 工具）")
    args = ap.parse_args()

    base = json.load(open(args.base_json))
    calls, errs = load_calls()
    dates = sorted({c["date"] for c in calls})
    since = args.since
    until = args.until or dates[-1]

    cur = bounded_calls(calls, since, until)
    cur_stats = window_stats(cur, f"{since}→{until}")
    base_stats = base["baseline"]

    adays = active_days(calls, since, until)
    gap_days = [d for d in dates if since <= d <= until and d not in adays]
    # 完整连续日（含未产生活动的日）——用于标注窗口是否完整
    cur_stats["active_days"] = len(adays)
    cur_stats["daily_avg_active_days"] = round(cur_stats["total_calls"] / len(adays), 1) if adays else 0
    cur_stats["log_gap_days"] = gap_days
    cur_stats["by_error_type"] = dict(Counter(e["type"] for e in errs))

    base_daily = base.get("baseline_daily_avg_active_days") or (
        round(base_stats["total_calls"] / base.get("baseline_active_days", 1), 1))

    compare = {
        "generated_at": datetime.now().strftime("%Y-%m-%dT%H:%M:%S%z"),
        "window": f"{since}→{until}",
        "source": "[MCP] 日志 + aipmc metrics --baseline",
        "anchors": {
            "total_calls": {
                "baseline": base_stats["total_calls"], "current": cur_stats["total_calls"],
                "delta_pct": pct(cur_stats["total_calls"], base_stats["total_calls"]),
            },
            "daily_avg_active_days": {
                "baseline": base_daily, "current": cur_stats["daily_avg_active_days"],
                "delta_pct": pct(cur_stats["daily_avg_active_days"], base_daily),
            },
            "retrieval_ratio": {
                "baseline": base_stats["retrieval_ratio"], "current": cur_stats["retrieval_ratio"],
            },
            "diversity_unique_tools": {
                "baseline": base_stats["diversity_unique_tools"],
                "current": cur_stats["diversity_unique_tools"],
            },
            "deep_verify_ratio": {
                "baseline": base_stats["deep_verify_ratio"], "current": cur_stats["deep_verify_ratio"],
            },
        },
        "current_window": cur_stats,
    }

    if not args.no_m0:
        compare["m0"] = run_m0(args.aipmc, since)
        compare["m0_caveat"] = (
            "M0 漏录率现按注册表跨项目聚合（bug-20260901-141137-acfabb 已修复），不再把跨项目 "
            "claude/codex session 误计为漏录；残余漏录为真实捕获缺口（罕见短/一次性会话）。"
            "注意：所有活跃项目须在 ~/.aipmc/projects.json 注册，否则其 session 仍会被计为漏录。"
        )

    out = Path(args.out) if args.out else HERE / f"mcp_compare_{since}_{until}.json"
    out.write_text(json.dumps(compare, ensure_ascii=False, indent=2))

    print("== M 线 7 天对比（[MCP] 日志口径）==")
    print(f"窗口 {since}→{until} | 活跃日 {cur_stats['active_days']} | 日均(活跃日) {cur_stats['daily_avg_active_days']}")
    print(f"总调用: 基线 {base_stats['total_calls']} → 当前 {cur_stats['total_calls']} "
          f"({compare['anchors']['total_calls']['delta_pct']}%)")
    print(f"检索占比: 基线 {base_stats['retrieval_ratio']:.1%} → 当前 {cur_stats['retrieval_ratio']:.1%}")
    print(f"多样性: 基线 {base_stats['diversity_unique_tools']} 工具(H={base_stats['diversity_shannon']}) "
          f"→ 当前 {cur_stats['diversity_unique_tools']} 工具(H={cur_stats['diversity_shannon']})")
    print(f"深度查证占比: 基线 {base_stats['deep_verify_ratio']:.1%} → 当前 {cur_stats['deep_verify_ratio']:.1%}")
    print(f"窗口内缺口日(无活动): {gap_days}")
    if "m0" in compare:
        m0 = compare["m0"]
        if "error" in m0:
            print("M0 漏录率: ⚠", m0["error"])
        else:
            print("M0 per-session 漏录率（bug-158f8e 跨天复核）:")
            for agent, u in m0.get("underreport", {}).items():
                print(f"  {agent}: {u['missing']}/{u['total']} = {u['missing_rate']:.1%}")
            for agent, c in m0.get("llm_session_coverage", {}).items():
                print(f"  [LLM] {agent}: 总{c['total']} 空session{c['empty_session']}")
            for agent, o in m0.get("reverse_orphan", {}).items():
                print(f"  反向对账 {agent}: 脱链 {o['missing']}/{o['total']}")
            print("  ⚠ 漏录率按注册表跨项目聚合（bug-141137 已修复）；确保活跃项目均已注册，见 m0_caveat")
    print("输出:", out)


if __name__ == "__main__":
    main()
