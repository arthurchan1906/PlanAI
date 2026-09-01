package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aipmc/cli"
	pmdb "aipmc/db"
)

// baseline.go — M0 捕获层完整性对账（MEASUREMENT_LOOP §2）：
// `aipmc metrics --baseline [--window 24h|--since <ISO>] [--skip_write]`
// 纯 SQL/日志对账，无 LLM 调用。输出 eval/baseline.json + 控制台摘要。

// sourceToAgent 将 discussion_log.source 映射到 proxy [LLM] 行的 agent 名。
var sourceToAgent = map[string]string{
	"codex-cli":   "codex",
	"claude-code": "claude",
	"opencode":    "opencode",
	"cursor":      "cursor",
	"gemini-cli":  "gemini",
}

type baselineWindow struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

type coverageStat struct {
	TotalLines   int `json:"total_lines"`
	WithSession  int `json:"with_session"`
	EmptySession int `json:"empty_session"`
	ProbeLines   int `json:"probe_lines"`
}

type underreportStat struct {
	Status            string   `json:"status"` // ok / unmeasurable
	Reason            string   `json:"reason,omitempty"`
	SessionsWithLLM   int      `json:"sessions_with_llm"`
	SessionsMissing   int      `json:"sessions_missing_in_discussion"`
	MissingRate       float64  `json:"missing_rate"`
	MissingSessionIDs []string `json:"missing_session_ids,omitempty"`
}

type orphanStat struct {
	Status                 string   `json:"status"` // ok / unmeasurable
	Reason                 string   `json:"reason,omitempty"`
	SessionsWithDiscussion int      `json:"sessions_with_discussion"`
	SessionsMissingInLLM   int      `json:"sessions_missing_in_llm_log"`
	MissingSessionIDs      []string `json:"missing_session_ids,omitempty"`
}

type coarseAlignStat struct {
	Status        string `json:"status"` // ok / n_a
	Reason        string `json:"reason,omitempty"`
	HoursWithLLM  int    `json:"hours_with_llm"`
	HoursWithDisc int    `json:"hours_with_disc"`
	HoursBoth     int    `json:"hours_both"`
	HoursLLMOnly  int    `json:"hours_llm_only"`
	HoursDiscOnly int    `json:"hours_disc_only"` // 脱链信号：有 discussion 但无 LLM 转发
}

type selfReportStat struct {
	CommitsInWindow int            `json:"commits_in_window"`
	TestStatus      map[string]int `json:"test_status"`
	SystemVerify    bool           `json:"system_verify_available"`
	VerifyNote      string         `json:"verify_note,omitempty"`
}

type baselineReport struct {
	GeneratedAt     string                     `json:"generated_at"`
	Window          baselineWindow             `json:"window"`
	ProjectsScanned []string                   `json:"projects_scanned,omitempty"`
	Coverage        map[string]coverageStat    `json:"llm_log_coverage"`
	Underreport     map[string]underreportStat `json:"underreport"`
	Orphans         map[string]orphanStat      `json:"orphan_sessions"`
	SelfReport      selfReportStat             `json:"self_report"`
	CoarseAlignment map[string]coarseAlignStat `json:"coarse_alignment"`
}

// isProbeLine 判定连通性探针：in_tok<=1 且 out_tok<=2（非真实 LLM 调用，
// 8/18 实测空 session 的 [LLM] 行全部为此形态）。
func isProbeLine(fields map[string]string) bool {
	return atoi(fields["in_tok"]) <= 1 && atoi(fields["out_tok"]) <= 2
}

// scanLLMLines 扫描日志中窗口内的 [LLM] 行，返回按 agent 的覆盖统计
// 与 (agent → session → 非探针请求数)。
// scanLLMLines 扫描日志中窗口内的 [LLM] 行，返回按 agent 的覆盖统计、
// (agent → session → 非探针请求数) 与 (agent → 活跃小时集合)。
// 活跃小时用于 claude 等无 session 可 join 的 agent 的粗粒度对账。
func scanLLMLines(path string, since time.Time) (map[string]*coverageStat, map[string]map[string]int, map[string]map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()

	coverage := map[string]*coverageStat{}
	sessions := map[string]map[string]int{}
	hourActivity := map[string]map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "[LLM]") {
			continue
		}
		ts, hasDate, ok := parseLogTimestamp(line)
		if !ok || !hasDate {
			continue // 旧格式无日期，窗口下保守跳过
		}
		if ts.Before(since) {
			continue
		}
		fields := parseKVFields(line)
		ag := fields["agent"]
		if ag == "" || fields["in_tok"] == "" || fields["out_tok"] == "" {
			continue
		}
		st := coverage[ag]
		if st == nil {
			st = &coverageStat{}
			coverage[ag] = st
		}
		hourKey := ts.Format("2006-01-02T15")
		hm := hourActivity[ag]
		if hm == nil {
			hm = map[string]bool{}
			hourActivity[ag] = hm
		}
		hm[hourKey] = true
		st.TotalLines++
		sid := fields["session"]
		if sid == "" {
			st.EmptySession++
			if isProbeLine(fields) {
				st.ProbeLines++
			}
			continue
		}
		st.WithSession++
		if isProbeLine(fields) {
			st.ProbeLines++
			continue
		}
		m := sessions[ag]
		if m == nil {
			m = map[string]int{}
			sessions[ag] = m
		}
		m[sid]++
	}
	return coverage, sessions, hourActivity, sc.Err()
}

func parseBaselineSince(arg string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if ts, err := time.ParseInLocation(layout, arg, time.Local); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析 --since %q（示例: 2026-08-17 或 2026-08-17T00:00:00）", arg)
}

// buildUnderreport 计算漏录率：有 proxy 请求但无 discussion_log 消息的 session。
func buildUnderreport(llmSessions map[string]map[string]int, discByAgent map[string]map[string]int, coverage map[string]*coverageStat) map[string]underreportStat {
	out := map[string]underreportStat{}
	for ag, sess := range llmSessions {
		if len(sess) == 0 {
			continue
		}
		disc := discByAgent[ag]
		st := underreportStat{Status: "ok"}
		st.SessionsWithLLM = len(sess)
		for sid := range sess {
			if disc[sid] == 0 {
				st.SessionsMissing++
				st.MissingSessionIDs = append(st.MissingSessionIDs, sid)
			}
		}
		if st.SessionsWithLLM > 0 {
			st.MissingRate = float64(st.SessionsMissing) / float64(st.SessionsWithLLM)
		}
		sort.Strings(st.MissingSessionIDs)
		st.MissingSessionIDs = capDetail(st.MissingSessionIDs)
		out[ag] = st
	}
	// 有日志行但 session 全空 → 不可按 session 对账（标注缺口，不静默跳过）。
	for ag, st := range coverage {
		if _, ok := out[ag]; ok || st.TotalLines == 0 || st.WithSession > 0 {
			continue
		}
		out[ag] = underreportStat{
			Status: "unmeasurable",
			Reason: "该 agent 的 [LLM] 行 session= 为空（请求体无 session_id 字段），无法按 session join 对账",
		}
	}
	return out
}

// buildOrphans 反向对账：discussion_log 有、[LLM] 无的 session（脱链）。
func buildOrphans(discByAgent map[string]map[string]int, llmSessions map[string]map[string]int) map[string]orphanStat {
	out := map[string]orphanStat{}
	for ag, disc := range discByAgent {
		if len(disc) == 0 {
			continue
		}
		st := orphanStat{Status: "ok"}
		st.SessionsWithDiscussion = len(disc)
		llm := llmSessions[ag]
		if llm == nil {
			// 该 agent 日志侧无任何可 join 的 session（如 claude session 全空）。
			// 不能判定为脱链——是日志侧归因缺失。
			out[ag] = orphanStat{
				Status:                 "unmeasurable",
				Reason:                 "日志侧无可 join 的 [LLM] session（该 agent 请求体无 session_id），脱链与否无法判定",
				SessionsWithDiscussion: len(disc),
			}
			continue
		}
		for sid := range disc {
			if llm[sid] == 0 {
				st.SessionsMissingInLLM++
				st.MissingSessionIDs = append(st.MissingSessionIDs, sid)
			}
		}
		sort.Strings(st.MissingSessionIDs)
		st.MissingSessionIDs = capDetail(st.MissingSessionIDs)
		out[ag] = st
	}
	return out
}

// buildCoarseAlignment 按「agent × 小时」聚合对比 LLM 请求与 discussion 活动。
// 用于无 session 可 join 的 agent（claude）：小时级有无对齐是弱信号，
// HoursDiscOnly>0 表示存在「有 discussion 活动但无任何 LLM 转发」的脱链小时。
func buildCoarseAlignment(llmHours, discHours map[string]map[string]bool) map[string]coarseAlignStat {
	out := map[string]coarseAlignStat{}
	agents := map[string]bool{}
	for ag := range llmHours {
		agents[ag] = true
	}
	for ag := range discHours {
		agents[ag] = true
	}
	for ag := range agents {
		lh, dh := llmHours[ag], discHours[ag]
		st := coarseAlignStat{Status: "ok"}
		hours := map[string]bool{}
		for h := range lh {
			hours[h] = true
		}
		for h := range dh {
			hours[h] = true
		}
		for h := range hours {
			l, d := lh[h], dh[h]
			switch {
			case l && d:
				st.HoursBoth++
			case l && !d:
				st.HoursLLMOnly++
			case !l && d:
				st.HoursDiscOnly++
			}
		}
		st.HoursWithLLM = len(lh)
		st.HoursWithDisc = len(dh)
		if st.HoursWithLLM == 0 && st.HoursWithDisc == 0 {
			continue
		}
		if st.HoursWithLLM == 0 && st.HoursWithDisc > 0 {
			st.Status = "unmeasurable"
			st.Reason = "日志侧无该 agent 的 [LLM] 行（可能未接入 proxy），无法做小时级对齐"
		}
		out[ag] = st
	}
	return out
}

func capDetail(ids []string) []string {
	const maxDetail = 20
	if len(ids) <= maxDetail {
		return ids
	}
	return ids[:maxDetail]
}

// scanDiscussionDBs 扫描给定的一组项目库路径的 discussion_log，聚合 (agent, session) 计数
// 与 (agent, 小时)。单库打开失败则跳过不阻塞。返回对账成功的库路径（供报告标注范围）。
func scanDiscussionDBs(dbPaths []string, sinceISO string) (map[string]map[string]int, map[string]map[string]bool, []string) {
	discByAgent := map[string]map[string]int{}
	discHours := map[string]map[string]bool{}
	var scanned []string
	for _, dbPath := range dbPaths {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			continue
		}
		d, err := sql.Open("sqlite", dbPath+"?mode=ro")
		if err != nil {
			continue
		}
		rows, err := d.Query(`SELECT source, session_id, COUNT(*) FROM discussion_log
			WHERE created_at >= ? AND session_id != '' AND session_id != 'unknown'
			GROUP BY source, session_id`, sinceISO)
		if err != nil {
			d.Close()
			continue
		}
		for rows.Next() {
			var src, sid string
			var n int
			if err := rows.Scan(&src, &sid, &n); err != nil {
				continue
			}
			ag := sourceToAgent[src]
			if ag == "" {
				continue
			}
			m := discByAgent[ag]
			if m == nil {
				m = map[string]int{}
				discByAgent[ag] = m
			}
			m[sid] += n
		}
		rows.Close()

		hrows, err := d.Query(`SELECT source, substr(created_at,1,13) FROM discussion_log
			WHERE created_at >= ? AND session_id != '' AND session_id != 'unknown'`, sinceISO)
		if err == nil {
			for hrows.Next() {
				var src, hk string
				if err := hrows.Scan(&src, &hk); err != nil {
					continue
				}
				ag := sourceToAgent[src]
				if ag == "" {
					continue
				}
				m := discHours[ag]
				if m == nil {
					m = map[string]bool{}
					discHours[ag] = m
				}
				m[hk] = true
			}
			hrows.Close()
		}
		d.Close()
		scanned = append(scanned, dbPath)
	}
	sort.Strings(scanned)
	return discByAgent, discHours, scanned
}

// scanDiscussionAcrossProjects 聚合所有注册项目库的 discussion_log（bug-141137）：
// [LLM] 日志是全局的（跨项目），而 discussion_log 按 hook 进程 CWD 分项目写入。
// 只读当前项目库会把其他项目（如 EncryptDrive/HmApp）的 session 误判为「有 LLM 无
// discussion」。此函数扫描 ~/.aipmc/projects.json 注册的全部项目 + 当前项目，把
// (agent, session) 计数与 (agent, 小时) 合并，供漏录率/脱链/粗对齐使用。
// collectProjectPathsFromLog 从全局日志反推出现过绝对 project= 标签的项目根目录。
// 用于补充注册表未涵盖的项目：即便某项目未登记在 ~/.aipmc/projects.json，
// 只要其 serve/proxy 行带 project=/绝对路径，也能据此定位其 discussion 库。
// bare-name（project=aipmc）无法可靠定位，跳过；测试临时目录（/var/folders/T/...）
// 的库不存在会在扫描时被 os.Stat 跳过，无副作用。
func collectProjectPathsFromLog(logPath string) []string {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		for _, tok := range strings.Fields(sc.Text()) {
			if !strings.HasPrefix(tok, "project=/") {
				continue
			}
			p := strings.TrimPrefix(tok, "project=")
			// 归一化：project=/x/proj/.pmai → /x/proj
			if strings.HasSuffix(p, "/.pmai") {
				p = strings.TrimSuffix(p, "/.pmai")
			} else if strings.HasSuffix(p, ".pmai") {
				p = strings.TrimSuffix(p, ".pmai")
			}
			if p != "" && strings.HasPrefix(p, "/") && p != "/" {
				seen[p] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// scanDiscussionAcrossProjects 聚合所有注册项目库 + 全局日志反推项目库的 discussion_log
// （bug-141137 + 增强）：[LLM] 日志是全局的（跨项目），而 discussion_log 按 hook 进程 CWD
// 分项目写入。只读当前项目库会把其他项目（如 EncryptDrive/HmApp）的 session 误判为「有 LLM
// 无 discussion」。此函数扫描 ~/.aipmc/projects.json 注册的项目 + 当前项目 + 日志中出现的
// 绝对 project= 路径对应项目，把 (agent, session) 计数与 (agent, 小时) 合并，供对账使用。
func scanDiscussionAcrossProjects(logPath, sinceISO string) (map[string]map[string]int, map[string]map[string]bool, []string) {
	var dbPaths []string
	seen := map[string]bool{}

	// 当前项目（运行时 .pmai 的父目录）。
	if runtimeDir, err := pmdb.RuntimeDir(); err == nil && runtimeDir != "" {
		cur := filepath.Dir(runtimeDir)
		if cur != "." && cur != "/" {
			seen[filepath.Join(cur, ".pmai", "data", "pmai.db")] = true
		}
	}
	// 注册的全部项目。
	for _, p := range pmdb.LoadCleanProjects() {
		seen[filepath.Join(p.Path, ".pmai", "data", "pmai.db")] = true
	}
	// 增强：从全局日志反推未注册项目的 project 路径（见 collectProjectPathsFromLog）。
	for _, p := range collectProjectPathsFromLog(logPath) {
		seen[filepath.Join(p, ".pmai", "data", "pmai.db")] = true
	}
	for dbPath := range seen {
		dbPaths = append(dbPaths, dbPath)
	}
	return scanDiscussionDBs(dbPaths, sinceISO)
}

func runBaseline(args *cli.Args) {
	now := time.Now()
	skipWrite := args.Bool("skip_write")
	sinceArg := args.Str("since", "")
	var since time.Time
	if sinceArg != "" {
		ts, err := parseBaselineSince(sinceArg)
		if err != nil {
			fmt.Println("⚠", err)
			return
		}
		since = ts
	} else {
		win := args.Str("window", "24h")
		d, err := time.ParseDuration(win)
		if err != nil {
			fmt.Printf("⚠ 无效 --window %q — 使用默认 24h\n", win)
			d = 24 * time.Hour
		}
		since = now.Add(-d)
	}
	sinceISO := since.Format("2006-01-02T15:04:05")

	// 1. 日志侧：扫描 [LLM] 行。
	logPath := filepath.Join(os.Getenv("HOME"), ".aipmc", "logs", "aipmc.log")
	coverage, llmSessions, llmHours, err := scanLLMLines(logPath, since)
	if err != nil {
		fmt.Printf("⚠ 无法读取日志 %s: %v\n", logPath, err)
		return
	}

	// 2. DB 侧：discussion_log 按 (source, session) 聚合 + commits 自报分布。
	db, err := pmdb.Open()
	if err != nil {
		fmt.Printf("⚠ 当前项目无 pmai.db（%v）— 基线需要 DB 侧对账，退出\n", err)
		return
	}
	defer db.Close()

	// 跨项目聚合 discussion_log（bug-141137）：[LLM] 日志全局 vs discussion 分项目库。
	discByAgent, discHours, scannedProjects := scanDiscussionAcrossProjects(logPath, sinceISO)
	if len(scannedProjects) > 0 {
		fmt.Printf("  对账项目库 %d 个: %s\n", len(scannedProjects), strings.Join(scannedProjects, ", "))
	}

	// 3. 计算漏录率 + 脱链。
	underreport := buildUnderreport(llmSessions, discByAgent, coverage)
	orphans := buildOrphans(discByAgent, llmSessions)
	coarse := buildCoarseAlignment(llmHours, discHours)

	// 4. 自报可信度：commits 窗口内 test_status 分布；系统验证列存在性。
	self := selfReportStat{TestStatus: map[string]int{}}
	var cPass, cAuto, cFail, cNotRun, cOther int
	db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN test_status='passed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN test_status='auto' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN test_status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN test_status='not_run' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN test_status NOT IN ('passed','auto','failed','not_run') THEN 1 ELSE 0 END),0),
		COUNT(*)
		FROM commits WHERE substr(created_at,1,19) >= ?`, sinceISO).Scan(&cPass, &cAuto, &cFail, &cNotRun, &cOther, &self.CommitsInWindow)
	self.TestStatus["passed"] = cPass
	self.TestStatus["auto"] = cAuto
	self.TestStatus["failed"] = cFail
	self.TestStatus["not_run"] = cNotRun
	self.TestStatus["other"] = cOther
	if cols, err := db.Query("PRAGMA table_info(commits)"); err == nil {
		for cols.Next() {
			var cid int
			var name, typ string
			var notnull int
			var dflt any
			var pk int
			if err := cols.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err == nil && name == "verify_status" {
				self.SystemVerify = true
			}
		}
		cols.Close()
	}
	if !self.SystemVerify {
		self.VerifyNote = "commits 表无 verify_status 列——系统实测通过率（L-O）不可查，自报膨胀率 gap 待 schema 迁移后计算（DATA_AUDIT §3）"
	}

	// 5. 输出。
	report := baselineReport{
		GeneratedAt:     now.Format("2006-01-02T15:04:05-07:00"),
		Window:          baselineWindow{Since: sinceISO, Until: now.Format("2006-01-02T15:04:05")},
		ProjectsScanned: scannedProjects,
		Coverage:        map[string]coverageStat{},
		Underreport:     underreport,
		Orphans:         orphans,
		SelfReport:      self,
		CoarseAlignment: coarse,
	}
	for ag, st := range coverage {
		report.Coverage[ag] = *st
	}
	printBaselineSummary(report, sinceISO)

	if !skipWrite {
		if err := os.MkdirAll("eval", 0o755); err == nil {
			data, _ := json.MarshalIndent(report, "", "  ")
			if werr := os.WriteFile("eval/baseline.json", data, 0o644); werr != nil {
				fmt.Printf("⚠ 写 eval/baseline.json 失败: %v\n", werr)
			} else {
				fmt.Printf("\n✅ 已写入 eval/baseline.json（%d bytes）\n", len(data))
			}
		}
	}
}

func printBaselineSummary(r baselineReport, sinceISO string) {
	fmt.Println("M0 捕获层完整性对账（MEASUREMENT_LOOP §2）")
	fmt.Printf("窗口: %s → %s\n\n", r.Window.Since, r.Window.Until)

	fmt.Println("[LLM] 日志行 session 覆盖")
	agents := make([]string, 0, len(r.Coverage))
	for ag := range r.Coverage {
		agents = append(agents, ag)
	}
	sort.Strings(agents)
	if len(agents) == 0 {
		fmt.Println("  （窗口内无 [LLM] 行）")
	}
	for _, ag := range agents {
		st := r.Coverage[ag]
		fmt.Printf("  %-8s 总 %d | 带 session %d | 空 session %d | 探针 %d\n",
			ag, st.TotalLines, st.WithSession, st.EmptySession, st.ProbeLines)
	}
	fmt.Println()

	fmt.Println("漏录率（有 LLM 请求但 discussion_log 无该 session）")
	for _, ag := range agents {
		st, ok := r.Underreport[ag]
		if !ok {
			continue
		}
		if st.Status == "unmeasurable" {
			fmt.Printf("  %-8s 不可对账: %s\n", ag, st.Reason)
			continue
		}
		fmt.Printf("  %-8s %d/%d session 漏录（%.1f%%）", ag, st.SessionsMissing, st.SessionsWithLLM, st.MissingRate*100)
		if len(st.MissingSessionIDs) > 0 {
			fmt.Printf(" 例: %s", strings.Join(st.MissingSessionIDs[:min(3, len(st.MissingSessionIDs))], ", "))
		}
		fmt.Println()
	}
	fmt.Println()

	fmt.Println("反向对账（discussion_log 有、[LLM] 无 = 脱链 session）")
	for _, ag := range agents {
		st, ok := r.Orphans[ag]
		if !ok {
			continue
		}
		if st.Status == "unmeasurable" {
			fmt.Printf("  %-8s 不可判定: %s\n", ag, st.Reason)
			continue
		}
		fmt.Printf("  %-8s %d/%d session 无对应 LLM 请求", ag, st.SessionsMissingInLLM, st.SessionsWithDiscussion)
		if len(st.MissingSessionIDs) > 0 {
			fmt.Printf(" 例: %s", strings.Join(st.MissingSessionIDs[:min(3, len(st.MissingSessionIDs))], ", "))
		}
		fmt.Println()
	}
	fmt.Println()

	fmt.Println("粗粒度对齐（agent × 小时，用于无 session 可 join 的 agent）")
	caAgents := make([]string, 0, len(r.CoarseAlignment))
	for ag := range r.CoarseAlignment {
		caAgents = append(caAgents, ag)
	}
	sort.Strings(caAgents)
	for _, ag := range caAgents {
		st := r.CoarseAlignment[ag]
		if st.Status == "unmeasurable" {
			fmt.Printf("  %-8s 不可对齐: %s\n", ag, st.Reason)
			continue
		}
		fmt.Printf("  %-8s LLM 活跃 %d 小时 | discussion 活跃 %d 小时 | 都有 %d | 仅LLM %d | 仅disc %d\n",
			ag, st.HoursWithLLM, st.HoursWithDisc, st.HoursBoth, st.HoursLLMOnly, st.HoursDiscOnly)
		if st.HoursDiscOnly > 0 {
			fmt.Printf("          ⚠ 有 discussion 但无 LLM 转发的小时: %d（脱链信号）\n", st.HoursDiscOnly)
		}
	}
	fmt.Println()

	fmt.Println("自报可信度（commits test_status）")
	fmt.Printf("  窗口内 commits: %d | %v\n", r.SelfReport.CommitsInWindow, r.SelfReport.TestStatus)
	fmt.Printf("  系统验证: %v\n", r.SelfReport.SystemVerify)
	if r.SelfReport.VerifyNote != "" {
		fmt.Printf("  ⚠ %s\n", r.SelfReport.VerifyNote)
	}
}
