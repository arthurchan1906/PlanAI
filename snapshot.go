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

// snapshot 实现 `aipmc snapshot` —— 反馈镜子快照（METRICS_SPEC v1 设计稿）。
// 方案 A（8/23 共识）：落盘 + 只读——默认写 .pmai/data/snapshots/latest.json
// （web/snapshot 只读消费，前端 Metrics 视图增量）；--diff 对比两份快照。
// 口径约束（8/27 注册表原则）：DB 质量指标复用 metrics_shared.go 的共享查询，
// 与 `aipmc metrics` 同一套 SQL，禁止另起炉灶。

// snapshotDoc 是快照 v1 schema（METRICS_SPEC §3）。
type snapshotDoc struct {
	SchemaVersion int          `json:"schema_version"`
	GeneratedAt   string       `json:"generated_at"`
	Window        snapshotWin  `json:"window"`
	Metrics       snapshotMets `json:"metrics"`
	Notes         snapshotNote `json:"notes"`
}

type snapshotWin struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

type snapshotMets struct {
	Consumption map[string]*agentConsumption `json:"consumption"`
	Quality     snapshotQuality              `json:"quality"`
	Injection   snapshotInjection            `json:"injection"`
}

type snapshotNote struct {
	CacheHitCoverage float64 `json:"cache_hit_coverage"`
}

type agentConsumption struct {
	Calls        int     `json:"calls"`
	InTok        int64   `json:"in_tok"`
	OutTok       int64   `json:"out_tok"`
	AvgLat       float64 `json:"avg_lat"`  // 单位：秒（[LLM] 行 lat= 字段）
	P95Lat       float64 `json:"p95_lat"`  // 单位：秒
	CacheHitRate float64 `json:"cache_hit_rate"`
	InjectedRate float64 `json:"injected_rate"` // 注入尝试率（请求带注入的比例），非到达率
}

type snapshotQuality struct {
	SummaryCoverage    float64 `json:"summary_coverage"`
	L2NestedGoal       int     `json:"l2_nested_goal"`
	EventProcessedRate float64 `json:"event_processed_rate"`
	EventUnconsumed    int     `json:"event_unconsumed"`
	WorkflowCompleted  float64 `json:"workflow_completed_rate"`
	CorrectionSignals  int     `json:"correction_signals"`
}

type snapshotInjection struct {
	EmergeEventsTotal int     `json:"emerge_events_total"`
	ActionItems       int     `json:"action_items"`
	InjectCharsAvg    int     `json:"inject_chars"`
	// InjectReachRate 注入内容实际到达率（8/27 Claude 审核补的对照指标）：
	// 成功注入行 / (成功注入行 + suppressed 行)。skip（去重）与 no_summary_data
	// （无数据可注）是设计行为，不计入分母——防 injected_rate 100% 被误读为
	// "注入通道健康"（注入预算裁剪 8/26 实测 ~49%）。
	InjectReachRate float64 `json:"inject_reach_rate"`
}

func dispatchSnapshot(args *cli.Args, raw []string) {
	if args.Bool("diff") || args.Str("diff", "") != "" {
		var files []string
		for _, a := range raw {
			if !strings.HasPrefix(a, "--") {
				files = append(files, a)
			}
		}
		if len(files) != 2 {
			fmt.Fprintln(os.Stderr, "用法: aipmc snapshot --diff <修复前.json> <修复后.json>")
			os.Exit(1)
		}
		printSnapshotDelta(files[0], files[1])
		return
	}
	doc, err := buildSnapshot(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot 生成失败: %v\n", err)
		os.Exit(1)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot 序列化失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
	// 方案 A 落盘：.pmai/data/snapshots/latest.json（只读消费方）。
	if dir, err := pmdb.RuntimeDir(); err == nil {
		dir = filepath.Join(dir, "data", "snapshots")
		if err := os.MkdirAll(dir, 0755); err == nil {
			if err := os.WriteFile(filepath.Join(dir, "latest.json"), out, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "⚠ 快照落盘失败: %v\n", err)
			}
		}
	}
}

func buildSnapshot(args *cli.Args) (*snapshotDoc, error) {
	until := time.Now()
	since := until.Add(-24 * time.Hour)
	if v := args.Str("since", ""); v != "" {
		t, err := parseSnapshotTime(v)
		if err != nil {
			return nil, fmt.Errorf("无效 --since %q: %v", v, err)
		}
		since = t
	}
	if v := args.Str("until", ""); v != "" {
		t, err := parseSnapshotTime(v)
		if err != nil {
			return nil, fmt.Errorf("无效 --until %q: %v", v, err)
		}
		until = t
	}
	cons, cacheCoverage := snapshotConsumption(since, until)
	qual := snapshotQualityFromDB(since, until)
	inj := snapshotInjectionFromLog(since, until)
	return &snapshotDoc{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Window:        snapshotWin{Since: since.Format(time.RFC3339), Until: until.Format(time.RFC3339)},
		Metrics: snapshotMets{
			Consumption: cons,
			Quality:     qual,
			Injection:   inj,
		},
		Notes: snapshotNote{CacheHitCoverage: cacheCoverage},
	}, nil
}

func parseSnapshotTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析（示例: 2026-08-27T00:00:00）")
}

// scanLog 逐行扫描全局 proxy 日志，窗口外行跳过；回调返回 false 终止。
func scanLog(fn func(line string) bool, since, until time.Time) {
	logPath := filepath.Join(os.Getenv("HOME"), ".aipmc", "logs", "aipmc.log")
	f, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		ts, hasDate, ok := parseLogTimestamp(line)
		// 窗口总是存在（snapshot 必有 since/until），旧格式无日期行无法定位
		// 到具体日期，保守跳过（与 metrics --window 行为一致，8/12 起才有日期）。
		if !ok || !hasDate {
			continue
		}
		if ts.Before(since) || ts.After(until) {
			continue
		}
		if !fn(line) {
			return
		}
	}
}

// snapshotConsumption 按窗口聚合 [LLM] 行（口径同 metrics.go 消耗参考 + E3）：
// cache_hit_rate = cache_hit/in_tok（cache_create 上游恒 0 无信号）；injected_rate =
// injected=Y 占比；p95_lat 取窗口内 lat 分布 95 分位。返回 per-agent 聚合与
// cache_hit 字段覆盖率（responses 路径缺口记入 notes）。
func snapshotConsumption(since, until time.Time) (map[string]*agentConsumption, float64) {
	type agg struct {
		calls       int
		inTok, out  int64
		cacheHit    int64
		injectedY   int
		lats        []float64
	}
	by := map[string]*agg{}
	cacheRows, parsedRows := 0, 0
	scanLog(func(line string) bool {
		if !strings.Contains(line, "[LLM]") {
			return true
		}
		fields := parseKVFields(line)
		if fields["agent"] == "" || fields["in_tok"] == "" || fields["out_tok"] == "" {
			return true
		}
		parsedRows++
		if fields["cache_hit"] != "" || fields["n_hit"] != "" {
			cacheRows++
		}
		ag := by[fields["agent"]]
		if ag == nil {
			ag = &agg{}
			by[fields["agent"]] = ag
		}
		ag.calls++
		ag.inTok += int64(atoi(fields["in_tok"]))
		ag.out += int64(atoi(fields["out_tok"]))
		if v := fields["cache_hit"]; v != "" {
			ag.cacheHit += int64(atoi(v))
		} else if v := fields["n_hit"]; v != "" {
			ag.cacheHit += int64(atoi(v))
		}
		if fields["injected"] == "Y" {
			ag.injectedY++
		}
		if v := fields["lat"]; v != "" {
			ag.lats = append(ag.lats, atof(strings.TrimSuffix(v, "s")))
		}
		return true
	}, since, until)

	out := map[string]*agentConsumption{}
	for name, ag := range by {
		c := &agentConsumption{
			Calls:  ag.calls,
			InTok:  ag.inTok,
			OutTok: ag.out,
		}
		if ag.calls > 0 {
			c.InjectedRate = float64(ag.injectedY) / float64(ag.calls)
			if len(ag.lats) > 0 {
				sum := 0.0
				for _, l := range ag.lats {
					sum += l
				}
				c.AvgLat = sum / float64(len(ag.lats))
			}
			c.P95Lat = p95(ag.lats)
		}
		if c.InTok > 0 {
			c.CacheHitRate = float64(ag.cacheHit) / float64(c.InTok)
		}
		out[name] = c
	}
	cov := 0.0
	if parsedRows > 0 {
		cov = float64(cacheRows) / float64(parsedRows)
	}
	return out, cov
}

func p95(lats []float64) float64 {
	if len(lats) == 0 {
		return 0
	}
	sorted := append([]float64(nil), lats...)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)-1) * 0.95)
	return sorted[idx]
}

// snapshotQualityFromDB 用共享口径查询 DB 质量指标（与 metrics.go 同一套 SQL）。
func snapshotQualityFromDB(since, until time.Time) snapshotQuality {
	q := snapshotQuality{}
	db, err := pmdb.Open()
	if err != nil {
		return q
	}
	defer db.Close()
	if total, withL2, err := summaryCoverageStats(db); err == nil && total > 0 {
		q.SummaryCoverage = float64(withL2) / float64(total)
	}
	if nested, _, err := l2NestedStats(db); err == nil {
		q.L2NestedGoal = nested
	}
	sinceStr := since.Format("2006-01-02T15:04:05")
	if ev, err := collectEventStats(db, sinceStr); err == nil {
		q.EventProcessedRate = ev.actionRate
	}
	db.QueryRow(`SELECT COUNT(*) FROM events WHERE consumed_by_agent=0 AND processed_by_agent=0`).Scan(&q.EventUnconsumed)
	// workflow_completed_rate = review_json workflow_completed=true 且属于 L2 宇宙
	// （有非空 summary 的 session）/ L2 数（8/27 回算口径：13/65=20% 的 L2 分母）。
	var wfCompleted, l2Total int
	db.QueryRow(`SELECT COUNT(DISTINCT s.session_id) FROM session_summaries s
		JOIN discussion_log d ON d.session_id=s.session_id
		WHERE s.summary!='' AND d.session_id!='' AND d.session_id!='unknown'
		AND json_valid(s.review_json) AND json_extract(s.review_json,'$.workflow_completed')=1`).Scan(&wfCompleted)
	if _, withL2, err := summaryCoverageStats(db); err == nil {
		l2Total = withL2
	}
	if l2Total > 0 {
		q.WorkflowCompleted = float64(wfCompleted) / float64(l2Total)
	}
	q.CorrectionSignals = correctionSignals(db, since, until)
	return q
}

// correctionSignals 统计窗口内 user 消息命中的挫败关键词数（关键词库与
// proxy/context_inject.go detectUserFrustration 保持一致——7 词；口径：
// METRICS_SPEC §2.2 correction_signals，辅助信号，不参与门禁）。
func correctionSignals(db *sql.DB, since, until time.Time) int {
	kws := []string{"没有变化", "还是不行", "没有效果", "还是不对", "完全没用", "你的方式很垃圾", "你在干什么"}
	var conds []string
	for range kws {
		conds = append(conds, "content LIKE ?")
	}
	q := `SELECT COUNT(*) FROM discussion_log WHERE role='user'
		AND created_at >= ? AND created_at <= ? AND (` + strings.Join(conds, " OR ") + `)`
	args := []any{since.Format("2006-01-02T15:04:05"), until.Format("2006-01-02T15:04:05")}
	for _, kw := range kws {
		args = append(args, "%"+kw+"%")
	}
	var n int
	db.QueryRow(q, args...).Scan(&n)
	return n
}

// snapshotInjectionFromLog 按窗口取注入类指标：最后一次 emerge_events 的
// total/items；成功注入行（[INJECT] agent= 非 skip/suppressed/emerge）的
// chars 均值（≤800 目标，METRICS_SPEC §2.3）。
func snapshotInjectionFromLog(since, until time.Time) snapshotInjection {
	inj := snapshotInjection{}
	var charsSum, charsN int
	var okLines, supLines int
	scanLog(func(line string) bool {
		if !strings.Contains(line, "[INJECT]") {
			return true
		}
		if strings.Contains(line, "emerge_events total=") {
			inj.EmergeEventsTotal = parseFieldInt(line, "total=")
			inj.ActionItems = parseFieldInt(line, "items=")
			return true
		}
		if strings.Contains(line, "suppressed=") {
			supLines++
			return true
		}
		if strings.HasPrefix(line, "[") {
			rest := line[strings.Index(line, "]")+1:]
			rest = strings.TrimSpace(rest)
			// 成功注入 = 正常注入行（agent= 前缀）或 guidelines_only（inject 前缀），
			// 与 metrics C1 inject_coverage 的分子口径一致。
			if strings.HasPrefix(rest, "[INJECT] agent=") || strings.HasPrefix(rest, "[INJECT] inject ") {
				okLines++
				if v := parseField(line, "chars="); v != "" {
					charsSum += atoi(v)
					charsN++
				}
			}
		}
		return true
	}, since, until)
	if charsN > 0 {
		inj.InjectCharsAvg = charsSum / charsN
	}
	if okLines+supLines > 0 {
		inj.InjectReachRate = float64(okLines) / float64(okLines+supLines)
	}
	return inj
}

// printSnapshotDelta 按 METRICS_SPEC §4 输出两份快照的对比表。
func printSnapshotDelta(aPath, bPath string) {
	a, err := readSnapshot(aPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败: %v\n", aPath, err)
		os.Exit(1)
	}
	b, err := readSnapshot(bPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败: %v\n", bPath, err)
		os.Exit(1)
	}
	fmt.Printf("%-24s %14s %14s %16s\n", "指标", "修复前", "修复后", "方向")
	// 方向定义（对照注册表目标：↑=越大越好，↓=越小越好）。
	rows := []struct {
		name   string
		before string
		after  string
		bf, af float64
		upGood bool
	}{
		{"summary_coverage", pct(a.Metrics.Quality.SummaryCoverage), pct(b.Metrics.Quality.SummaryCoverage), a.Metrics.Quality.SummaryCoverage, b.Metrics.Quality.SummaryCoverage, true},
		{"event_processed_rate", pct(a.Metrics.Quality.EventProcessedRate), pct(b.Metrics.Quality.EventProcessedRate), a.Metrics.Quality.EventProcessedRate, b.Metrics.Quality.EventProcessedRate, true},
		{"workflow_completed", pct(a.Metrics.Quality.WorkflowCompleted), pct(b.Metrics.Quality.WorkflowCompleted), a.Metrics.Quality.WorkflowCompleted, b.Metrics.Quality.WorkflowCompleted, true},
		{"action_items", fmt.Sprint(a.Metrics.Injection.ActionItems), fmt.Sprint(b.Metrics.Injection.ActionItems), float64(a.Metrics.Injection.ActionItems), float64(b.Metrics.Injection.ActionItems), false},
		{"emerge_events_total", fmt.Sprint(a.Metrics.Injection.EmergeEventsTotal), fmt.Sprint(b.Metrics.Injection.EmergeEventsTotal), float64(a.Metrics.Injection.EmergeEventsTotal), float64(b.Metrics.Injection.EmergeEventsTotal), false},
		{"inject_chars", fmt.Sprint(a.Metrics.Injection.InjectCharsAvg), fmt.Sprint(b.Metrics.Injection.InjectCharsAvg), float64(a.Metrics.Injection.InjectCharsAvg), float64(b.Metrics.Injection.InjectCharsAvg), false},
		{"cache_hit_rate", pct(consumptionTotalRate(a)), pct(consumptionTotalRate(b)), consumptionTotalRate(a), consumptionTotalRate(b), true},
	}
	for _, r := range rows {
		fmt.Printf("%-24s %14s %14s %16s\n", r.name, r.before, r.after, deltaMark(r.bf, r.af, r.upGood))
	}
}

func consumptionTotalRate(d *snapshotDoc) float64 {
	var hit, tok int64
	for _, c := range d.Metrics.Consumption {
		hit += int64(float64(c.InTok) * c.CacheHitRate)
		tok += c.InTok
	}
	if tok == 0 {
		return 0
	}
	return float64(hit) / float64(tok)
}

func deltaMark(before, after float64, upGood bool) string {
	if before == after {
		return "⚪ 未变"
	}
	better := after > before
	if !upGood {
		better = after < before
	}
	if better {
		return "✅ 改善"
	}
	return "❌ 恶化"
}

func readSnapshot(path string) (*snapshotDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d snapshotDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
