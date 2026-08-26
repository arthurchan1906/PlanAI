# PITFALLS（实测陷阱记录）

> 每次踩坑后记录根因与正确姿势，供后续实现/审核引用。2026-08-26 起。

## 1. store 写路径测试必须 PMAI_HOME 隔离（2026-08-26，S1 实证）

`store` 包的 spool/写路径测试若不加 `setupDailyDB(t)`（`t.Setenv("PMAI_HOME", t.TempDir())`），
`discussionSpoolPath()`/`pmdb.FindPath()` 会从测试 cwd 向上解析到**真实 `.pmai/cache/` 与生产库**：
测试删除真实 spool、写入种子行，活跃 agent 的 flush 会把种子当真实待补写条目 INSERT 进生产库。

- 教训：任何涉及 `os.Remove(path)` / 写文件的 store 测试，第一行必须 `setupDailyDB(t)`。
- 实证：`TestSpoolDropsWhenFull` 漏隔离 → `discussion_log` 落 1 行 `id='seed'` 空数据 + `fts5_index` 1 条。

## 2. 清理 spool 污染：先清文件再清表（2026-08-26，P1 再实证）

删除生产表中 seed 行后，若 spool 文件里还留着垃圾条目，活跃 flush 会**再补写回去**（UNIQUE 不冲突）——
「DELETE 后验证 0 行」成立的瞬间就会被重新污染。

- 正确顺序：① 先删/清空 spool 文件（消除再补写源）→ ② 再 DELETE 表数据 → ③ 最后验证。
- 本次只做了 ②，seed 被 spool 垃圾再补写；spool 文件被后续 flush 逐批清空（absent）后才真正干净。

## 3. 兜底路径返回值必须与正常路径同构（2026-08-26，P1 panic）

`LogDiscussion` 兜底路径（spooled/dropped）曾不返回 `id` 键，而 `main.go` 用 `r["id"].(string)`
强断言 → 锁竞争兜底时 `aipmc log` 运行时 panic（恰在 P0 场景触发）。

- 教训：函数返回值结构变化时，全量扫描消费方；兜底路径要返回 id（spool 条目 id）或消费方安全提取。
- 修复：`spoolDiscussionFallback` 返回生成 id；`main.go`/`mcp.go` 对缺失 id 安全降级为 `-`。
