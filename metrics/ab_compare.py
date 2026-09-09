#!/usr/bin/env python3
"""路2 A/B 测量脚手架：固定窗口 before/after 行为维度对比 + 独立结果。

用途：北星 task-69ed87 路2 A/B（同 agent claude skill 开/关）的测量脚本。

原则（METRICS_REPORT-2026-09-03 §0 / decision-20260908-151649 / BEHAVIOR_BASELINE）：
  - 固定窗口可复算：一律 --since/--until，禁止 all 窗口（盲试分母随日志漂移 4880→4916）。
  - 行为维度（B10/B11/B12+同行感知）= 可复算信号，由 `aipmc metrics --behavior --json` 权威产出。
  - D1 自发率（双目标：纯自发≈0%↑ + 含半自发 15%→30%）依赖**人工双标 + gold**，本脚本
    不臆造；除非 `--d1-gold <文件>` 提供了标注集，否则只在占位处提示「需人工标注」。
  - 排除 auto：D1 标注层做；独立结果（task done / done_gate / 孤儿绑定）的 review_status=auto
    不作成绩，本脚本对这类数字显式标注口径。

用法：
  python3 metrics/ab_compare.py \
      --before-since 2026-08-29 --before-until 2026-09-03 \
      --after-since  2026-09-04 --after-until  2026-09-08
  python3 metrics/ab_compare.py --no-results   # 只比行为维度，不解析独立结果
"""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

DEFAULT_AIPMC = "dist/aipmc"

# 行为维度键 → 中文名（BEHAVIOR_BASELINE 口径）
DIM_LABEL = {
    "parse_coverage": "解析覆盖率",
    "retrieval_awareness": "历史检索意识",
    "planfulness": "计划性",
    "peer_awareness": "同行感知",
    "blind_try": "盲试检测",
}
# 独立结果指标（来自 aipmc metrics 文本）→ 正则
IND_RE = {
    "task_completion_rate": re.compile(r"E7\s+task_completion_rate\s+([\d.]+)%\s+\((\d+)/(\d+)\)"),
    "orphan_rate": re.compile(r"orphan_rate\s+([\d.]+)%\s+\((\d+)/(\d+)\)"),
    "done_gate": re.compile(r"E9\s+done_gate\s+pass=(\d+)\s+reject=(\d+)"),
}


def run_behavior(aipmc, since, until):
    """跑 fixed-window 行为基线，返回 JSON dict；失败返回 {} + reason。"""
    if not since or not until:
        return {"error": "行为基线要求同时给定 --since 与 --until"}
    cmd = [aipmc, "metrics", "--behavior", "--since", since, "--until", until, "--json"]
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=180)
    except FileNotFoundError:
        return {"error": f"aipmc 不存在: {aipmc}"}
    except Exception as e:  # noqa: BLE001
        return {"error": str(e)}
    out = r.stdout.strip()
    if not out:
        return {"error": f"没有 stdout（stderr: {r.stderr[:200]}）"}
    try:
        return json.loads(out)
    except json.JSONDecodeError:
        return {"error": f"非 JSON 输出（首 200 字: {out[:200]}）"}


def run_ind(aipmc):
    """解析 aipmc metrics 文本中的独立结果指标（排除 auto 口径由调用方注记）。"""
    r = subprocess.run([aipmc, "metrics"], capture_output=True, text=True, timeout=120)
    text = r.stdout
    ind = {}
    for name, pat in IND_RE.items():
        m = pat.search(text)
        if m:
            ind[name] = m.groups()
    return ind


def pct(r):
    """ratio → 百分比字符串（0.515 → 51.5%）。"""
    return f"{r * 100:.1f}%" if r is not None else "—"


def dim_before_after(before, after):
    rows = []
    for key, label in DIM_LABEL.items():
        b = before.get(key)
        a = after.get(key)
        if isinstance(b, dict):
            br = b.get("ratio")
            ar = a.get("ratio") if isinstance(a, dict) else None
            delta = "—" if (br is None or ar is None) else f"{((ar - br) * 100):+.1f}pp"
            rows.append((label, pct(br), pct(ar), delta))
        else:
            br, ar = b, a
            delta = "—" if (br is None or ar is None) else f"{((ar - br) * 100):+.1f}pp"
            rows.append((label, pct(br), pct(ar), delta))
    return rows


def fmt_table(rows):
    lines = [f"{'指标':<14}{'before':>10}{'after':>10}{'Δ':>10}"]
    for label, b, a, d in rows:
        lines.append(f"{label:<14}{b:>10}{a:>10}{d:>10}")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--aipmc", default=DEFAULT_AIPMC)
    ap.add_argument("--before-since", required=True, help="A/B before 窗口起始（如 2026-08-29）")
    ap.add_argument("--before-until", required=True, help="A/B before 窗口结束")
    ap.add_argument("--after-since", required=True, help="A/B after 窗口起始")
    ap.add_argument("--after-until", required=True, help="A/B after 窗口结束")
    ap.add_argument("--no-results", action="store_true", help="不解析独立结果指标")
    ap.add_argument("--d1-gold", default=None, help="D1 双标/gold 标注 JSON（可选）")
    args = ap.parse_args()

    before = run_behavior(args.aipmc, args.before_since, args.before_until)
    after = run_behavior(args.aipmc, args.after_since, args.after_until)
    if "error" in before or "error" in after:
        sys.exit(f"行为基线失败: before={before.get('error')} after={after.get('error')}")

    print("== 路2 A/B 行为维度（固定窗口可复算）==")
    print(f"before: {args.before_since}→{args.before_until}")
    print(f"after : {args.after_since}→{args.after_until}")
    print(f"会话/调用 before: {before.get('total_sessions')}/{before.get('total_calls')}  "
          f"after: {after.get('total_sessions')}/{after.get('total_calls')}")
    print()
    print(fmt_table(dim_before_after(before, after)))
    print()

    if args.d1_gold:
        try:
            gold = json.loads(Path(args.d1_gold).read_text())
            print("== D1 双目标（来自 gold 标注集）==")
            print(json.dumps(gold, ensure_ascii=False, indent=2))
        except Exception as e:  # noqa: BLE001
            print(f"读取 --d1-gold 失败: {e}")
    else:
        print("== D1 双目标（纯自发↑ / 含半自发 15%→30%）==")
        print("  [需人工双标] 本脚手架不臆造自发率；请按 D1 协议提供标注集（--d1-gold）。")
        print("  口径红线：排除 review_status=auto、分列双目标、禁止用执行率当成绩。")
    print()

    if args.no_results:
        return
    ind = run_ind(args.aipmc)
    print("== 独立结果指标（项目级；auto 口径见注）==")
    if ind.get("task_completion_rate"):
        p, ok, tot = ind["task_completion_rate"]
        print(f"task_completion_rate: {p}% ({ok}/{tot})")
    if ind.get("orphan_rate"):
        p, n, tot = ind["orphan_rate"]
        print(f"orphan_rate: {p}% ({n}/{tot})")
    if ind.get("done_gate"):
        p, rj = ind["done_gate"]
        print(f"done_gate: pass={p} reject={rj}")
    if not ind:
        print("  （未解析到；建议核对 aipmc metrics 输出或改用 --no-results）")
    print("  注：独立结果中 review_status=auto 的 commit 不作成绩；D1 双标层需显式排除 auto。")


if __name__ == "__main__":
    main()
