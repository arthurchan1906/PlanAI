package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aipmc/cli"
	pmdb "aipmc/db"
)

// dispatchMetrics implements `aipmc metrics` — a read-only, point-in-time
// check of EVALUATION.md targets. No JSON snapshots, no time series, no diff:
// v1 is just "how do we stand right now against the documented targets".
// DB-class metrics reflect the current project (cwd .pmai); log-class metrics
// cover the global proxy log (~/.aipmc/logs/aipmc.log) which has no project
// field yet (documented limitation, v2 adds it).
func dispatchMetrics(args *cli.Args) {
	window := args.Str("window", "") // optional: "24h" for log-class metrics only
	// commit 三件套窗口：默认只看修复后数据（StoreGitCommit 97ce814 起），
	// 避免历史污染（ED 547 空 hash、aipmc 历史孤儿）淹没告警信号。
	// --since all 看全表（存量状态）；--since 2026-08-01 自定义窗口。
	since := args.Str("since", "2026-08-07T14:00:00")
	fmt.Println("AIPM 评估指标 — 目标值来自 docs/EVALUATION.md")
	fmt.Println("DB 类指标: 当前项目 point-in-time；日志类指标: ~/.aipmc/logs/aipmc.log 全局（无 project 字段）")
	fmt.Printf("commit 三件套窗口: since=%s（--since all 看全表）\n", since)
	if window != "" {
		fmt.Printf("日志窗口: %s（DB 类指标不支持窗口回算，始终为当前值）\n", window)
	}
	fmt.Println()

	// ── DB class (current project) ──
	db, err := pmdb.Open()
	if err == nil {
		var total, withL2, nested, mdBlock int
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN summary!='' THEN 1 ELSE 0 END),0) FROM session_summaries").Scan(&total, &withL2)
		// B2 双口径：nested_goal = goal 值本身是嵌套 JSON；md_block = 摘要含 ```json 代码块（不同缺陷，分开统计）。
		db.QueryRow("SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%\"goal\":\"{%'").Scan(&nested)
		db.QueryRow("SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%```json%'").Scan(&mdBlock)
		var evTotal, evProcessed int
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(processed_by_agent),0) FROM events").Scan(&evTotal, &evProcessed)
		var evUnique int
		db.QueryRow("SELECT COUNT(DISTINCT type || '|' || entity_type || '|' || entity_id) FROM events").Scan(&evUnique)
		var cTotal, cOrphan, cHashOk, cHashUnique int
		if since != "" && since != "all" {
			db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN task_id IS NULL OR task_id='' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN commit_hash IS NOT NULL AND commit_hash != '' THEN 1 ELSE 0 END),0) FROM commits WHERE created_at >= ?", since).Scan(&cTotal, &cOrphan, &cHashOk)
			db.QueryRow("SELECT COUNT(DISTINCT commit_hash) FROM commits WHERE created_at >= ? AND commit_hash IS NOT NULL AND commit_hash != ''", since).Scan(&cHashUnique)
		} else {
			db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN task_id IS NULL OR task_id='' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN commit_hash IS NOT NULL AND commit_hash != '' THEN 1 ELSE 0 END),0) FROM commits").Scan(&cTotal, &cOrphan, &cHashOk)
			db.QueryRow("SELECT COUNT(DISTINCT commit_hash) FROM commits WHERE commit_hash IS NOT NULL AND commit_hash != ''").Scan(&cHashUnique)
		}
		db.Close()

		fmt.Println("── [DB 当前项目] ──")
		l2 := 0.0
		if total > 0 {
			l2 = float64(withL2) / float64(total)
		}
		proc := 0.0
		if evTotal > 0 {
			proc = float64(evProcessed) / float64(evTotal)
		}
		dup := 0.0
		if evTotal > 0 {
			dup = 1.0 - float64(evUnique)/float64(evTotal)
		}
		printRow("B1  l2_coverage", pct(l2), "≥85%", l2 >= 0.85)
		printRow("B2  l2_nested_goal", fmt.Sprint(nested), "=0", nested == 0)
		printRow("B2  l2_md_block", fmt.Sprint(mdBlock), "=0", mdBlock == 0)
		printRow("B6  event_dup_rate", pct(dup), "<10%", dup < 0.10)
		printRow("D2  event_processed_rate", pct(proc), "≥40%", proc >= 0.40)
		// commit 三件套：任一标红 = 采集管道异常（任务关联 / 来源可追踪 / 去重正确性）。
		orphanRate, hashTrace, hashDup := 0.0, 0.0, 0.0
		if cTotal > 0 {
			orphanRate = float64(cOrphan) / float64(cTotal)
			hashTrace = float64(cHashOk) / float64(cTotal)
		}
		if cHashOk > 0 {
			hashDup = 1.0 - float64(cHashUnique)/float64(cHashOk)
		}
		fmt.Println("P0  commit 三件套（采集管道完整性）")
		printRow("     orphan_rate", pct(orphanRate)+" ("+fmt.Sprint(cOrphan)+"/"+fmt.Sprint(cTotal)+")", "<10%", orphanRate < 0.10)
		printRow("     hash_traceability", pct(hashTrace)+" ("+fmt.Sprint(cHashOk)+"/"+fmt.Sprint(cTotal)+")", ">90%", hashTrace > 0.90)
		printRow("     hash_uniqueness", pct(hashDup), "=0", hashDup == 0)
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

	type llm struct{ calls, inTok, outTok, cacheHit, cacheCreate, injectedY, injectedN int; latSum float64; latMax float64 }
	byAgent := map[string]*llm{}
	var agentHookErr, postCommitErr, faOK, faErr, supTotal, supChar, skipTotal int
	var latestItems, latestTotal int
	latestAt := ""
	haveLatest := false
	var mcpTotal, mcpErr int
	mcpByTool := map[string]int{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	llmLines := 0
	for sc.Scan() {
		line := sc.Text()
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
			if v := fields["cache_hit"]; v != "" {
				ag.cacheHit += atoi(v)
			}
			if v := fields["cache_create"]; v != "" {
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
		case strings.Contains(line, "suppressed="):
			supTotal++
			if strings.Contains(line, "reason=char_limit") {
				supChar++
			}
		case strings.Contains(line, "[MCP-ERR]"):
			// 错误已由 [MCP] status=ERR 计数（避免双计），此处仅保留分类锚点。
		case strings.Contains(line, "[MCP]"):
			fields := parseKVFields(line)
			if tool := fields["tool"]; tool != "" {
				mcpTotal++
				mcpByTool[tool]++
				if fields["status"] == "ERR" {
					mcpErr++
				}
			}
		case strings.Contains(line, "emerge_events total="):
			haveLatest = true
			latestTotal = parseFieldInt(line, "total=")
			latestItems = parseFieldInt(line, "items=")
			if len(line) >= 9 && line[0] == '[' {
				latestAt = line[1:9]
			}
		}
	}

	fmt.Println("── [日志全局] ──")
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
	printRow("C1  inject_rate", pct(injRate), "≥80%", injRate >= 0.80)
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
		if a.cacheHit+a.cacheCreate > 0 {
			cr = float64(a.cacheHit) / float64(a.cacheHit+a.cacheCreate)
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
		out[tok[:eq]] = tok[eq+1:]
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
