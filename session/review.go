package session

import (
	"encoding/json"
	"regexp"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

var (
	entityIDPattern = regexp.MustCompile(`(?i)(task|plan|decision|commit|bug)-\d{8}-\d{6}-[a-f0-9]{6}`)
	filePathPattern = regexp.MustCompile(`[a-zA-Z0-9_./-]+\.(?:go|js|jsx|ts|tsx|md|sql|json)`)
	sqlitePattern   = regexp.MustCompile(`(?i)sqlite3|\.pmai/data/pmai\.db`)
)

// ReviewResult is the B1 rule output for one session.
type ReviewResult struct {
	SessionID         string         `json:"session_id"`
	Source            string         `json:"source"`
	Intent            string         `json:"intent"`
	WorkflowBaseline  bool           `json:"workflow_baseline"`
	WorkflowCompleted bool           `json:"workflow_completed"`
	MCPTools          []string       `json:"mcp_tools"`
	MCPCompliance     mcpCompliance  `json:"mcp_compliance"`
	HookCoverage      string         `json:"hook_coverage"`
	SQLite3Violation  bool           `json:"sqlite3_violation"`
	Layer0Edges       []layer0Edge   `json:"layer0_edges"`
	OrphanMCPEvents   []orphanEvent  `json:"orphan_mcp_events"`
	Findings          []taggedItem   `json:"findings"`
	Positives         []taggedItem   `json:"positives"`
	MergedMCPCount    int            `json:"merged_mcp_count"`
	UserPromptCount   int            `json:"user_prompt_count"`
	ToolCallCount     int            `json:"tool_call_count"`
	DirectiveSession  bool           `json:"directive_session"`
}

type mcpCompliance struct {
	HasBriefing      bool     `json:"has_briefing"`
	HasMarkConsumed  bool     `json:"has_mark_consumed"`
	HasReadDiscussions bool   `json:"has_read_discussions"`
	HasSearchContext bool     `json:"has_search_context"`
	HasRecordCommit  bool     `json:"has_record_commit"`
	HasCreateTask    bool     `json:"has_create_task"`
	Missing          []string `json:"missing"`
}

type layer0Edge struct {
	Type   string  `json:"type"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	Weight float64 `json:"weight"`
}

type orphanEvent struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
}

type taggedItem struct {
	Tag      string `json:"tag"`
	Evidence string `json:"evidence"`
}

// ReviewSession applies B1 rules to merged session messages.
func ReviewSession(sessionID, source string, messages []map[string]any, mergedMCP int, orphans []orphanEvent) ReviewResult {
	tools := extractMCPTools(messages)
	compliance := buildCompliance(tools)
	intent := inferIntent(messages, compliance)
	userCount, toolCount := countRoles(messages)
	hookCoverage := hookCoverageStatus(userCount, toolCount, messages)
	sqliteViolation := detectSQLiteViolation(messages)
	edges := buildLayer0Edges(sessionID, messages)
	baseline := compliance.HasBriefing
	completed := workflowCompleted(intent, compliance, messages)
	findings, positives := buildFindings(sessionID, intent, baseline, completed, compliance, hookCoverage, sqliteViolation, messages)
	isDirective := isDirectiveSession(messages)

	return ReviewResult{
		SessionID:         sessionID,
		Source:            source,
		Intent:            intent,
		WorkflowBaseline:  baseline,
		WorkflowCompleted: completed,
		MCPTools:          tools,
		MCPCompliance:     compliance,
		HookCoverage:      hookCoverage,
		SQLite3Violation:  sqliteViolation,
		Layer0Edges:       edges,
		OrphanMCPEvents:   orphans,
		Findings:          findings,
		Positives:         positives,
		MergedMCPCount:    mergedMCP,
		UserPromptCount:   userCount,
		ToolCallCount:     toolCount,
		DirectiveSession:  isDirective,
	}
}

// QualityScore exposes score from ReviewResult (helper for store layer).
func (r ReviewResult) QualityScoreValue() int {
	score := 100
	if !r.WorkflowBaseline {
		score -= 30
	}
	if !r.WorkflowCompleted {
		score -= 25
	}
	if len(r.MCPCompliance.Missing) > 0 {
		score -= 10 * len(r.MCPCompliance.Missing)
	}
	if r.HookCoverage == "incomplete" {
		score -= 15
	}
	if r.SQLite3Violation {
		score -= 20
	}
	if score < 0 {
		score = 0
	}
	return score
}

func extractMCPTools(messages []map[string]any) []string {
	seen := map[string]bool{}
	var tools []string
	for _, m := range messages {
		content := u.Str(m["content"])
		if !IsMCPLog(content) {
			continue
		}
		tool := ParseMCPTool(content)
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		tools = append(tools, tool)
	}
	if tools == nil {
		tools = []string{}
	}
	return tools
}

func buildCompliance(tools []string) mcpCompliance {
	has := func(name string) bool {
		for _, t := range tools {
			if t == name {
				return true
			}
		}
		return false
	}
	c := mcpCompliance{
		HasBriefing:        has("aipm_get_briefing"),
		HasMarkConsumed:    has("aipm_mark_consumed"),
		HasReadDiscussions: has("aipm_read_discussions") || has("aipm_search_discussions"),
		HasSearchContext:   has("aipm_search_context") || has("aipm_smart_search"),
		HasRecordCommit:    has("aipm_record_commit"),
		HasCreateTask:      has("aipm_create_task"),
	}
	var missing []string
	if !c.HasBriefing {
		missing = append(missing, "get_briefing")
	}
	c.Missing = missing
	return c
}

func inferIntent(messages []map[string]any, c mcpCompliance) string {
	if c.HasRecordCommit || c.HasCreateTask {
		return "coding"
	}
	var userText strings.Builder
	for _, m := range messages {
		if u.Str(m["role"]) != "user" {
			continue
		}
		userText.WriteString(u.Str(m["content"]))
		userText.WriteByte('\n')
	}
	text := strings.ToLower(userText.String())
	discussionKW := []string{"讨论", "意见", "想法", "回应", "不要修改", "不要改代码", "说说", "评价", "对齐", "交换"}
	for _, kw := range discussionKW {
		if strings.Contains(text, kw) {
			return "discussion"
		}
	}
	reconKW := []string{"查看", "了解", "搜索", "有什么", "总结", "看看", "读取", "最近发言"}
	for _, kw := range reconKW {
		if strings.Contains(text, kw) {
			return "recon"
		}
	}
	codingKW := []string{"实现", "修复", "写代码", "commit", "开工", "实施", "删除", "添加"}
	for _, kw := range codingKW {
		if strings.Contains(text, kw) {
			return "coding"
		}
	}
	return "recon"
}

func workflowCompleted(intent string, c mcpCompliance, messages []map[string]any) bool {
	if !c.HasBriefing {
		return false
	}
	switch intent {
	case "discussion":
		if !(c.HasMarkConsumed || c.HasReadDiscussions) {
			return false
		}
		return hasSubstantiveAssistant(messages)
	case "recon":
		return c.HasSearchContext || c.HasReadDiscussions
	case "coding":
		if !c.HasMarkConsumed {
			return false
		}
		return c.HasRecordCommit || c.HasCreateTask
	default:
		return false
	}
}

func hasSubstantiveAssistant(messages []map[string]any) bool {
	for _, m := range messages {
		if u.Str(m["role"]) != "assistant" {
			continue
		}
		content := u.Str(m["content"])
		if IsMCPLog(content) || strings.HasPrefix(content, "[tool:") {
			continue
		}
		if len([]rune(content)) >= 80 {
			return true
		}
	}
	return false
}

func countRoles(messages []map[string]any) (users, tools int) {
	toolPrefixes := []string{"🔧", "📝", "👁", "🔍", "🆕", "🛠", "📡", "💭", "🗑", "📂", "🌐", "❓", "🤖", "📋"}
	for _, m := range messages {
		role := u.Str(m["role"])
		content := u.Str(m["content"])
		if role == "user" {
			users++
			continue
		}
		if role == "tool" {
			tools++
			continue
		}
		if role == "assistant" {
			for _, p := range toolPrefixes {
				if strings.HasPrefix(content, p) {
					tools++
					break
				}
			}
		}
	}
	return users, tools
}

func hookCoverageStatus(userCount, toolCount int, messages []map[string]any) string {
	if userCount == 0 && len(messages) > 0 {
		return "incomplete"
	}
	if userCount <= 1 && toolCount > 20 {
		return "incomplete"
	}
	return "complete"
}

func detectSQLiteViolation(messages []map[string]any) bool {
	for _, m := range messages {
		role := u.Str(m["role"])
		if role != "user" && role != "assistant" {
			continue
		}
		content := u.Str(m["content"])
		if IsMCPLog(content) {
			continue
		}
		if sqlitePattern.MatchString(content) {
			return true
		}
	}
	return false
}

func buildLayer0Edges(sessionID string, messages []map[string]any) []layer0Edge {
	from := "session:" + sessionID
	seen := map[string]bool{}
	var edges []layer0Edge
	var text strings.Builder
	for _, m := range messages {
		text.WriteString(u.Str(m["content"]))
		text.WriteByte('\n')
	}
	body := text.String()
	for _, m := range entityIDPattern.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		key := m[1] + ":" + m[0]
		if seen[key] {
			continue
		}
		seen[key] = true
		if store.EntityExists(m[1], m[0]) {
			edges = append(edges, layer0Edge{Type: "entity_ref", From: from, To: key, Weight: 1})
		}
	}
	fileSeen := map[string]bool{}
	for _, path := range filePathPattern.FindAllString(body, -1) {
		if strings.Count(path, "/") == 0 && !strings.Contains(path, ".go") {
			continue
		}
		if fileSeen[path] {
			continue
		}
		fileSeen[path] = true
		edges = append(edges, layer0Edge{Type: "file_overlap", From: from, To: "file:" + path, Weight: 1})
	}
	if edges == nil {
		edges = []layer0Edge{}
	}
	return edges
}

func buildFindings(sessionID, intent string, baseline, completed bool, c mcpCompliance, hook string, sqliteViolation bool, messages []map[string]any) ([]taggedItem, []taggedItem) {
	var findings, positives []taggedItem
	evidence := firstUserMessageID(messages)
	if !baseline {
		findings = append(findings, taggedItem{Tag: "missing_get_briefing", Evidence: evidence})
	}
	if baseline && !c.HasMarkConsumed && intent != "recon" {
		findings = append(findings, taggedItem{Tag: "missing_mark_consumed", Evidence: evidence})
	}
	if !completed {
		findings = append(findings, taggedItem{Tag: "workflow_incomplete", Evidence: sessionID})
	}
	if hook == "incomplete" {
		findings = append(findings, taggedItem{Tag: "hook_coverage_incomplete", Evidence: sessionID})
	}
	if sqliteViolation {
		findings = append(findings, taggedItem{Tag: "sqlite3_direct_query", Evidence: evidence})
	}
	if completed && baseline && len(findings) == 0 {
		positives = append(positives, taggedItem{Tag: "workflow_completed", Evidence: sessionID})
	}
	if c.HasReadDiscussions && hasSubstantiveAssistant(messages) {
		positives = append(positives, taggedItem{Tag: "cross_agent_read", Evidence: sessionID})
	}
	if findings == nil {
		findings = []taggedItem{}
	}
	if positives == nil {
		positives = []taggedItem{}
	}
	return findings, positives
}

func firstUserMessageID(messages []map[string]any) string {
	for _, m := range messages {
		if u.Str(m["role"]) == "user" {
			return u.Str(m["id"])
		}
	}
	if len(messages) > 0 {
		return u.Str(messages[0]["id"])
	}
	return ""
}

// isDirectiveSession checks whether the session's first user prompt contains
// an explicit instruction (entity ID reference or directive keyword without
// question words). Directive sessions are exempt from "search-first" metrics.
func isDirectiveSession(messages []map[string]any) bool {
	firstUser := ""
	for _, m := range messages {
		if u.Str(m["role"]) == "user" {
			firstUser = u.Str(m["content"])
			break
		}
	}
	if firstUser == "" {
		return false
	}

	// Signal 1: first user prompt contains an entity ID reference
	// (but still check for question keywords to avoid misclassifying queries)
	questionKW := []string{"为什么", "是什么原因", "查一下", "看一下", "怎么办", "怎么回", "如何", "是不是"}
	if entityIDPattern.MatchString(firstUser) {
		for _, kw := range questionKW {
			if strings.Contains(firstUser, kw) {
				return false // looks like a question about a known entity
			}
		}
		return true
	}

	// Signal 2: contains directive keyword AND no question keyword
	directiveKW := []string{"修改", "改成", "删除", "添加", "commit", "实现", "修复", "提交"}

	hasDirective := false
	for _, kw := range directiveKW {
		if strings.Contains(firstUser, kw) {
			hasDirective = true
			break
		}
	}
	if !hasDirective {
		return false
	}
	for _, kw := range questionKW {
		if strings.Contains(firstUser, kw) {
			return false // looks like a question despite having directive keywords
		}
	}
	return true
}

// ReviewJSON marshals the review result for storage.
func (r ReviewResult) ReviewJSON() string {
	// Fix: ReviewResult has duplicate QualityScore field issue - I added QualityScore in struct incorrectly
	type payload struct {
		ReviewResult
		QualityScore int `json:"quality_score"`
	}
	p := payload{ReviewResult: r, QualityScore: r.QualityScoreValue()}
	return u.JsonStr(p)
}

// EntityRefsJSON returns entity refs from layer0 entity edges.
func (r ReviewResult) EntityRefsJSON() string {
	var refs []string
	for _, e := range r.Layer0Edges {
		if e.Type == "entity_ref" {
			refs = append(refs, e.To)
		}
	}
	if refs == nil {
		refs = []string{}
	}
	b, _ := json.Marshal(refs)
	return string(b)
}
