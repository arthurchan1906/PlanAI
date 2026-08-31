#!/usr/bin/env python3
"""D1 自发率归因协议 — 抽样脚本。

从 aipmc + ED 双库 discussion_log 抽取 user turn 试点/正式样本。
- 排除点破效应日（>= 2026-08-27）
- 只抽 codex-cli + claude-code（用户明确 cursor/opencode 暂不考量）
- 按 项目×agent 分层轮询
- 📡 aipm_* 行去重（每个调用落 ✅结果 + 裸行 两行）

用法:
  python3 metrics/d1_sampling.py [--n 20] [--cutoff 2026-08-27T00:00:00]
      [--out metrics/d1_annotation_pilot.json]
"""
import sqlite3, json, sys, random, argparse

def conn(dbpath):
    db = sqlite3.connect(dbpath)
    db.text_factory = lambda b: b.decode('utf-8', errors='replace')
    return db

def extract_turns(dbpath, agent_filter, cutoff):
    db = conn(dbpath)
    rows = db.execute("""
        SELECT d1.id, d1.session_id, d1.content, d1.created_at, d1.source
        FROM discussion_log d1
        WHERE d1.role='user' AND d1.source IN ({}) AND d1.created_at < ?
        ORDER BY d1.created_at ASC
    """.format(",".join("?"*len(agent_filter))), agent_filter+[cutoff]).fetchall()
    turns = []
    for uid, sid, ucontent, uat, usrc in rows:
        try:
            nxt = db.execute("""SELECT min(created_at) FROM discussion_log
                WHERE session_id=? AND role='user' AND created_at > ?""", (sid, uat)).fetchone()[0]
        except Exception:
            continue
        end = nxt if nxt else '2999-12-31T23:59:59'
        tools = db.execute("""
            SELECT content FROM discussion_log
            WHERE session_id=? AND role='assistant' AND content LIKE '📡 aipm_%'
              AND created_at > ? AND created_at < ?
            ORDER BY created_at ASC""", (sid, uat, end)).fetchall()
        seen = {}
        for (c,) in tools:
            body = c.replace('📡 ','').strip()
            for sym in ('✅','❌'):
                if sym in body: body = body.split(sym)[0].strip()
            if body and body not in seen:
                seen[body] = body
        tool_seq = list(seen.keys())
        if not tool_seq: continue
        turns.append({"project":"aipmc" if "aipmc" in dbpath else "ed",
            "agent":usrc,"session":sid,"user_msg":ucontent.strip(),
            "turn_start":uat,"mcp_seq":tool_seq})
    db.close()
    return turns

def try_sample(turns, n, seed=42):
    random.seed(seed)
    groups = {}
    for i,t in enumerate(turns):
        t["idx"]=i
        groups.setdefault((t["project"],t["agent"]),[]).append(t)
    for lst in groups.values(): random.shuffle(lst)
    keys=list(groups.keys()); picked=[]
    while len(picked)<n:
        made=0
        for k in keys:
            if len(picked)>=n: break
            if groups[k]: picked.append(groups[k].pop(0)); made+=1
        if made==0: break
    return picked

def main():
    ap=argparse.ArgumentParser()
    ap.add_argument('--n', type=int, default=20)
    ap.add_argument('--cutoff', default='2026-08-27T00:00:00')
    ap.add_argument('--out', default='metrics/d1_annotation_pilot.json')
    ap.add_argument('--seed', type=int, default=42)
    ap.add_argument('--dbs', nargs='*', default=[
        '/Users/dazsec/workspace/aipmc/.pmai/data/pmai.db',
        '/Users/dazsec/projects/EncryptDrive/.pmai/data/pmai.db'])
    args=ap.parse_args()
    agents=['codex-cli','claude-code']
    all_turns=[]
    for db in args.dbs:
        ts=extract_turns(db, agents, args.cutoff)
        print(f"{db}: {len(ts)} 有效 turn", file=sys.stderr)
        all_turns+=ts
    picked=try_sample(all_turns, args.n, args.seed)
    print(f"# 总有效={len(all_turns)} 采样={len(picked)}", file=sys.stderr)
    json.dump({"cutoff":args.cutoff,"n":len(picked),"samples":picked},
              open(args.out,'w'), ensure_ascii=False, indent=2)
    print(f"已写出 {args.out} ({len(picked)} 条)")

if __name__=='__main__': main()
