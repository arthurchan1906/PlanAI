#!/usr/bin/env python3
"""D1 试点标注一致率计算。
用法: python3 metrics/d1_agreement.py [codex_json] [claude_json]
codex/后续 claude 判定均为 {annotator,date,labels:[{idx,project,agent,turn,label,note}]}
一致 = 主标签完全相同(label 相等)。"
"""
import json, sys
from collections import Counter, defaultdict

def load(p):
    d=json.load(open(p))
    if isinstance(d,dict): return d.get('labels',[])
    return d

def key(r): return (r.get('idx'), r.get('project'), r.get('agent'), r.get('turn')[:16])

def main():
    codex_p = sys.argv[1] if len(sys.argv)>1 else 'metrics/d1_codex_labels.json'
    claude_p = sys.argv[2] if len(sys.argv)>2 else None
    codex = load(codex_p)
    if not claude_p:
        print(f"codex 判定载入: {len(codex)} 条 (待 claude 复标)")
        print(Counter(x['label'] for x in codex))
        return
    claude = load(claude_p)
    cmap={key(r):r['label'] for r in claude}
    agree=defaultdict(lambda:[0,0])
    total=0; agree_total=0; disagree=[]
    for r in codex:
        k=key(r); cl=cmap.get(k)
        if cl is None:
            print(f"警告: idx {r['idx']} 无 claude 对应判定"); continue
        total+=1
        grp=(r['project'],r['agent'])
        c=cl==r['label']
        agree[grp][0]+=1 if c else 0; agree[grp][1]+=1
        agree_total+=1 if c else 0
        if not c: disagree.append((r['idx'],r['label'],cl))
    print("="*50)
    print(f"D1 试点一致率 (严格 label 相同)")
    print(f"总计: {agree_total}/{total} = {agree_total/total:.1%}")
    print("\n按 项目×agent:")
    for g in sorted(agree):
        a,t=agree[g]
        print(f"  {g}: {a}/{t} = {a/t:.1%}" if t else f"  {g}: 0")
    print("\n分歧样本 (codex vs claude):")
    for i,ca,cl in disagree:
        print(f"  #{i}: codex={ca} vs claude={cl}")
    print(f"\n分歧数: {len(disagree)}")

if __name__=='__main__': main()
