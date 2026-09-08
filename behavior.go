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
var retrievalTools = map[string]bool{
	"aipm_search_context": true, "aipm_smart_search": true, "aipm_search_discussions": true,
	"aipm_read_discussions": true, "aipm_get_briefing": true, "aipm_trace_context": true,
	"aipm_get_commit": true, "aipm_get_task": true, "aipm_get_decision": true, "aipm_get_plan": true,
}

var planTools = map[string]bool{
	"aipm_create_plan": true, "aipm_create_task": true, "aipm_get_plan": true,
	"aipm_create_roadmap": true, "aipm_update_plan": true,
}

// computeBehaviorBaseline computes the 3-dimension baseline from structured events.
func computeBehaviorBaseline(events []ToolEvent) BehaviorReport {
	rep := BehaviorReport{TotalCalls: len(events)}
	sessions := map[string]bool{}
	toolSet := map[string]*TopTool{}
	var retrievalCalls, errCalls int
	planSessions := map[string]bool{}

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
	rep.BlindTry.Denominator = len(events)
	rep.BlindTry.Numerator = errCalls
	if len(events) > 0 {
		rep.BlindTry.Ratio = float64(errCalls) / float64(len(events))
	}

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
	where := ""
	var whereArgs []any
	if since != "" {
		where = " AND created_at >= ?"
		whereArgs = []any{since}
	}
	db, err := pmdb.Open()
	if err != nil {
		fmt.Printf("⚠ 无法打开项目库: %v\n", err)
		return
	}
	defer db.Close()

	rows, err := db.Query(`SELECT session_id, source, content, created_at FROM discussion_log WHERE role='tool'`+where+` ORDER BY created_at ASC`, whereArgs...)
	if err != nil {
		fmt.Printf("⚠ 查询 discussion_log role='tool' 失败: %v\n", err)
		return
	}
	defer rows.Close()

	events := []ToolEvent{}
	totalRows := 0
	for rows.Next() {
		var sid, src, content, ts string
		if err := rows.Scan(&sid, &src, &content, &ts); err != nil {
			continue
		}
		if strings.Contains(content, "mcp__aipm") || strings.Contains(content, "📡") {
			totalRows++
		}
		if ev, ok := parseToolRow(sid, src, content, ts); ok {
			events = append(events, ev)
		}
	}

	rep := computeBehaviorBaseline(events)
	if totalRows > 0 {
		rep.ParseCoverage = float64(len(events)) / float64(totalRows)
	}
	// 盲试检测(proxy): [MCP] 日志 status=ERR 占比 — discussion_log 不带状态,
	// 故从权威 [MCP] 日志取, 与 session 基线不同源, 报告明确标注来源。
	rep.BlindTry = mcpErrRate(logPath(), since)
	rep.Window = since

	if args.Bool("json") {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Println("B11 三维度行为基线（来源 discussion_log role='tool' 行 + [MCP] 日志状态）")
	fmt.Printf("窗口: %s | 解析覆盖率: %.1f%% | 会话 %d | 调用 %d\n\n", orAll(since), rep.ParseCoverage*100, rep.TotalSessions, rep.TotalCalls)
	fmt.Printf("历史检索意识: %.1f%%（%d/%d）— 检索类 aipm_* 调用占比\n", rep.RetrievalAwareness.Ratio*100, rep.RetrievalAwareness.Numerator, rep.RetrievalAwareness.Denominator)
	fmt.Printf("计划性: %.1f%%（%d/%d）— 使用过 plan/task 工具的会话占比\n", rep.Planfulness.Ratio*100, rep.Planfulness.Numerator, rep.Planfulness.Denominator)
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

func orAll(s string) string {
	if s == "" {
		return "all"
	}
	return s
}

// mcpErrRate returns the ERR ratio for aipm tool calls in the global [MCP] log.
func mcpErrRate(log, since string) BehaviorDim {
	f, err := os.Open(log)
	if err != nil {
		return BehaviorDim{}
	}
	defer f.Close()
	var total, errs int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "[MCP]") || !strings.Contains(line, "tool=") {
			continue
		}
		// 窗口对齐: [MCP] 行前缀 [YYYY-MM-DD ...], 仅统计日期 >= since 的行。
		if since != "" && len(line) >= 10 && line[:10] < since[:10] {
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
	dim := BehaviorDim{Numerator: errs, Denominator: total}
	if total > 0 {
		dim.Ratio = float64(errs) / float64(total)
	}
	return dim
}

var mcpToolRe = regexp.MustCompile(`tool=([a-z0-9_]+)`)
var mcpStatusRe = regexp.MustCompile(`status=([A-Z]+)`)

func logPath() string {
	return filepath.Join(os.Getenv("HOME"), ".aipmc", "logs", "aipmc.log")
}
