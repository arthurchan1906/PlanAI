#!/usr/bin/env python3
"""新项目接入探针：首会话 AIPM 使用 + 接入快照（项目级 A/B 的测量工具）。

用途（task-20260909-102039-092ca3 项目级设计 v2）：
  单元 = 项目；主结局 = 新项目「前 3 会话内是否**自发**调用 aipm_* 工具」（0/1，机器判定）；
  辅以首会话分列 + 接入快照（二进制/配置/指南层/skill），使臂归属与版本可事后审计。

口径（预注册写死，勿改）：
  - claude 的工具调用落 role='tool' 行；codex 的落 role='assistant' 行（📡）——分列，不混算。
  - 「自发」= 有调用 **且** 该调用之前的用户轮次不含 aipm/pmai 关键词（纯字符串判定，
    排除用户在会话里明说「记到 AIPM」造成的污染；见 codex 2026-09-22 复核 #3）。
  - 干预装法：**项目级** `<项目>/.claude/skills/aipm-usage/SKILL.md`（已验证可加载，
    vf-20260922-114301-b7dcc5）；全局路径仅作审计字段记录。

用法：
  python3 metrics/first_session_probe.py /path/to/project [--json]

只读（mode=ro），不改动目标项目任何文件。
"""
import argparse
import hashlib
import json
import re
import sqlite3
import sys
from datetime import datetime
from pathlib import Path

TOOL_RE = re.compile(r"(?:🛠\s*mcp__aipm__|📡\s*)(aipm_[a-z0-9_]+)")
USER_MENTION_RE = re.compile(r"aipm|pmai", re.I)  # 用户轮次中的干预关键词
CLAUDE = "claude-code"
CODEX = "codex-cli"


def sha12(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()[:12]


def _skill_state(p: Path) -> dict:
    return {"installed": p.exists(), "sha12": sha12(p) if p.exists() else None}


def snapshot(project: Path, aipmc_bin: str) -> dict:
    snap = {
        "project": str(project),
        "captured_at": datetime.now().isoformat(timespec="seconds"),
        "aipmc_bin": None,
        "mcp_configured": None,
        "guidelines_md": None,
        "claude_md": None,
        "skill_project": None,
        "skill_global": None,
    }
    b = Path(aipmc_bin)
    if b.exists():
        snap["aipmc_bin"] = {"path": str(b), "sha12": sha12(b),
                             "mtime": datetime.fromtimestamp(b.stat().st_mtime).isoformat(timespec="seconds")}
    for name in (".claude/settings.local.json", ".claude/settings.json", ".mcp.json"):
        p = project / name
        if p.exists() and "aipm" in p.read_text(errors="replace"):
            snap["mcp_configured"] = name
            break
    if snap["mcp_configured"] is None:
        snap["mcp_configured"] = False
    g = project / ".pmai" / "guidelines.md"
    snap["guidelines_md"] = {"exists": g.exists(), "chars": len(g.read_text(errors="replace")) if g.exists() else 0}
    c = project / "CLAUDE.md"
    snap["claude_md"] = {"exists": c.exists(), "chars": len(c.read_text(errors="replace")) if c.exists() else 0}
    # 干预对象：项目级为主（设计口径），全局仅作审计
    snap["skill_project"] = _skill_state(project / ".claude" / "skills" / "aipm-usage" / "SKILL.md")
    snap["skill_global"] = _skill_state(Path.home() / ".claude" / "skills" / "aipm-usage" / "SKILL.md")
    return snap


def _session_stats(con, src: str, sid: str) -> dict:
    call_role = "tool" if src == CLAUDE else "assistant"
    rows = con.execute(
        "SELECT created_at, content FROM discussion_log WHERE source=? AND session_id=? AND role=? ORDER BY created_at",
        (src, sid, call_role)).fetchall()
    calls, first_ts = [], None
    for ts, c in rows:
        m = TOOL_RE.search(c or "")
        if m:
            calls.append(m.group(1))
            if first_ts is None:
                first_ts = ts
    # 用户诱导污染：首个 aipm 调用之前的用户轮次是否提及 aipm/pmai
    user_prompted = False
    if first_ts:
        for ts, c in con.execute(
                "SELECT created_at, content FROM discussion_log WHERE source=? AND session_id=? AND role='user' ORDER BY created_at",
                (src, sid)):
            if ts < first_ts and USER_MENTION_RE.search(c or ""):
                user_prompted = True
                break
    return {
        "session_id": sid,
        "aipm_calls": len(calls),
        "tool_rows": len(rows),
        "first_call_at": first_ts,
        "user_prompted": user_prompted,
        "spontaneous": len(calls) > 0,                        # 原始
        "spontaneous_strict": len(calls) > 0 and not user_prompted,  # 预注册口径
        "top_tools": sorted({t: calls.count(t) for t in calls}.items(), key=lambda kv: -kv[1])[:5],
    }


def first_sessions(db: Path, n: int = 3) -> dict:
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    con.text_factory = lambda b: b.decode("utf-8", "replace") if isinstance(b, bytes) else b
    out = {"db": str(db), "sessions_total": con.execute("SELECT COUNT(DISTINCT session_id) FROM discussion_log").fetchone()[0]}
    for src in (CLAUDE, CODEX):
        sids = [r[0] for r in con.execute(
            "SELECT session_id FROM discussion_log WHERE source=? AND session_id NOT IN ('unknown','') "
            "GROUP BY session_id ORDER BY MIN(created_at) ASC LIMIT ?",
            (src, n))]
        sess = [_session_stats(con, src, s) for s in sids]
        out[src] = {
            "sessions": sess,
            "first_session": sess[0] if sess else None,
            "first_n_spontaneous_strict": any(s["spontaneous_strict"] for s in sess) if sess else None,  # 主结局
        }
    con.close()
    return out


def render(snap: dict, fs: dict) -> str:
    L = ["== 接入快照 =="]
    b = snap["aipmc_bin"]
    L.append(f"  项目: {snap['project']}  （{snap['captured_at']}）")
    L.append(f"  aipmc 二进制: {b['sha12']} @{b['mtime']}" if b else "  aipmc 二进制: 未找到")
    L.append(f"  MCP 配置: {snap['mcp_configured']}")
    L.append(f"  guidelines.md: {'有 %d 字' % snap['guidelines_md']['chars'] if snap['guidelines_md']['exists'] else '无'}"
             f"   CLAUDE.md: {'有 %d 字' % snap['claude_md']['chars'] if snap['claude_md']['exists'] else '无'}")
    sp, sg = snap["skill_project"], snap["skill_global"]
    L.append(f"  干预(skill): 项目级={'已装 ' + sp['sha12'] if sp['installed'] else '未装'}"
             f"  全局={'已装 ' + sg['sha12'] if sg['installed'] else '未装'}")
    L.append("")
    L.append(f"== 前 3 会话 AIPM 使用（会话总数 {fs['sessions_total']}）==")
    for src in (CLAUDE, CODEX):
        d = fs.get(src)
        if not d or not d["sessions"]:
            L.append(f"  [{src}] 无会话")
            continue
        fs0 = d["first_session"]
        L.append(f"  [{src}] 主结局（前3会话严格自发）= {'是' if d['first_n_spontaneous_strict'] else '否'}")
        for s in d["sessions"]:
            tag = "严格自发" if s["spontaneous_strict"] else ("用户提及" if s["user_prompted"] else "未自发")
            L.append(f"      {s['session_id'][:8]}  调用 {s['aipm_calls']:>3} / 行 {s['tool_rows']:>4}  [{tag}]"
                     + ("  " + ", ".join(f"{t}×{n}" for t, n in s["top_tools"]) if s["top_tools"] else ""))
    return "\n".join(L)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("project", help="目标项目路径（含 .pmai/data/pmai.db）")
    ap.add_argument("--aipmc", default="dist/aipmc", help="aipmc 二进制路径（默认 dist/aipmc）")
    ap.add_argument("--json", action="store_true", help="输出 JSON")
    args = ap.parse_args()
    project = Path(args.project).expanduser().resolve()
    db = project / ".pmai" / "data" / "pmai.db"
    if not db.exists():
        print(f"[warn] 未找到 {db}——只输出快照", file=sys.stderr)
    snap = snapshot(project, args.aipmc)
    fs = first_sessions(db) if db.exists() else {"db": str(db), "sessions_total": 0}
    if args.json:
        print(json.dumps({"snapshot": snap, "first_sessions": fs}, ensure_ascii=False, indent=2))
    else:
        print(render(snap, fs))


if __name__ == "__main__":
    main()
