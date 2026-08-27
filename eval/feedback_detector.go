package eval

// 事后反馈检测器（B 线 P0，8/27 v13.1）：纯正则、零语义、不碰热路径（批处理模块）。
// 检测两类信号：
//   1. 实体引用未查询（F5 类）：assistant 消息引用 decision-/task-/bug-/commit-/
//      plan-/thread-/idea- ID，但该 session 从未调用该实体类型对应的任何查询工具
//      （get_X / list_X / search_context / smart_search / read_discussions 等）
//      → 强漏查候选（MissingQueries 输出全部期望工具名）。
//      已调用过该类工具则保守不判漏查（工具调用行无参数，无法验证查的是否同一
//      ID——宁可漏报不误报，弱信号限制，见注释）。
//   2. 数据源引用规范性：assistant 消息中数字声明（N 次/条/行/天/%…）前后 40
//      字符内是否有来源词（[MCP]/日志/discussion_log/实测/库…），无 → 规范性信号。
// 输出（钉 1 接口契约）: FeedbackGap{SessionID, Source, EntityRefs[], MissingQueries[],
//   DataSourceRefs[], Timestamp} → C2 线回填 session_summaries(entity_refs)。
// 约束 A（accepted）：本检测输出"引用了 decision-X 但未查询"是行为事实（可验证的
// 调用记录），允许注入；模式化标签（"伪进展"等）禁止注入，由 C2 线去模式化。

import (
	"database/sql"
	"regexp"
	"sort"
	"strings"
)

var (
	// entityIDRe 匹配 AIPM 实体 ID：<type>-YYYYMMDD-HHMMSS-xxxxxx（库内全小写，引用时容忍大小写）。
	entityIDRe = regexp.MustCompile(`(?i)\b(decision|task|bug|commit|plan|thread|idea)-\d{8}-\d{6}-[0-9a-f]{6}\b`)
	// mcpCallRe 匹配 hook 写入的工具调用行：🛠 mcp__aipm__aipm_get_decision
	mcpCallRe = regexp.MustCompile(`(?i)mcp__aipm__aipm_([a-z_]+)`)
	// numClaimRe 数字声明：N 次/条/行/天/个/%/倍/小时
	numClaimRe = regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:次|条|行|天|个|%|倍|小时)`)
	// sourceWords 数据源来源词（权威口径词 + 通用来源词）。
	// 8/27 审核收敛：去掉「库/日志/记录/统计」等中文高频泛词（几乎任何讨论都会在
	// 窗口内命中 → HasSource 恒 true，数据源维度形同虚设），只保留精确来源词。
	sourceWords = []string{"[mcp]", "discussion_log", "实测", "日志统计", "口径"}
	// entityQueryTools 实体类型 → 期望查询工具（命中任一即视为该类型已查询）。
	entityQueryTools = map[string][]string{
		"decision": {"aipm_get_decision", "aipm_list_decisions", "aipm_search_context", "aipm_smart_search"},
		"task":     {"aipm_get_task", "aipm_list_tasks", "aipm_search_context", "aipm_smart_search"},
		"bug":      {"aipm_get_bug", "aipm_list_bugs", "aipm_search_context", "aipm_smart_search"},
		"commit":   {"aipm_get_commit", "aipm_list_commits"},
		"plan":     {"aipm_get_plan", "aipm_list_plans", "aipm_search_context", "aipm_smart_search"},
		"thread":   {"aipm_read_discussions", "aipm_search_discussions", "aipm_trace_context"},
		"idea":     {"aipm_search_context", "aipm_smart_search"},
	}
)

// EntityRef 一处实体引用（纯正则提取，零语义）。
type EntityRef struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Context string `json:"context"` // 引用所在行截断（供人类核对）
	MsgID   string `json:"message_id"`
	Ts      string `json:"ts"`
}

// DataSourceRef 一处数字声明及是否有来源词。
type DataSourceRef struct {
	Claim     string `json:"claim"`
	HasSource bool   `json:"has_source"`
	MsgID     string `json:"message_id"`
}

// FeedbackGap 一个 session 的事后反馈缺口（钉 1 接口契约的输出 schema）。
type FeedbackGap struct {
	SessionID      string          `json:"session_id"`
	Source         string          `json:"source"`
	Timestamp      string          `json:"timestamp"`
	EntityRefs     []EntityRef     `json:"entity_refs"`
	MissingQueries []string        `json:"missing_queries"` // 强漏查：引用类型对应的期望查询工具全部未调用
	DataSourceRefs []DataSourceRef `json:"data_source_refs"`
}

// DetectFeedbackGaps 扫描 since 之后活跃（至少 1 条非 tool 消息）的 session，
// 按最后活跃时间降序取前 limit 个，逐个检测反馈缺口。
func DetectFeedbackGaps(db *sql.DB, since string, limit int) ([]FeedbackGap, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.Query(`
		SELECT source, session_id, MAX(created_at)
		FROM discussion_log
		WHERE created_at >= ? AND session_id != '' AND session_id != 'unknown' AND role != 'tool'
		GROUP BY source, session_id
		ORDER BY MAX(created_at) DESC
		LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 先完整收集 session 列表并关闭 rows，再逐个检测——避免 rows 未关时
	// 单连接（测试/小连接池）下内层查询等连接死锁。
	type sess struct {
		src, sid, lastSeen string
	}
	var sessions []sess
	for rows.Next() {
		var src, sid, lastSeen string
		if err := rows.Scan(&src, &sid, &lastSeen); err != nil {
			continue
		}
		sessions = append(sessions, sess{src, sid, lastSeen})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	var gaps []FeedbackGap
	for _, s := range sessions {
		gap, err := detectSessionGap(db, s.src, s.sid, s.lastSeen, since)
		if err != nil {
			continue
		}
		if len(gap.EntityRefs) > 0 || len(gap.DataSourceRefs) > 0 {
			gaps = append(gaps, *gap)
		}
	}
	return gaps, nil
}

// detectSessionGap 单个 session：assistant 消息提取实体引用 + 数据源声明，
// 工具调用行收集已调用工具，最后做类型级漏查判定。
func detectSessionGap(db *sql.DB, src, sid, lastSeen, since string) (*FeedbackGap, error) {
	gap := &FeedbackGap{
		SessionID:      sid,
		Source:         src,
		Timestamp:      lastSeen,
		EntityRefs:     []EntityRef{},
		MissingQueries: []string{},
		DataSourceRefs: []DataSourceRef{},
	}
	rows, err := db.Query(
		`SELECT id, role, content, created_at FROM discussion_log
		 WHERE session_id = ? AND created_at >= ? ORDER BY created_at ASC, rowid ASC`,
		sid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	called := map[string]bool{} // 已调用的 aipm 工具（aipm_ 前缀）
	for rows.Next() {
		var id, role, content, createdAt string
		if err := rows.Scan(&id, &role, &content, &createdAt); err != nil {
			continue
		}
		if role == "tool" {
			for _, m := range mcpCallRe.FindAllStringSubmatch(content, -1) {
				if len(m) > 1 {
					called["aipm_"+strings.ToLower(m[1])] = true
				}
			}
			continue
		}
		if role != "assistant" {
			continue // 只检 agent 自身行为，user 引用不算漏查信号
		}
		if strings.HasPrefix(content, "📡") {
			continue // 📡 工具结果摘要行（hook 写入 role=assistant）：task-xxx 是工具参数
			// 非 agent 正文引用——提取会语义颠倒（执行了操作被当"没查过"），8/27 审核
		}
		for _, m := range entityIDRe.FindAllStringSubmatch(content, -1) {
			if len(m) < 2 {
				continue
			}
			gap.EntityRefs = append(gap.EntityRefs, EntityRef{
				Type:    strings.ToLower(m[1]),
				ID:      m[0],
				Context: refContext(content, m[0]),
				MsgID:   id,
				Ts:      createdAt,
			})
		}
		for _, m := range numClaimRe.FindAllString(content, -1) {
			idx := strings.Index(content, m)
			if idx < 0 {
				continue
			}
			lo, hi := idx-40, idx+40
			if lo < 0 {
				lo = 0
			}
			if hi > len(content) {
				hi = len(content)
			}
			gap.DataSourceRefs = append(gap.DataSourceRefs, DataSourceRef{
				Claim:     m,
				HasSource: hasSourceWord(content[lo:hi]),
				MsgID:     id,
			})
		}
	}

	// 类型级强漏查：引用过的类型，其期望查询工具全部未调用 → 输出 MissingQueries。
	refTypes := map[string]bool{}
	for _, r := range gap.EntityRefs {
		refTypes[r.Type] = true
	}
	for typ := range refTypes {
		tools := entityQueryTools[typ]
		if len(tools) == 0 {
			continue
		}
		var missing []string
		for _, t := range tools {
			if !called[t] {
				missing = append(missing, t)
			}
		}
		// 全部未调用才判强漏查（调用过任一 → 保守不判，工具行无参数无法验证具体 ID）。
		if len(missing) == len(tools) {
			gap.MissingQueries = append(gap.MissingQueries, missing...)
		}
	}
	sort.Strings(gap.MissingQueries)
	gap.MissingQueries = dedupStrings(gap.MissingQueries)
	return gap, nil
}

// dedupStrings 去重并保持输入顺序（MissingQueries 跨实体类型会重复输出公共工具）。
func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// refContext 取引用所在行（按 \n 切分找含 ID 的行），截断 120 字供核对。
func refContext(content, id string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, id) {
			line = strings.TrimSpace(line)
			if len(line) > 120 {
				line = line[:120] + "…"
			}
			return line
		}
	}
	return ""
}

// hasSourceWord 窗口内是否含任一来源词（小写比较）。
func hasSourceWord(window string) bool {
	w := strings.ToLower(window)
	for _, s := range sourceWords {
		if strings.Contains(w, s) {
			return true
		}
	}
	return false
}
