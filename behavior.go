package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"aipmc/cli"
	pmdb "aipmc/db"
)

// ============================================================
// B10/B11: 结构化事件流 + 三维度行为基线
// 数据源: discussion_log 中 role='tool' 的行 (📡 <tool> <status>)
// B10 = 把 tool 行解析成 {ts, session, agent, tool, status} 结构化事件
// B11 = 用这些事件跑出 三维度行为基线 (历史检索意识 / 计划性 / 盲试检测)
// 只读、非持续服务、跑一次出数字报告(与 mcp 基线口径一致, 排除 auto 章)。
// ============================================================

// toolRowRe recognizes AIPM MCP tool calls in discussion_log role='tool' rows.
// Real rows use client-native notation: "🛠 mcp__aipm__aipm_<tool>" (claude-code)
// or the aipmc-documented "📡 aipm_<tool>". Non-aipm tools (🔧/👁/📝/🆕) are excluded.
var toolRowRe = regexp.MustCompile(`(?:🛠\s*mcp__aipm__|📡\s*)(aipm_[a-z0-9_]+)`)

// ToolEvent is one structured MCP tool call parsed from a discussion_log tool row.
type ToolEvent struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent"`
	Tool      string `json:"tool"`
	Status    string `json:"status"` // "ok" | "err"
	Ts        string `json:"ts"`
}

// BehaviorDim is one dimension of the 3-dimension behavior baseline.
type BehaviorDim struct {
	Ratio       float64 `json:"ratio"`
	Numerator   int     `json:"numerator"`
	Denominator int     `json:"denominator"`
}

// BehaviorReport is the one-shot 3-dimension behavior baseline.
type BehaviorReport struct {
	TotalSessions      int         `json:"total_sessions"`
	TotalCalls         int         `json:"total_calls"`
	ParseCoverage      float64     `json:"parse_coverage"`
	RetrievalAwareness BehaviorDim `json:"retrieval_awareness"`
	Planfulness        BehaviorDim `json:"planfulness"`
	BlindTry           BehaviorDim `json:"blind_try"`
	PeerAwareness      BehaviorDim `json:"peer_awareness"`
	TopTools           []TopTool   `json:"top_tools"`
	Window             string      `json:"window,omitempty"`
}

// TopTool is the per-tool count summary.
type TopTool struct {
	Tool   string `json:"tool"`
	Calls  int    `json:"calls"`
	Errors int    `json:"errors"`
}

// parseToolRow extracts {tool, status} from a discussion_log tool-row content.
// Returns ok=false when no 📡 <tool> token is found (unparseable → B10 coverage).
func parseToolRow(sessionID, agent, content, ts string) (ToolEvent, bool) {
	m := toolRowRe.FindStringSubmatch(content)
	if m == nil {
		return ToolEvent{}, false
	}
	status := "ok"
	if strings.Contains(content, "❌") || strings.Contains(content, "✗") ||
		strings.Contains(content, "ERR") || strings.Contains(content, "失败") ||
		strings.Contains(content, "err=") {
		status = "err"
	}
	return ToolEvent{SessionID: sessionID, Agent: agent, Tool: m[1], Status: status, Ts: ts}, true
}

// retrievalTools / planTools define the dimension cohorts. These are AIPM tools
// that expose "context acquisition" (检索) and "planning" (计划) respectively.
// 注: aipm_get_briefing 在系统层面兼作 E5b 执行率探针(强制注入的基线), 本维度按
// 「获取上下文」归入检索类——双归类仅为口径说明, 不改变报告的方向性结论。
var retrievalTools = map[string]bool{
	"aipm_search_context": true, "aipm_smart_search": true, "aipm_search_discussions": true,
	"aipm_read_discussions": true, "aipm_get_briefing": true, "aipm_trace_context": true,
	"aipm_get_commit": true, "aipm_get_task": true, "aipm_get_decision": true, "aipm_get_plan": true,
}

var planTools = map[string]bool{
	"aipm_create_plan": true, "aipm_create_task": true, "aipm_get_plan": true,
	"aipm_create_roadmap": true, "aipm_update_plan": true,
}

// peerAwareTools: L1 同行感知工具——用 `list_sessions` 查看跨 agent 状态板,
// 是「感知同行在做什么」的行为信号(agent 协作感知 L1)。
var peerAwareTools = map[string]bool{"aipm_list_sessions": true}

// computeBehaviorBaseline computes the 3-dimension baseline from structured events.
func computeBehaviorBaseline(events []ToolEvent) BehaviorReport {
	rep := BehaviorReport{TotalCalls: len(events)}
	sessions := map[string]bool{}
	toolSet := map[string]*TopTool{}
	var retrievalCalls, errCalls int
	planSessions := map[string]bool{}
	peerSessions := map[string]bool{}

	for _, e := range events {
		if e.Tool == "" {
			continue
		}
		if e.SessionID != "" {
			sessions[e.SessionID] = true
		}
		rep.RetrievalAwareness.Denominator++
		if retrievalTools[e.Tool] {
			rep.RetrievalAwareness.Numerator++
			retrievalCalls++
		}
		if planTools[e.Tool] && e.SessionID != "" {
			planSessions[e.SessionID] = true
		}
		if peerAwareTools[e.Tool] && e.SessionID != "" {
			peerSessions[e.SessionID] = true
		}
		if e.Status == "err" {
			errCalls++
		}
		tt := toolSet[e.Tool]
		if tt == nil {
			tt = &TopTool{Tool: e.Tool}
			toolSet[e.Tool] = tt
		}
		tt.Calls++
		if e.Status == "err" {
			tt.Errors++
		}
	}

	rep.TotalSessions = len(sessions)
	if denom := rep.RetrievalAwareness.Denominator; denom > 0 {
		rep.RetrievalAwareness.Ratio = float64(retrievalCalls) / float64(denom)
	}
	rep.Planfulness.Denominator = len(sessions)
	rep.Planfulness.Numerator = len(planSessions)
	if len(sessions) > 0 {
		rep.Planfulness.Ratio = float64(len(planSessions)) / float64(len(sessions))
	}
	rep.PeerAwareness.Denominator = len(sessions)
	rep.PeerAwareness.Numerator = len(peerSessions)
	if len(sessions) > 0 {
		rep.PeerAwareness.Ratio = float64(len(peerSessions)) / float64(len(sessions))
	}
	rep.BlindTry.Denominator = len(events)
	rep.BlindTry.Numerator = errCalls
	if len(events) > 0 {
		rep.BlindTry.Ratio = float64(errCalls) / float64(len(events))
	}
	// 盲试检测(session 派生口径): 来自讨论行内带 ❌/ERR/失败 标记的事件。此值在 CLI
	// 报告路径被权威 [MCP] 日志的 mcpErrRate 覆盖——这里保留纯 session 版仅供测试/
	// 独立复算, 不作为最终报告值(报告用 [MCP] 日志, 见 runBehaviorBaseline)。

	// top tools, most-called first
	for _, tt := range toolSet {
		rep.TopTools = append(rep.TopTools, *tt)
	}
	sort.Slice(rep.TopTools, func(i, j int) bool {
		if rep.TopTools[i].Calls != rep.TopTools[j].Calls {
			return rep.TopTools[i].Calls > rep.TopTools[j].Calls
		}
		return rep.TopTools[i].Tool < rep.TopTools[j].Tool
	})
	if len(rep.TopTools) > 10 {
		rep.TopTools = rep.TopTools[:10]
	}
	return rep
}

// runBehaviorBaseline implements `aipmc metrics --behavior` (B11 one-shot report).
func runBehaviorBaseline(args *cli.Args) {
	since := args.Str("since", "")
	until := args.Str("until", "")
	rep, err := behaviorReport(since, until)
	if err != nil {
		fmt.Printf("⚠ 行为基线构建失败: %v\n", err)
		return
	}
	if args.Bool("json") {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return
	}
	printBehaviorReport(rep, windowLabel(since, until))
}

// loadToolEvents loads + parses discussion_log role='tool' rows in the [since, until)
// window. `until` is a date-only upper bound (whole day included via dayAfter).
func loadToolEvents(since, until string) ([]ToolEvent, int, error) {
	where := ""
	var whereArgs []any
	if since != "" {
		where = " AND created_at >= ?"
		whereArgs = append(whereArgs, since)
	}
	if until != "" {
		where += " AND created_at < ?"
		whereArgs = append(whereArgs, dayAfter(until))
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT session_id, source, content, created_at FROM discussion_log WHERE role='tool'`+where+` ORDER BY created_at ASC`, whereArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events := []ToolEvent{}
	totalRows := 0
	for rows.Next() {
		var sid, src, content, ts string
		if rows.Scan(&sid, &src, &content, &ts) != nil {
			continue
		}
		if strings.Contains(content, "mcp__aipm") || strings.Contains(content, "📡") {
			totalRows++
		}
		if ev, ok := parseToolRow(sid, src, content, ts); ok {
			events = append(events, ev)
		}
	}
	return events, totalRows, nil
}

// behaviorReport builds the 3-dimension behavior baseline (blind-try overridden by
// the authoritative [MCP] log family) plus peer-awareness rate for [since, until).
func behaviorReport(since, until string) (BehaviorReport, error) {
	events, totalRows, err := loadToolEvents(since, until)
	if err != nil {
		return BehaviorReport{}, err
	}
	rep := computeBehaviorBaseline(events)
	if totalRows > 0 {
		rep.ParseCoverage = float64(len(events)) / float64(totalRows)
	}
	rep.BlindTry = mcpErrRate(logsDir(), since, until)
	if since != "" || until != "" {
		rep.Window = windowLabel(since, until)
	}
	return rep, nil
}

// printBehaviorReport renders the behavior baseline report (B11 one-shot + B12 panel).
func printBehaviorReport(rep BehaviorReport, win string) {
	fmt.Println("行为基线三维度+同行感知（来源 discussion_log role='tool' 行 + [MCP] 日志族状态）")
	if win != "" {
		fmt.Printf("窗口: %s | ", win)
	}
	fmt.Printf("解析覆盖率: %.1f%% | 会话 %d | 调用 %d\n\n", rep.ParseCoverage*100, rep.TotalSessions, rep.TotalCalls)
	fmt.Printf("历史检索意识: %.1f%%（%d/%d）— 检索类 aipm_* 调用占比\n", rep.RetrievalAwareness.Ratio*100, rep.RetrievalAwareness.Numerator, rep.RetrievalAwareness.Denominator)
	fmt.Printf("计划性: %.1f%%（%d/%d）— 使用过 plan/task 工具的会话占比\n", rep.Planfulness.Ratio*100, rep.Planfulness.Numerator, rep.Planfulness.Denominator)
	fmt.Printf("同行感知: %.1f%%（%d/%d）— 使用过 list_sessions 的会话占比\n", rep.PeerAwareness.Ratio*100, rep.PeerAwareness.Numerator, rep.PeerAwareness.Denominator)
	fmt.Printf("盲试检测: %.1f%%（%d/%d）— 工具调用报错占比\n", rep.BlindTry.Ratio*100, rep.BlindTry.Numerator, rep.BlindTry.Denominator)
	fmt.Println("\nTOP 工具:")
	for _, tt := range rep.TopTools {
		flag := ""
		if tt.Errors > 0 {
			flag = fmt.Sprintf(" err=%d", tt.Errors)
		}
		fmt.Printf("  %-30s %d%s\n", tt.Tool, tt.Calls, flag)
	}
}

// printBehaviorPanel renders the B12 behavior baseline rows inside the regular
// metrics panel. These are directional (非因果) signals, not gates — hence target
// marked "参考" and ok=true to avoid false alarms in the panel's ❌/✅.
func printBehaviorPanel(rep BehaviorReport, win string) {
	if win != "" {
		fmt.Printf("  窗口 %s\n", win)
	}
	printRow("B12 retrieval_awareness", pct(rep.RetrievalAwareness.Ratio)+fmt.Sprintf(" (%d/%d)", rep.RetrievalAwareness.Numerator, rep.RetrievalAwareness.Denominator), "参考(方向性)", true)
	printRow("B12 planfulness", pct(rep.Planfulness.Ratio)+fmt.Sprintf(" (%d/%d)", rep.Planfulness.Numerator, rep.Planfulness.Denominator), "参考(方向性)", true)
	printRow("B12 peer_awareness", pct(rep.PeerAwareness.Ratio)+fmt.Sprintf(" (%d/%d)", rep.PeerAwareness.Numerator, rep.PeerAwareness.Denominator), "参考(L1同行感知)", true)
	printRow("B12 blind_try", pct(rep.BlindTry.Ratio)+fmt.Sprintf(" (%d/%d)", rep.BlindTry.Numerator, rep.BlindTry.Denominator), "参考([MCP]ERR占比)", true)
}

func orAll(s string) string {
	if s == "" {
		return "all"
	}
	return s
}

// windowLabel renders the report window as "since→until" (or "since→now" /
// "all"). Missing bounds map to "all"/"now".
func windowLabel(since, until string) string {
	if since == "" && until == "" {
		return "all"
	}
	s := orAll(since)
	if until == "" {
		return s + "→now"
	}
	return s + "→" + until
}

// dayOnly truncates a bound to its YYYY-MM-DD day (matching the log-day window in
// metrics/mcp_baseline.py / mcp_compare.py). Non-date strings pass through unchanged.
func dayOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// dayAfter returns the exclusive day upper bound for a date-only `until` (YYYY-MM-DD)
// so the whole `until` day is included via `created_at < dayAfter(until)`. Non-date
// bounds (full ISO datetime) pass through unchanged.
func dayAfter(bound string) string {
	t, err := time.ParseInLocation("2006-01-02", bound, time.Local)
	if err != nil {
		return bound
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// mcpErrRate returns the ERR ratio for aipm tool calls in the global [MCP] log family,
// spanning the current aipmc.log and all rotated aipmc.log.* archives (glob pattern,
// same as metrics/mcp_baseline.py load_calls). Window bounds are log-day granularity.
func mcpErrRate(dir, since, until string) BehaviorDim {
	sinceDay := dayOnly(since)
	untilDay := dayOnly(until)
	files, err := filepath.Glob(filepath.Join(dir, "aipmc.log*"))
	if err != nil {
		return BehaviorDim{}
	}
	sort.Strings(files)
	var total, errs int
	for _, path := range files {
		base := filepath.Base(path)
		// 只读主日志 (aipmc.log) 与轮转归档 (aipmc.log.<ts>)，跳过非日志同名文件。
		if !strings.HasSuffix(base, ".log") && !strings.Contains(base, ".log.") {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if !strings.Contains(line, "[MCP]") || !strings.Contains(line, "tool=") {
				continue
			}
			dateM := mcpDateRe.FindStringSubmatch(line)
			if dateM == nil {
				continue
			}
			date := dateM[1]
			if sinceDay != "" && date < sinceDay {
				continue
			}
			if untilDay != "" && date > untilDay {
				continue
			}
			toolM := mcpToolRe.FindStringSubmatch(line)
			if toolM == nil || !strings.HasPrefix(toolM[1], "aipm_") {
				continue
			}
			statusM := mcpStatusRe.FindStringSubmatch(line)
			if statusM == nil {
				continue
			}
			total++
			if statusM[1] == "ERR" {
				errs++
			}
		}
		f.Close()
	}
	dim := BehaviorDim{Numerator: errs, Denominator: total}
	if total > 0 {
		dim.Ratio = float64(errs) / float64(total)
	}
	return dim
}

var mcpToolRe = regexp.MustCompile(`tool=([a-z0-9_]+)`)
var mcpStatusRe = regexp.MustCompile(`status=([A-Z]+)`)
var mcpDateRe = regexp.MustCompile(`\[(\d{4}-\d{2}-\d{2}) \d{2}:\d{2}:\d{2}\]`)

func logsDir() string {
	return filepath.Join(os.Getenv("HOME"), ".aipmc", "logs")
}
