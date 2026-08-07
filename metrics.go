package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	fmt.Println("AIPM 评估指标 — 目标值来自 docs/EVALUATION.md")
	fmt.Println("DB 类指标: 当前项目 point-in-time；日志类指标: ~/.aipmc/logs/aipmc.log 全局（无 project 字段）")
	if window != "" {
		fmt.Printf("日志窗口: %s（DB 类指标不支持窗口回算，始终为当前值）\n", window)
	}
	fmt.Println()

	// ── DB class (current project) ──
	db, err := pmdb.Open()
	if err == nil {
		var total, withL2, nested int
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN summary!='' THEN 1 ELSE 0 END),0) FROM session_summaries").Scan(&total, &withL2)
		db.QueryRow("SELECT COUNT(*) FROM session_summaries WHERE summary LIKE '%\"goal\":\"{%' OR summary LIKE '%```json%'").Scan(&nested)
		var evTotal, evProcessed int
		db.QueryRow("SELECT COUNT(*), COALESCE(SUM(processed_by_agent),0) FROM events").Scan(&evTotal, &evProcessed)
		var evUnique int
		db.QueryRow("SELECT COUNT(DISTINCT type || '|' || entity_type || '|' || entity_id) FROM events").Scan(&evUnique)
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
		printRow("B6  event_dup_rate", pct(dup), "<10%", dup < 0.10)
		printRow("D2  event_processed_rate", pct(proc), "≥40%", proc >= 0.40)
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
	haveLatest := false

	llmRe := regexp.MustCompile(`agent=(\w+).*?in_tok=(\d+) out_tok=(\d+)(?: cache_hit=(\d+))?(?: cache_create=(\d+))? injected=([YN]) lat=([\d.]+)s`)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "[LLM]"):
			m := llmRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ag := byAgent[m[1]]
			if ag == nil {
				ag = &llm{}
				byAgent[m[1]] = ag
			}
			ag.calls++
			ag.inTok += atoi(m[2])
			ag.outTok += atoi(m[3])
			if m[4] != "" {
				ag.cacheHit += atoi(m[4])
			}
			if m[5] != "" {
				ag.cacheCreate += atoi(m[5])
			}
			if m[6] == "Y" {
				ag.injectedY++
			} else {
				ag.injectedN++
			}
			lat := atof(m[7])
			ag.latSum += lat
			if lat > ag.latMax {
				ag.latMax = lat
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
		case strings.Contains(line, "emerge_events total="):
			haveLatest = true
			latestTotal = parseFieldInt(line, "total=")
			latestItems = parseFieldInt(line, "items=")
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
		printRow("C3  action_items(最新emerge)", fmt.Sprintf("%d/%d", latestItems, latestTotal), "≤10", latestItems <= 10)
	} else {
		printRow("C3  action_items(最新emerge)", "无日志", "≤10", false)
	}
	fmt.Println()

	fmt.Println("── [消耗参考] ──")
	fmt.Printf("%-8s %7s %12s %12s %10s %10s\n", "agent", "calls", "in_tok", "out_tok", "avg_lat", "cache_rate")
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
	}
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
