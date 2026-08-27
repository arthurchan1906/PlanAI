package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"aipmc/cli"
	pmdb "aipmc/db"
	"aipmc/store"
)

// dispatchMetrics implements `aipmc metrics` — a read-only, point-in-time
// check of EVALUATION.md targets. No JSON snapshots, no time series, no diff:
// v1 is just "how do we stand right now against the documented targets".
// DB-class metrics reflect the current project (cwd .pmai); log-class metrics
// cover the global proxy log (~/.aipmc/logs/aipmc.log) which has no project
// field yet (documented limitation, v2 adds it). 8/14 起日志按 20MB 自动归档
// （保留 7 份），log-class 指标只扫当前文件；历史窗口请用 aipmc.log.* 归档复核。
func dispatchMetrics(args *cli.Args) {
	window := args.Str("window", "") // optional: "24h" for log-class metrics only
	// commit 三件套窗口：默认只看修复后数据（StoreGitCommit 97ce814 起），
	// 避免历史污染（ED 547 空 hash、aipmc 历史孤儿）淹没告警信号。
	// --since all 看全表（存量状态）；--since 2026-08-01 自定义窗口。
	since := args.Str("since", "2026-08-07T14:00:00")
	// F1/F4 DB 类窗口（W3 8/13）：--since 仅作用于验收/诊断类行（D2 events、H2 rel_path），
	// 其他 DB 行保持全表=机制健康现状——不静默改变已有指标语义。
	dbSince := ""
	var dbSinceArgs []any
	if since != "" && since != "all" {
		dbSince = " AND created_at >= ?"
		dbSinceArgs = []any{since}
	}
	// --window 生效于日志类指标：解析为截止时间，扫描时跳过更早的行。
	// DB 类指标仅 F1/F4 支持窗口回算（见上），其余行始终为当前全表值。
	var cutoff time.Time
	var hasCutoff bool
	if window != "" {
		d, err := time.ParseDuration(window)
		if err != nil {
			fmt.Printf("⚠ 无效 --window %q（示例: 24h、72h）— 忽略窗口\n", window)
		} else {
			cutoff = time.Now().Add(-d)
			hasCutoff = true
		}
	}
	// --baseline: M0 捕获层完整性对账（独立命令，不走常规指标清单）。
	if args.Bool("baseline") {
		runBaseline(args)
		return
	}
	fmt.Println("AIPM 评估指标 — 目标值来自 docs/EVALUATION.md")
	fmt.Println("DB 类指标: 当前项目 point-in-time；日志类指标: ~/.aipmc/logs/aipmc.log（serve 行带 project= 标签，proxy/hook 行无；已按 20MB 归档，只扫当前文件）")
	fmt.Printf("窗口: since=%s（--since all 看全表；F1/F4 验收诊断行随窗口，其余 DB 行保持全表=机制健康现状）\n", since)
	if hasCutoff {
		fmt.Printf("日志窗口: %s（截止 %s）\n", window, cutoff.Format("2006-01-02 15:04:05"))
	}
	fmt.Println()

	// ── DB class (current project) ──
	db, err := pmdb.Open()
	if err == nil {
		var total, withL2, nested, mdBlock int
		// B1 分母口径（8/12 修正，写死）：分母=discussion_log 去重 session_id
		// （排除空/unknown）；分子=这些 session 中至少有一条非空 summary 的 session 数
		// （JOIN discussion_log 保证分子属于分母宇宙）。旧口径用 session_summaries 行数
		// 作分母会高估覆盖率（ED 实测 58% vs 真实 34%——session_summaries 只收录
		// 跑过 pipeline 的 session）。
		db.QueryRow(`SELECT
			(SELECT COUNT(DISTINCT session_id) FROM discussion_log WHERE session_id!='' AND session_id!='unknown'),
			(SELECT COUNT(DISTINCT s.session_id) FROM session_summaries s JOIN discussion_log d ON d.session_id=s.session_id WHERE s.summary!='' AND d.session_id!='' AND d.session_id!='unknown')`).Scan(&total, &withL2)
		// B2 双口径：nested_goal = goal 值本身是嵌套 JSON；md_block = 摘要含 ```json 代码块（不同缺陷，分开统计）。
		db.QueryRow("SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%\"goal\":\"{%'").Scan(&nested)
		db.QueryRow("SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%```json%'").Scan(&mdBlock)
		var evTotal, evProcessed int
		evWhere := ""
		var evArgs []any
		if dbSince != "" {
			evWhere = " WHERE created_at >= ?"
			evArgs = dbSinceArgs
		}
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(processed_by_agent),0) FROM events"+evWhere, evArgs...).Scan(&evTotal, &evProcessed)
		var evUnique int
		db.QueryRow("SELECT COUNT(DISTINCT type || '|' || entity_type || '|' || entity_id) FROM events"+evWhere, evArgs...).Scan(&evUnique)
		// F1 事件→动作漏斗（W2 8/13）：三口径拆分 + 按类型处理分布。
		// 免处理 = 生成即完成使命（创建通知/低置信建议）；可行动 = 需 agent 响应；
		// 诊断问题：处理分布是否集中于单一类型（8/13 实测 19 个已处理全为 commit_orphan）。
		type evTypeStat struct {
			typ       string
			total     int
			processed int
		}
		var evStats []evTypeStat
		if evRows, err := db.Query("SELECT type, COUNT(*), COALESCE(SUM(processed_by_agent),0) FROM events"+evWhere+" GROUP BY type ORDER BY COUNT(*) DESC", evArgs...); err == nil {
			for evRows.Next() {
				var es evTypeStat
				if err := evRows.Scan(&es.typ, &es.total, &es.processed); err == nil {
					evStats = append(evStats, es)
				}
			}
			evRows.Close()
		}
		// F1/D2 三口径（W2 8/13 + 8/26 统一）：免处理（生成即完成使命，不要求响应）/
		// 可行动（需 agent 响应）/已处理。D2 指标用**可行动**口径——参考性事件
		// （hotspot_untracked/tentative_link）设计上不要求处理动作，计入分母会
		// 虚假拉低处理率（8/26 讨论：低处理率≠管道堵，须区分必须处理类型）。
		evFree, evAction, evActionProc := 0, 0, 0
		freeNames := map[string]bool{"tentative_link": true, "task_created": true, "plan_created": true}
		actionNames := map[string]bool{"commit_orphan": true, "mcp_error": true, "hotspot_untracked": true}
		var actionDist []string
		for _, es := range evStats {
			switch {
			case freeNames[es.typ]:
				evFree += es.total
			case actionNames[es.typ]:
				evAction += es.total
				evActionProc += es.processed
				actionDist = append(actionDist, fmt.Sprintf("%s %d/%d", es.typ, es.processed, es.total))
			}
		}
		actionRate := 0.0
		if evAction > 0 {
			actionRate = float64(evActionProc) / float64(evAction)
		}
		// H2 metadata 健康（8/10 T2）：空串（对话消息本应空）与非法 JSON 分开统计；
		// valid_rate 分母排除空串——口径固定，防止与 tool role 缺失混为一谈。
		var dlTotal, dlValid, dlEmpty, dlInvalid int
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN metadata!='' AND json_valid(metadata) THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN metadata='' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN metadata!='' AND json_valid(metadata)=0 THEN 1 ELSE 0 END),0) FROM discussion_log").Scan(&dlTotal, &dlValid, &dlEmpty, &dlInvalid)
		// H2 rel_path 覆盖率（T3b+T4 闭环验收锚点，M1 8/13 修正口径）：
		// filetools = 结构化文件操作（claude type∈edit/new_file/read/write；
		// codex tool_name∈apply_patch/Write/Read），锚点 claude≥90%；
		// bash = 高置信模式才写 rel_path（git add/cat/wc/find...），锚点=参考
		// （决策 19 接受漏检）。分母若含 bash 则 90% 物理不可达（ED bash 48%）。
		var (
			ftClaudeTotal, ftClaudeHit, ftCodexTotal, ftCodexHit int
			bsClaudeTotal, bsClaudeHit, bsCodexTotal, bsCodexHit int
		)
		// H2 窗口（W3）：rel_path 验收随 --since（部署后数据），避免存量稀释假象。
		// F4 口径（W3 配套，8/13）：filetools 分母只计「项目内文件操作」——项目外路径
		// （/tmp、~/.claude/plans、其他项目）ToRelPath 按设计返回空，永远不可能有 rel_path，
		// 计入分母会虚假稀释 90% 锚点（ED 实测缺失 5/50 全为项目外）。bash 行保持原口径（参考）。
		dbPath, _ := pmdb.FindPath()
		projRoot := ""
		if strings.HasSuffix(dbPath, string(filepath.Separator)+".pmai"+string(filepath.Separator)+"data"+string(filepath.Separator)+"pmai.db") {
			projRoot = filepath.Dir(filepath.Dir(filepath.Dir(dbPath)))
		}
		ftCond := ""
		var ftArgs []any
		if projRoot != "" {
			ftCond = " AND (instr(json_extract(metadata,'$.file_path'), ? || '/') = 1 OR json_extract(metadata,'$.file_path') = ?)"
			ftArgs = []any{projRoot, projRoot}
		}
		h2Cond := " WHERE metadata != '' AND json_valid(metadata)"
		var h2Args []any
		if dbSince != "" {
			h2Cond += dbSince
			h2Args = dbSinceArgs
		}
		// 占位符顺序：SELECT 内 filetools 4 组条件（每组 2 个 ?）先于 WHERE 的 ?。
		ftArgs4 := append(append(append(append([]any{}, ftArgs...), ftArgs...), ftArgs...), ftArgs...)
		h2QueryArgs := append(ftArgs4, h2Args...)
		h2SQL := fmt.Sprintf(`SELECT
			COALESCE(SUM(CASE WHEN source='claude-code' AND json_extract(metadata,'$.type') IN ('edit','new_file','read','write')%s THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='claude-code' AND json_extract(metadata,'$.type') IN ('edit','new_file','read','write')%s AND (json_extract(metadata,'$.rel_path') != '' OR EXISTS (SELECT 1 FROM json_each(metadata,'$.rel_paths'))) THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='codex-cli' AND json_extract(metadata,'$.tool_name') IN ('apply_patch','Write','Read')%s THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='codex-cli' AND json_extract(metadata,'$.tool_name') IN ('apply_patch','Write','Read')%s AND (json_extract(metadata,'$.rel_path') != '' OR EXISTS (SELECT 1 FROM json_each(metadata,'$.rel_paths'))) THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='claude-code' AND json_extract(metadata,'$.type')='bash' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='claude-code' AND json_extract(metadata,'$.type')='bash' AND json_extract(metadata,'$.rel_path') != '' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='codex-cli' AND json_extract(metadata,'$.tool_name')='Bash' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN source='codex-cli' AND json_extract(metadata,'$.tool_name')='Bash' AND json_extract(metadata,'$.rel_path') != '' THEN 1 ELSE 0 END),0)
			FROM discussion_log`, ftCond, ftCond, ftCond, ftCond) + h2Cond
		db.QueryRow(h2SQL, h2QueryArgs...).
			Scan(&ftClaudeTotal, &ftClaudeHit, &ftCodexTotal, &ftCodexHit,
				&bsClaudeTotal, &bsClaudeHit, &bsCodexTotal, &bsCodexHit)
		var cTotal, cOrphan, cHashOk, cHashDupRows, cMultiTaskGroups int
		// hash_uniqueness 语义：只把「采集 bug 重复」标红——同 task 重复行 / 含空 task 的重复组行。
		// 多 task 同 hash（同一物理 commit 被多个 task 引用，relates_to 多对多）是合法语义，单独计数不告警。
		const dupRowsSQL = "SELECT COALESCE(SUM(1),0) FROM commits c WHERE c.commit_hash IS NOT NULL AND c.commit_hash != '' AND c.commit_hash IN (SELECT commit_hash FROM commits WHERE commit_hash IS NOT NULL AND commit_hash != '' %s GROUP BY commit_hash HAVING COUNT(*) > 1 AND (COUNT(DISTINCT task_id) = 1 OR SUM(CASE WHEN task_id IS NULL OR task_id='' THEN 1 ELSE 0 END) > 0))"
		const multiTaskSQL = "SELECT COUNT(*) FROM (SELECT commit_hash FROM commits WHERE commit_hash IS NOT NULL AND commit_hash != '' %s GROUP BY commit_hash HAVING COUNT(*) > 1 AND COUNT(DISTINCT task_id) > 1 AND SUM(CASE WHEN task_id IS NULL OR task_id='' THEN 1 ELSE 0 END) = 0)"
		if since != "" && since != "all" {
			db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN task_id IS NULL OR task_id='' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN commit_hash IS NOT NULL AND commit_hash != '' THEN 1 ELSE 0 END),0) FROM commits WHERE created_at >= ?", since).Scan(&cTotal, &cOrphan, &cHashOk)
			db.QueryRow(fmt.Sprintf(dupRowsSQL, "AND created_at >= ?"), since).Scan(&cHashDupRows)
			db.QueryRow(fmt.Sprintf(multiTaskSQL, "AND created_at >= ?"), since).Scan(&cMultiTaskGroups)
		} else {
			db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN task_id IS NULL OR task_id='' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN commit_hash IS NOT NULL AND commit_hash != '' THEN 1 ELSE 0 END),0) FROM commits").Scan(&cTotal, &cOrphan, &cHashOk)
			db.QueryRow(fmt.Sprintf(dupRowsSQL, "")).Scan(&cHashDupRows)
			db.QueryRow(fmt.Sprintf(multiTaskSQL, "")).Scan(&cMultiTaskGroups)
		}
		// D3 workflow_score：启发式规则分（100 起扣，非 AI 质量评估）— 覆盖率标注分母。
		var wfTotal, wfScored, wfSum int
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN quality_score > 0 THEN 1 ELSE 0 END),0) FROM session_summaries").Scan(&wfTotal, &wfScored)
		db.QueryRow("SELECT COALESCE(SUM(quality_score),0) FROM session_summaries WHERE quality_score > 0").Scan(&wfSum)
		// D4 task_completion_rate：done / 活跃任务（不含 deleted）。
		var taskDone, taskTotal int
		db.QueryRow("SELECT COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0), COUNT(*) FROM tasks WHERE status IN ('done','todo','in_progress','blocked','paused')").Scan(&taskDone, &taskTotal)
		// workflow_score 按 agent（启发式规则分，非 AI 质量评估）
		wfByAgent := map[string][2]int{}
		if wfRows, err := db.Query("SELECT COALESCE(source,''), quality_score FROM session_summaries WHERE quality_score > 0"); err == nil {
			for wfRows.Next() {
				var src string
				var sc int
				if err := wfRows.Scan(&src, &sc); err == nil {
					v := wfByAgent[src]
					v[0] += sc
					v[1]++
					wfByAgent[src] = v
				}
			}
			wfRows.Close()
		}
		db.Close()

		fmt.Println("── [DB 当前项目] ──")
		l2 := 0.0
		if total > 0 {
			l2 = float64(withL2) / float64(total)
		}
		dup := 0.0
		if evTotal > 0 {
			dup = 1.0 - float64(evUnique)/float64(evTotal)
		}
		// B1 改名 summary_coverage（8/27 口径统一，8/26 讨论总结动作 2）：
		// 原名 l2_coverage 与「L2 确认器」（P1b LLM 判定器）共用「L2」造成
		// 命名歧义（40.1% vs 52.8% 打架一部分源于此）。本指标实为 session_summaries
		// 摘要覆盖率（理解层产物覆盖），非 EVAL L2 语义覆盖——改名消歧。
		printRow("B1  summary_coverage", pct(l2)+fmt.Sprintf(" (%d/%d sessions)", withL2, total), "≥85%", l2 >= 0.85)
		printRow("B2  l2_nested_goal", fmt.Sprint(nested), "=0", nested == 0)
		printRow("B2  l2_md_block", fmt.Sprint(mdBlock), "=0", mdBlock == 0)
		printRow("B6  event_dup_rate", pct(dup), "<10%", dup < 0.10)
		printRow("D2  event_processed_rate(可行动)", pct(actionRate)+fmt.Sprintf(" (%d/%d)", evActionProc, evAction), "≥40%", actionRate >= 0.40)
		fmt.Printf("F1  事件→动作漏斗: 免处理=%d 可行动=%d 已处理=%d (%.1f%%)\n", evFree, evAction, evActionProc, actionRate*100)
		if len(actionDist) > 0 {
			fmt.Printf("     可行动处理分布: %s\n", strings.Join(actionDist, " · "))
		}
		wfAvg := 0.0
		if wfScored > 0 {
			wfAvg = float64(wfSum) / float64(wfScored)
		}
		taskRate := 0.0
		if taskTotal > 0 {
			taskRate = float64(taskDone) / float64(taskTotal)
		}
		printRow("E6  workflow_score", fmt.Sprintf("%.1f (覆盖 %d/%d)", wfAvg, wfScored, wfTotal), "≥60", wfAvg >= 60)
		// 按 agent 拆分：反映工作流规范性差异（MCP 绕过/hook 缺失/SQL 直查扣分），非 AI 质量。
		if len(wfByAgent) > 0 {
			agents := make([]string, 0, len(wfByAgent))
			for a := range wfByAgent {
				agents = append(agents, a)
			}
			sort.Slice(agents, func(i, j int) bool {
				return float64(wfByAgent[agents[i]][0])/float64(wfByAgent[agents[i]][1]) > float64(wfByAgent[agents[j]][0])/float64(wfByAgent[agents[j]][1])
			})
			fmt.Print("E6  workflow_score 按agent:")
			for _, a := range agents {
				v := wfByAgent[a]
				if a == "" {
					a = "unknown"
				}
				fmt.Printf(" %s=%.1f(n=%d)", a, float64(v[0])/float64(v[1]), v[1])
			}
			fmt.Println()
		}
		printRow("E7  task_completion_rate", pct(taskRate)+" ("+fmt.Sprint(taskDone)+"/"+fmt.Sprint(taskTotal)+")", ">80%", taskRate > 0.80)
		// commit 三件套：任一标红 = 采集管道异常（任务关联 / 来源可追踪 / 去重正确性）。
		orphanRate, hashTrace, hashDup := 0.0, 0.0, 0.0
		if cTotal > 0 {
			orphanRate = float64(cOrphan) / float64(cTotal)
			hashTrace = float64(cHashOk) / float64(cTotal)
		}
		if cHashOk > 0 {
			hashDup = float64(cHashDupRows) / float64(cHashOk)
		}
		fmt.Println("P0  commit 三件套（采集管道完整性）")
		printRow("     orphan_rate", pct(orphanRate)+" ("+fmt.Sprint(cOrphan)+"/"+fmt.Sprint(cTotal)+")", "<10%", orphanRate < 0.10)
		printRow("     hash_traceability", pct(hashTrace)+" ("+fmt.Sprint(cHashOk)+"/"+fmt.Sprint(cTotal)+")", ">90%", hashTrace > 0.90)
		printRow("     hash_uniqueness", pct(hashDup)+" ("+fmt.Sprint(cHashDupRows)+"行/"+fmt.Sprint(cHashOk)+")", "=0", hashDup == 0)
		fmt.Printf("      hash 多task引用: %d 组（同 commit 多 task 关联，合法 relates_to，不告警）\n", cMultiTaskGroups)
		// H2 metadata 健康（8/10 T2）：valid_rate 分母排除空串（对话消息本应空）；
		// invalid = 非空但非合法 JSON，必须为 0。
		dlNonEmpty := dlTotal - dlEmpty
		dlValidRate := 0.0
		if dlNonEmpty > 0 {
			dlValidRate = float64(dlValid) / float64(dlNonEmpty)
		}
		printRow("H2  metadata_health", fmt.Sprintf("valid=%s (%d/%d) empty=%d invalid=%d", pct(dlValidRate), dlValid, dlNonEmpty, dlEmpty, dlInvalid), "valid≥99.5% invalid=0", dlInvalid == 0 && dlValidRate >= 0.995)
		ftClaudeRate, ftCodexRate := 0.0, 0.0
		if ftClaudeTotal > 0 {
			ftClaudeRate = float64(ftClaudeHit) / float64(ftClaudeTotal)
		}
		if ftCodexTotal > 0 {
			ftCodexRate = float64(ftCodexHit) / float64(ftCodexTotal)
		}
		printRow("H2  rel_path_coverage(filetools)", fmt.Sprintf("claude=%s (%d/%d) codex=%s (%d/%d)", pct(ftClaudeRate), ftClaudeHit, ftClaudeTotal, pct(ftCodexRate), ftCodexHit, ftCodexTotal), "claude≥90% codex按决策19", ftClaudeTotal == 0 || ftClaudeRate >= 0.90)
		bsClaudeRate, bsCodexRate := 0.0, 0.0
		if bsClaudeTotal > 0 {
			bsClaudeRate = float64(bsClaudeHit) / float64(bsClaudeTotal)
		}
		if bsCodexTotal > 0 {
			bsCodexRate = float64(bsCodexHit) / float64(bsCodexTotal)
		}
		printRow("H2  rel_path_coverage(bash)", fmt.Sprintf("claude=%s (%d/%d) codex=%s (%d/%d)", pct(bsClaudeRate), bsClaudeHit, bsClaudeTotal, pct(bsCodexRate), bsCodexHit, bsCodexTotal), "参考（高置信才写）", true)
		fmt.Println()
	} else {
		fmt.Printf("⚠ 当前项目无 pmai.db（%v）— 跳过 DB 类指标\n\n", err)
	}

	// ── Log class (global proxy log) ──
	logPath := filepath.Join(os.Getenv("HOME"), ".aipmc", "logs", "aipmc.log")
	f, err := os.Open(logPath)
	if err != nil {
		fmt.Printf("⚠ 无法读取日志 %s: %v\n", logPath, err)
		return
	}
	defer f.Close()

	type llm struct {
		calls, inTok, outTok, cacheHit, cacheCreate, injectedY, injectedN int
		latSum                                                            float64
		latMax                                                            float64
	}
	byAgent := map[string]*llm{}
	var agentHookErr, postCommitErr, faOK, faErr, supTotal, supChar, skipTotal int
	var injOK, injGuidelines, injSame, injNoSum int
	var latestItems, latestTotal int
	latestAt := ""
	haveLatest := false
	var mcpTotal, mcpErr int
	mcpByTool := map[string]int{}
	mcpByAgent := map[string]int{}
	mcpErrReason := map[string]int{}
	var pipeL3, pipeReconDone, pipeReconErr, reviewErr int
	var dgTotal, dgPass, dgReject int
	dgReason := map[string]int{}
	oldFmtLines := 0

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	llmLines := 0
	for sc.Scan() {
		line := sc.Text()
		if hasCutoff {
			ts, hasDate, ok := parseLogTimestamp(line)
			if !ok || !hasDate {
				if ok {
					// 旧格式 [HH:MM:SS] 无日期，无法定位到具体日期，窗口下保守跳过。
					oldFmtLines++
				}
				continue
			}
			if ts.Before(cutoff) {
				continue
			}
		}
		switch {
		case strings.Contains(line, "[LLM]"):
			llmLines++
			fields := parseKVFields(line)
			agName := fields["agent"]
			if agName == "" || fields["in_tok"] == "" || fields["out_tok"] == "" {
				continue
			}
			ag := byAgent[agName]
			if ag == nil {
				ag = &llm{}
				byAgent[agName] = ag
			}
			ag.calls++
			ag.inTok += atoi(fields["in_tok"])
			ag.outTok += atoi(fields["out_tok"])
			// 字段名兼容：anthropic 路径打 cache_hit=/cache_create=，
			// responses 路径打 n_hit=/n_create=——统一归一化，字段名变更不破坏指标。
			if v := fields["cache_hit"]; v != "" {
				ag.cacheHit += atoi(v)
			} else if v := fields["n_hit"]; v != "" {
				ag.cacheHit += atoi(v)
			}
			if v := fields["cache_create"]; v != "" {
				ag.cacheCreate += atoi(v)
			} else if v := fields["n_create"]; v != "" {
				ag.cacheCreate += atoi(v)
			}
			if fields["injected"] == "Y" {
				ag.injectedY++
			} else {
				ag.injectedN++
			}
			if v := fields["lat"]; v != "" {
				lat := atof(strings.TrimSuffix(v, "s"))
				ag.latSum += lat
				if lat > ag.latMax {
					ag.latMax = lat
				}
			}
		case strings.Contains(line, "[HOOK]"):
			if strings.Contains(line, "hook=post-commit") {
				if strings.Contains(line, "status=ERR") {
					postCommitErr++
				}
			} else if strings.Contains(line, "json_parse_err") || strings.Contains(line, "panic") || strings.Contains(line, "FAILED") {
				agentHookErr++
			}
		case strings.Contains(line, "file_assoc"):
			if strings.Contains(line, "body_parse=err") {
				faErr++
			} else if strings.Contains(line, "file_assoc files=") {
				faOK++
			}
		case strings.Contains(line, "[INJECT] skip"):
			skipTotal++
			if strings.Contains(line, "reason=same_content") {
				injSame++
			} else if strings.Contains(line, "reason=no_summary_data") {
				injNoSum++
			}
		case strings.Contains(line, "[INJECT] inject "):
			// 仅 guidelines_only 行带 "inject " 前缀（source=guidelines_only）。
			injGuidelines++
		case strings.Contains(line, "[INJECT] agent="):
			// 正常注入行：agent=... goals=...（无 "inject " 前缀）。
			injOK++
		case strings.Contains(line, "suppressed="):
			supTotal++
			if strings.Contains(line, "reason=char_limit") {
				supChar++
			}
		case strings.Contains(line, "[MCP-ERR]"):
			// 错误已由 [MCP] status=ERR 计数（避免双计），此处统计 reason 分类分布
			// （8/12 起 recordMCPError 输出 reason=business_reject|idempotent|system_fault）。
			mcpErrReason[parseField(line, "reason=")]++
		case strings.Contains(line, "[MCP]"):
			fields := parseKVFields(line)
			if tool := fields["tool"]; tool != "" {
				mcpTotal++
				mcpByTool[tool]++
				if fields["status"] == "ERR" {
					mcpErr++
				}
				if src := fields["src"]; src != "" && src != "-" {
					// 决策 43：历史旧行 src=codex-mcp-client（8/7 归一化前）并入 codex-cli，
					// 避免按 agent 统计分裂。
					if strings.EqualFold(src, "codex-mcp-client") {
						src = "codex-cli"
					}
					mcpByAgent[src]++
				}
			}
		case strings.Contains(line, "[PIPELINE]"):
			if strings.Contains(line, "L3 session=") {
				pipeL3++
			} else if strings.Contains(line, "reconcile done") {
				pipeReconDone++
			} else if strings.Contains(line, "reconcile error") {
				pipeReconErr++
			} else if strings.Contains(line, "review error") {
				reviewErr++
			}
		case strings.Contains(line, "[DONE-GATE]"):
			dgTotal++
			if strings.Contains(line, " pass ") {
				dgPass++
			} else if strings.Contains(line, " reject ") {
				dgReject++
				if r := parseField(line, "reason="); r != "" {
					dgReason[r]++
				}
			}
		case strings.Contains(line, "emerge_events total="):
			haveLatest = true
			latestTotal = parseFieldInt(line, "total=")
			latestItems = parseFieldInt(line, "items=")
			if len(line) >= 2 && line[0] == '[' {
				if end := strings.Index(line, "]"); end > 0 {
					latestAt = line[1:end]
				}
			}
		}
	}

	fmt.Println("── [日志全局] ──")
	if hasCutoff && oldFmtLines > 0 {
		fmt.Printf("  ⚠ 跳过旧格式（无日期）日志 %d 条 — --window 只对 8/12 起带日期行生效\n", oldFmtLines)
	}
	inj := 0
	injTot := 0
	for _, a := range byAgent {
		inj += a.injectedY
		injTot += a.injectedY + a.injectedN
	}
	injRate := 0.0
	if injTot > 0 {
		injRate = float64(inj) / float64(injTot)
	}
	faRate := 0.0
	if faOK+faErr > 0 {
		faRate = float64(faOK) / float64(faOK+faErr)
	}
	supRate := 0.0
	if supTotal+skipTotal > 0 {
		supRate = float64(supChar) / float64(supTotal+skipTotal)
	}
	// B8: agent hook 调用总量不可测（无成功埋点），仅能统计错误计数。
	printRow("B8  hook_error(agent)", fmt.Sprint(agentHookErr)+" 次", "计数", agentHookErr == 0)
	printRow("B8  hook_error(post-commit)", fmt.Sprint(postCommitErr)+" 次", "计数", postCommitErr == 0)
	// C1 双口径：inject_rate=实际注入请求占比（same_content 去重跳过是设计行为，
	// 非失败）；inject_coverage=有数据可注时的覆盖（注入 + 去重 / 排除 no_summary）。
	printRow("C1  inject_rate", pct(injRate), "参考", true)
	covRate, _ := injectCoverage(injOK, injGuidelines, injSame, injNoSum)
	printRow("C1  inject_coverage", pct(covRate)+fmt.Sprintf(" (注入%d+去重%d)", injOK+injGuidelines, injSame), "≥80%", covRate >= 0.80)
	printRow("C2  file_parse_ok_rate", pct(faRate), "≥90%", faRate >= 0.90)
	printRow("C3  suppressed(char_limit)", fmt.Sprintf("%d/%d", supChar, supTotal+skipTotal)+" 次", "<30%", supRate < 0.30)
	if haveLatest {
		if latestAt != "" {
			printRow("C3  action_items(最新emerge)", fmt.Sprintf("%d/%d @%s", latestItems, latestTotal, latestAt), "≤10", latestItems <= 10)
		} else {
			printRow("C3  action_items(最新emerge)", fmt.Sprintf("%d/%d", latestItems, latestTotal), "≤10", latestItems <= 10)
		}
	} else {
		printRow("C3  action_items(最新emerge)", "无日志", "≤10", false)
	}
	// MCP 指标：结构化 [MCP] 日志（tool=/status=），总量不依赖 src=（serve 重启前旧行无 src）。
	mcpRate := 0.0
	if mcpTotal > 0 {
		mcpRate = 1.0 - float64(mcpErr)/float64(mcpTotal)
	}
	printRow("E5  mcp_success_rate", pct(mcpRate), "≥95%", mcpRate >= 0.95)
	printRow("E5  mcp_calls", fmt.Sprint(mcpTotal)+" 次", "参考", true)
	if len(mcpErrReason) > 0 {
		rs := make([]string, 0, len(mcpErrReason))
		for r, n := range mcpErrReason {
			if r == "" {
				r = "unknown"
			}
			rs = append(rs, fmt.Sprintf("%s=%d", r, n))
		}
		sort.Strings(rs)
		printRow("E5  mcp_err_reason", strings.Join(rs, " "), "参考", true)
	}
	readN, writeN := 0, 0
	for tool, n := range mcpByTool {
		if isWriteTool(tool) {
			writeN += n
		} else {
			readN += n
		}
	}
	printRow("E5  mcp_read/write", fmt.Sprintf("%d/%d", readN, writeN), "参考", true)
	if len(mcpByTool) > 0 {
		type toolCount struct {
			tool string
			n    int
		}
		tools := make([]toolCount, 0, len(mcpByTool))
		for tool, n := range mcpByTool {
			tools = append(tools, toolCount{tool, n})
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].n > tools[j].n })
		fmt.Printf("E5  mcp 工具分布 Top%d:", min(len(tools), 8))
		for i, tc := range tools {
			if i >= 8 {
				break
			}
			fmt.Printf(" %s=%d", tc.tool, tc.n)
		}
		fmt.Println()
	}
	if len(mcpByAgent) > 0 {
		agents := make([]string, 0, len(mcpByAgent))
		for a := range mcpByAgent {
			agents = append(agents, a)
		}
		sort.Slice(agents, func(i, j int) bool { return mcpByAgent[agents[i]] > mcpByAgent[agents[j]] })
		fmt.Print("E5  mcp 按agent:")
		for _, a := range agents {
			fmt.Printf(" %s=%d", a, mcpByAgent[a])
		}
		fmt.Println()
	}
	// 协作感知（L1）：agent_status 显式声明采纳率 — explicit=1 的行占比。
	// 今天 0 次显式声明=行为尚未养成，指标用于盯采纳进度（数据源是 store 不是日志）。
	if exp, total, err := store.CountExplicitStatuses(""); err == nil {
		rate := "—"
		if total > 0 {
			rate = fmt.Sprintf("%d/%d (%.0f%%)", exp, total, float64(exp)/float64(total)*100)
		}
		printRow("E5  update_status 显式率", rate, "参考", true)
	} else {
		printRow("E5  update_status 显式率", "无数据", "参考", true)
	}
	// E8 pipeline 健康度：L3 session 处理量 = 运行频率参考；reconcile 成功率为健康主指标。
	reconTotal := pipeReconDone + pipeReconErr
	reconRate := 0.0
	if reconTotal > 0 {
		reconRate = float64(pipeReconDone) / float64(reconTotal)
	}
	printRow("E8  pipeline_health", fmt.Sprintf("L3=%d recon=%d err=%d (%.1f%%)", pipeL3, reconTotal, pipeReconErr, reconRate*100), "成功率≥98%", reconRate >= 0.98)
	printRow("E8  review_error", fmt.Sprint(reviewErr)+" 次", "计数", reviewErr == 0)
	// E9 done-gate：pass/reject 分布（reject 埋点 8/7 补，历史日志仅 pass）。
	dgReasons := ""
	if len(dgReason) > 0 {
		rs := make([]string, 0, len(dgReason))
		for r, n := range dgReason {
			rs = append(rs, fmt.Sprintf("%s=%d", r, n))
		}
		sort.Strings(rs)
		dgReasons = " [" + strings.Join(rs, " ") + "]"
	}
	printRow("E9  done_gate", fmt.Sprintf("pass=%d reject=%d%s", dgPass, dgReject, dgReasons), "参考", dgReject == 0)
	// E3 成本效率：cache_hit/in_tok — 上游自动 prefix cache 利用率（8/7 实测
	// 43 亿 / 46.6 亿 ≈ 92%）。cache_create 上游不返回恒 0，hit/(hit+create)
	// 恒 100% 无信号（观测缺口见 EVALUATION E3）。
	var tCacheHit, tInTokAll int
	for _, a := range byAgent {
		tCacheHit += a.cacheHit
		tInTokAll += a.inTok
	}
	cacheHitRate := 0.0
	if tInTokAll > 0 {
		cacheHitRate = float64(tCacheHit) / float64(tInTokAll)
	}
	printRow("E3  cache_hit_rate", pct(cacheHitRate)+" ("+comma(tCacheHit)+"/"+comma(tInTokAll)+" tok)", "≥90%", cacheHitRate >= 0.90)
	fmt.Println()

	// Self-check: if there were [LLM] lines but nothing parsed, the log format
	// changed (e.g. new field inserted) and metrics would silently read zero.
	if llmLines > 100 && len(byAgent) == 0 {
		fmt.Printf("⚠ 自检: 日志有 %d 条 [LLM] 行但解析为 0 — 日志格式可能已变化，请检查 key=value 解析\n", llmLines)
	}

	fmt.Println("── [消耗参考] ──")
	fmt.Printf("%-8s %7s %12s %12s %10s %10s\n", "agent", "calls", "in_tok", "out_tok", "avg_lat", "cache_rate")
	var tCalls, tIn, tOut int
	var tLat float64
	for _, ag := range []string{"claude", "codex", "cursor", "opencode"} {
		a := byAgent[ag]
		if a == nil || a.calls == 0 {
			continue
		}
		cr := 0.0
		if a.inTok > 0 {
			cr = float64(a.cacheHit) / float64(a.inTok)
		}
		avg := a.latSum / float64(a.calls)
		fmt.Printf("%-8s %7d %12s %12s %8.1fs %10s\n", ag, a.calls, comma(a.inTok), comma(a.outTok), avg, pct(cr))
		tCalls += a.calls
		tIn += a.inTok
		tOut += a.outTok
		tLat += a.latSum
	}
	if tCalls > 0 {
		fmt.Printf("%-8s %7d %12s %12s %8.1fs\n", "totals", tCalls, comma(tIn), comma(tOut), tLat/float64(tCalls))
	}
}

// isWriteTool classifies aipm MCP tools into write (state-changing) vs read.
func isWriteTool(tool string) bool {
	for _, p := range []string{"record_", "create_", "update_", "link_", "add_to_thread", "mark_", "submit_feedback", "append_"} {
		if strings.Contains(tool, p) {
			return true
		}
	}
	return false
}

// parseKVFields extracts key=value tokens from a log line (after the [TAG]
// marker) into a map. Order-independent, so inserting a new field (e.g.
// project=) does not break parsing.
func parseKVFields(line string) map[string]string {
	out := map[string]string{}
	idx := strings.Index(line, "]")
	if idx < 0 {
		return out
	}
	for _, tok := range strings.Fields(line[idx+1:]) {
		eq := strings.Index(tok, "=")
		if eq <= 0 {
			continue
		}
		// 首见优先：行内参数回显可能重复同一 key（如 `[MCP] ... src=codex-mcp-client | src=-`），
		// 来源字段先出现，参数回显在后——后者不应覆盖前者。
		if _, seen := out[tok[:eq]]; !seen {
			out[tok[:eq]] = tok[eq+1:]
		}
	}
	return out
}

func printRow(name, val, target string, ok bool) {
	mark := "❌"
	if ok {
		mark = "✅"
	}
	fmt.Printf("%-28s %-12s 目标 %-8s %s\n", name, val, target, mark)
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

// injectCoverage 计算 C1 inject_coverage（HARNESS M1，8/18 修正）：
// 分子 = 注入 + 去重（same_content）；分母 = 注入 + 去重 + guidelines_only，
// **排除 no_summary_data**（无数据可注的请求不稀释覆盖率，与 C1 注释一致）。
// 返回 (rate, denom)，denom=0 时 rate=0（口径可复现，供 S4 fixture 核验）。
func injectCoverage(injOK, injGuidelines, injSame, injNoSum int) (float64, int) {
	denom := injOK + injGuidelines + injSame
	if denom <= 0 {
		return 0, 0
	}
	return float64(injOK+injGuidelines+injSame) / float64(denom), denom
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func comma(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 6 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func parseFieldInt(line, field string) int {
	idx := strings.Index(line, field)
	if idx < 0 {
		return 0
	}
	rest := line[idx+len(field):]
	end := strings.IndexAny(rest, " \t")
	if end < 0 {
		end = len(rest)
	}
	return atoi(rest[:end])
}

func parseField(line, field string) string {
	idx := strings.Index(line, field)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(field):]
	end := strings.IndexAny(rest, " \t")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// parseLogTimestamp extracts the leading "[...]" timestamp from a log line.
// 8/12 起 LogShared 输出 [2006-01-02 15:04:05]；历史行只有 [15:04:05]（无日期）。
// 返回解析后的时间、是否含日期、是否解析成功。无日期旧行 ok=true 但 hasDate=false，
// 供 --window 保守跳过（旧行无法定位日期）。
func parseLogTimestamp(line string) (t time.Time, hasDate, ok bool) {
	if len(line) < 2 || line[0] != '[' {
		return time.Time{}, false, false
	}
	end := strings.Index(line, "]")
	if end < 0 {
		return time.Time{}, false, false
	}
	s := line[1:end]
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t, true, true
	}
	if t, err := time.ParseInLocation("15:04:05", s, time.Local); err == nil {
		return t, false, true
	}
	return time.Time{}, false, false
}
