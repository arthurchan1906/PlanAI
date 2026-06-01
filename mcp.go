package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ============================================================
// MCP Server — JSON-RPC over stdio (Phase 2)
// ============================================================

// MCPTool defines a tool that the MCP server exposes to the Agent.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema MCPInputSchema  `json:"inputSchema"`
}

// MCPInputSchema defines the JSON Schema for tool parameters.
type MCPInputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// JSONRPCMessage is a generic JSON-RPC 2.0 message.
type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpToolResult is the structured result returned by MCP tools.
type mcpToolResult struct {
	Content        []mcpContent `json:"content"`
	RelatedContext interface{}  `json:"related_context,omitempty"`
	Reflection     string       `json:"reflection,omitempty"`
	IsError        bool         `json:"isError,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCP tool handler function signature.
type mcpToolHandler func(args map[string]interface{}) mcpToolResult

// mcpServer holds registered tools and handles the protocol lifecycle.
type mcpServer struct {
	tools   map[string]MCPTool
	handlers map[string]mcpToolHandler
	reader  *bufio.Reader
	writer  io.Writer
}

func newMCPServer() *mcpServer {
	s := &mcpServer{
		tools:   make(map[string]MCPTool),
		handlers: make(map[string]mcpToolHandler),
		reader:  bufio.NewReader(os.Stdin),
		writer:  os.Stdout,
	}
	s.registerTools()
	return s
}

// registerTools registers all MCP tools with descriptions and schemas.
func (s *mcpServer) registerTools() {
	s.addTool(MCPTool{
		Name:        "aipm_get_briefing",
		Description: "获取当前项目简报。包含进行中的任务、PM 最新变更、进度风险、重复检测、scope 漂移等分析结果。Agent 在开始编码前应调用此工具获取最新上下文。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleBriefing)

	s.addTool(MCPTool{
		Name:        "aipm_search_context",
		Description: "在项目知识库中搜索。返回匹配的 tasks/plans/decisions/bugs/ideas 及它们的关联上下文（父子关系、相关 commit、PM 决策等）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]string{
					"type":        "string",
					"description": "搜索关键词",
				},
			},
			Required: []string{"query"},
		},
	}, s.handleSearch)

	s.addTool(MCPTool{
		Name:        "aipm_record_commit",
		Description: "记录一个代码 commit。自动检测 commit 文件是否在 task 的 plan scope 内，返回关联性分析和反思提示。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id":  map[string]string{"type": "string", "description": "关联的 Task ID"},
				"title":    map[string]string{"type": "string", "description": "Commit 标题"},
				"summary":  map[string]string{"type": "string", "description": "变更摘要"},
				"files":    map[string]string{"type": "string", "description": "变更文件列表，逗号分隔"},
				"branch":   map[string]string{"type": "string", "description": "分支名"},
				"status":   map[string]string{"type": "string", "description": "commit/draft"},
			},
			Required: []string{"task_id", "title"},
		},
	}, s.handleRecordCommit)

	s.addTool(MCPTool{
		Name:        "aipm_create_task",
		Description: "创建一个新 Task。自动检测标题重复、回填 roadmap_id、检查 plan 状态。返回创建结果和重复/冲突提示。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":    map[string]string{"type": "string", "description": "Task 标题"},
				"plan_id":  map[string]string{"type": "string", "description": "所属 Plan ID"},
				"priority": map[string]string{"type": "string", "description": "P0/P1/P2"},
				"status":   map[string]string{"type": "string", "description": "todo/in_progress"},
				"phase":    map[string]string{"type": "string", "description": "所属 phase"},
			},
			Required: []string{"title", "plan_id"},
		},
	}, s.handleCreateTask)

	s.addTool(MCPTool{
		Name:        "aipm_analyze",
		Description: "运行项目分析，检测 scope 漂移、孤儿任务、重复 plan、进度风险、阻塞超时、决策影响。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleAnalyze)

	s.addTool(MCPTool{
		Name:        "aipm_record_bug",
		Description: "记录一个 Bug。包含错误信息、根因分析、修复方案、标签等完整元数据，便于后续搜索和关联。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":       map[string]string{"type": "string", "description": "Bug 标题"},
				"error":       map[string]string{"type": "string", "description": "完整错误信息"},
				"files":       map[string]string{"type": "string", "description": "相关文件，逗号分隔"},
				"root_cause":  map[string]string{"type": "string", "description": "根因分析"},
				"fix":         map[string]string{"type": "string", "description": "修复方案"},
				"severity":    map[string]string{"type": "string", "description": "critical/major/minor"},
				"tags":        map[string]string{"type": "string", "description": "标签，逗号分隔"},
				"commit_id":   map[string]string{"type": "string", "description": "关联的 commit ID（可选）"},
			},
			Required: []string{"title", "error", "root_cause", "fix"},
		},
	}, s.handleRecordBug)

	s.addTool(MCPTool{
		Name:        "aipm_update_task_status",
		Description: "更新 Task 状态（todo/in_progress/blocked/done）。更新到 done 前会检查是否有已验证的 commit。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id": map[string]string{"type": "string", "description": "Task ID"},
				"status":  map[string]string{"type": "string", "description": "todo/in_progress/blocked/done"},
				"note":    map[string]string{"type": "string", "description": "状态变更说明"},
			},
			Required: []string{"task_id", "status"},
		},
	}, s.handleUpdateTaskStatus)

	s.addTool(MCPTool{
		Name:        "aipm_mark_consumed",
		Description: "标记所有 PM 事件为已消费。Agent 在阅读简报后应调用此工具，以便 PM 追踪 Agent 是否已响应变更。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleMarkConsumed)

	s.addTool(MCPTool{
		Name:        "aipm_append_task_note",
		Description: "向 Task 追加一条备注。Agent 在工作中需要记录思路、发现或设计讨论时使用，支持持续积累而不覆盖已有内容。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id": map[string]string{"type": "string", "description": "Task ID"},
				"content": map[string]string{"type": "string", "description": "备注内容"},
			},
			Required: []string{"task_id", "content"},
		},
	}, s.handleAppendTaskNote)

	s.addTool(MCPTool{
		Name:        "aipm_link_entities",
		Description: "在两个实体之间建立关联关系。例如将 bug 关联到 commit、将 task 关联到 decision。关系类型包括: fixes, relates_to, blocked_by, implements 等。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"source_type": map[string]string{"type": "string", "description": "源实体类型 (task/commit/bug/decision/idea/plan)"},
				"source_id":   map[string]string{"type": "string", "description": "源实体 ID"},
				"relation":    map[string]string{"type": "string", "description": "关系 (fixes/relates_to/blocked_by/implements/depends_on)"},
				"target_type": map[string]string{"type": "string", "description": "目标实体类型"},
				"target_id":   map[string]string{"type": "string", "description": "目标实体 ID"},
				"note":        map[string]string{"type": "string", "description": "关联说明（可选）"},
			},
			Required: []string{"source_type", "source_id", "relation", "target_type", "target_id"},
		},
	}, s.handleLinkEntities)

	s.addTool(MCPTool{
		Name:        "aipm_record_decision",
		Description: "记录一个架构或技术决策。包含背景、决策内容和状态，可被后续任务和 commit 引用。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":      map[string]string{"type": "string", "description": "决策标题"},
				"background": map[string]string{"type": "string", "description": "背景和上下文"},
				"decision":   map[string]string{"type": "string", "description": "决策内容"},
				"status":     map[string]string{"type": "string", "description": "proposed/accepted/deprecated"},
			},
			Required: []string{"title", "background", "decision"},
		},
	}, s.handleRecordDecision)

	s.addTool(MCPTool{
		Name:        "aipm_submit_feedback",
		Description: "提交工具使用反馈到远程反馈服务器。当用户发现 AIPM 工具的 bug 或改进建议时调用。label: bug/suggestion。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"label":   map[string]string{"type": "string", "description": "bug 或 suggestion"},
				"content": map[string]string{"type": "string", "description": "反馈内容"},
			},
			Required: []string{"label", "content"},
		},
	}, s.handleSubmitFeedback)
}

func (s *mcpServer) addTool(tool MCPTool, handler mcpToolHandler) {
	s.tools[tool.Name] = tool
	s.handlers[tool.Name] = handler
}

// ---- Tool Handlers ----

func (s *mcpServer) handleBriefing(args map[string]interface{}) mcpToolResult {
	briefing := BuildBriefing()
	report := runFullAnalysis()

	related := map[string]interface{}{
		"analysis_summary": report.Summary,
		"active_plans_count": len(report.Progress),
		"risks":              report.Progress,
	}

	reflection := ""
	if len(report.Orphans) > 0 {
		reflection = fmt.Sprintf("⚠️ 检测到 %d 个孤儿任务（in_progress 但无 commit）。检查这些任务是否需要 commit 或更新状态。", len(report.Orphans))
	}
	if len(report.Duplicates) > 0 {
		reflection += fmt.Sprintf("⚠️ 检测到 %d 个重复 Plan。避免重复创建。", len(report.Duplicates))
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: briefing},
		},
		RelatedContext: related,
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleSearch(args map[string]interface{}) mcpToolResult {
	query := getStr(args, "query", "")
	if query == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "请提供搜索关键词 (query)"}},
			IsError: true,
		}
	}
	result := searchProjectContext(query, 10)

	// Build enriched context
	relatedIDs := []string{}
	if results, ok := result["results"].([]searchHit); ok {
		for _, h := range results {
			relatedIDs = append(relatedIDs, h.ID)
		}
	}

	context := map[string]interface{}{
		"total_results": result["count"],
		"related_ids":   relatedIDs,
	}

	// Check for duplicates in results
	reflection := ""
	if cnt, ok := result["count"].(int); ok && cnt == 0 {
		reflection = "未找到匹配结果。可以创建新的 task/plan，但请先确认是否属于已有 plan 的范围。"
	} else if cnt > 5 {
		reflection = fmt.Sprintf("找到 %d 个相关结果。建议缩小搜索范围或使用 aipm_get_briefing 了解当前项目概况。", cnt)
	}

	text := fmt.Sprintf("搜索 '%s' 找到 %v 个结果", query, result["count"])
	for _, h := range result["results"].([]searchHit) {
		text += fmt.Sprintf("\n- [%s] %s (%s)", h.Type, h.Title, h.ID)
	}

	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: text}},
		RelatedContext: context,
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleRecordCommit(args map[string]interface{}) mcpToolResult {
	taskID := getStr(args, "task_id", "")
	title := getStr(args, "title", "")
	summary := getStr(args, "summary", "")
	branch := getStr(args, "branch", "main")
	status := getStr(args, "status", "committed")
	filesStr := getStr(args, "files", "")

	var files []string
	if filesStr != "" {
		for _, f := range splitAndTrim(filesStr, ",") {
			if f != "" {
				files = append(files, f)
			}
		}
	}

	commit, err := createCommit(title, summary, "", "", branch, "", taskID, "", status, "not_run", "pending", files)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 commit 失败: %v", err)}},
			IsError: true,
		}
	}

	// Analyze scope drift for this commit
	report := runFullAnalysis()
	var driftWarnings []string
	for _, d := range report.Drifts {
		if d.CommitID == commit["id"] {
			driftWarnings = append(driftWarnings, fmt.Sprintf("⚠️ 文件 %v 可能超出 plan scope", d.OutOfScope))
		}
	}

	// Get related commits for the same task
	allCommits, _ := listCommitsByTask(taskID)
	related := map[string]interface{}{
		"commit":        commit,
		"task_commits":  len(allCommits),
		"drift_warnings": driftWarnings,
	}

	reflection := fmt.Sprintf("Commit '%s' 已记录。", title)
	if len(driftWarnings) > 0 {
		reflection += " " + driftWarnings[0]
		reflection += " 建议: 确认这些文件是否应属于当前 task，如是请更新 plan scope。"
	}
	if len(allCommits) == 1 {
		reflection += " 这是此 task 的第一个 commit。"
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ Commit 已创建: %s [%s]", commit["id"], title)},
		},
		RelatedContext: related,
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleCreateTask(args map[string]interface{}) mcpToolResult {
	title := getStr(args, "title", "")
	planID := getStr(args, "plan_id", "")
	priority := getStr(args, "priority", "P1")
	status := getStr(args, "status", "todo")
	phase := getStr(args, "phase", "general")

	// Duplicate check before creating
	report := runFullAnalysis()
	hasDuplicate := false
	for _, d := range report.Duplicates {
		if d.Title1 == title || d.Title2 == title {
			hasDuplicate = true
			break
		}
	}

	task, err := createTask(title, priority, status, phase, planID, nil)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 task 失败: %v", err)}},
			IsError: true,
		}
	}

	// Get the plan to check status
	plan, _ := getPlan(planID)
	related := map[string]interface{}{
		"task":           task,
		"plan_title":     str(plan["title"]),
		"plan_status":    str(plan["status"]),
	}

	reflection := fmt.Sprintf("Task '%s' 已创建 (plan: %s)。", title, str(plan["title"]))
	if hasDuplicate {
		reflection += " ⚠️ 可能已存在类似 task，请用 aipm_search_context 确认。"
	}
	if str(plan["status"]) == "draft" {
		reflection += " ℹ️ 注意：此 plan 仍为 draft 状态。"
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ Task 已创建: %s [%s]", task["id"], title)},
		},
		RelatedContext: related,
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleRecordBug(args map[string]interface{}) mcpToolResult {
	title := getStr(args, "title", "")
	errMsg := getStr(args, "error", "")
	rootCause := getStr(args, "root_cause", "")
	fix := getStr(args, "fix", "")
	files := getStr(args, "files", "")
	severity := getStr(args, "severity", "minor")
	tags := getStr(args, "tags", "")
	commitID := getStr(args, "commit_id", "")

	if title == "" || errMsg == "" || rootCause == "" || fix == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "title, error, root_cause, fix 为必填字段"}},
			IsError: true,
		}
	}

	bug, err := createBug(title, errMsg, severity, "open", commitID, errMsg, files, rootCause, fix, tags)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 bug 失败: %v", err)}},
			IsError: true,
		}
	}

	reflection := fmt.Sprintf("Bug '%s' 已记录 (severity: %s)。", title, severity)
	if commitID != "" {
		reflection += fmt.Sprintf(" 已关联 commit %s。", commitID)
	}
	reflection += " 请确保 fix 方案已经过验证。"

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ Bug 已记录: %s [%s]", bug["id"], title)},
		},
		Reflection: reflection,
	}
}

func (s *mcpServer) handleAppendTaskNote(args map[string]interface{}) mcpToolResult {
	taskID := getStr(args, "task_id", "")
	content := getStr(args, "content", "")
	if taskID == "" || content == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "task_id 和 content 为必填字段"}},
			IsError: true,
		}
	}
	result, err := appendTaskNote(taskID, content)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("追加备注失败: %v", err)}},
			IsError: true,
		}
	}
	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ 备注已追加到 task %s", taskID)},
		},
		RelatedContext: result,
	}
}

func (s *mcpServer) handleLinkEntities(args map[string]interface{}) mcpToolResult {
	sourceType := getStr(args, "source_type", "")
	sourceID := getStr(args, "source_id", "")
	relation := getStr(args, "relation", "")
	targetType := getStr(args, "target_type", "")
	targetID := getStr(args, "target_id", "")
	note := getStr(args, "note", "")

	if sourceType == "" || sourceID == "" || relation == "" || targetType == "" || targetID == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "所有字段均为必填: source_type, source_id, relation, target_type, target_id"}},
			IsError: true,
		}
	}

	link, err := createLink(sourceType, sourceID, relation, targetType, targetID, note)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建关联失败: %v", err)}},
			IsError: true,
		}
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ 已关联: %s/%s --[%s]--> %s/%s", sourceType, sourceID, relation, targetType, targetID)},
		},
		RelatedContext: link,
	}
}

func (s *mcpServer) handleRecordDecision(args map[string]interface{}) mcpToolResult {
	title := getStr(args, "title", "")
	background := getStr(args, "background", "")
	decision := getStr(args, "decision", "")
	status := getStr(args, "status", "proposed")

	if title == "" || background == "" || decision == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "title, background, decision 为必填字段"}},
			IsError: true,
		}
	}

	dec, err := createDecision(title, background, decision, status)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建决策失败: %v", err)}},
			IsError: true,
		}
	}

	reflection := fmt.Sprintf("决策 '%s' 已记录 (status: %s)。", title, status)
	reflection += " 如有受影响的 task，请用 aipm_link_entities 关联。"

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ 决策已记录: %s [%s]", dec["id"], title)},
		},
		Reflection: reflection,
	}
}

func (s *mcpServer) handleUpdateTaskStatus(args map[string]interface{}) mcpToolResult {
	taskID := getStr(args, "task_id", "")
	status := getStr(args, "status", "")
	note := getStr(args, "note", "")

	if taskID == "" || status == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "task_id 和 status 为必填字段"}},
			IsError: true,
		}
	}

	_, err := updateTask(taskID, status, note, false, false)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新失败: %v", err)}},
			IsError: true,
		}
	}

	reflection := fmt.Sprintf("Task 状态已更新为 '%s'。", status)
	if status == "done" {
		reflection += " 请确认已记录所有相关 commit。"
	}
	if status == "blocked" {
		reflection += " 请在 note 中说明阻塞原因和需要的决策。"
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ Task %s → %s", taskID, status)},
		},
		Reflection: reflection,
	}
}

func (s *mcpServer) handleSubmitFeedback(args map[string]interface{}) mcpToolResult {
	label := getStr(args, "label", "suggestion")
	content := getStr(args, "content", "")

	if content == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "content 为必填字段"}},
			IsError: true,
		}
	}

	fb, err := addFeedback(label, content)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("反馈服务器不可达，但已本地记录: %v", err)}},
			IsError: true,
		}
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ 反馈已提交到服务器 [%s]", label)},
		},
		RelatedContext: fb,
	}
}

func (s *mcpServer) handleMarkConsumed(args map[string]interface{}) mcpToolResult {
	if err := markEventsConsumed(); err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("标记失败: %v", err)}},
			IsError: true,
		}
	}
	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: "✅ 所有 PM 事件已标记为已消费。PM 可确认 Agent 已响应变更。"}},
	}
}

func (s *mcpServer) handleAnalyze(args map[string]interface{}) mcpToolResult {
	report := runFullAnalysis()
	text := fmt.Sprintf("分析完成: %s", report.Summary)

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: text},
		},
		RelatedContext: report,
		Reflection:     report.Summary,
	}
}

// ---- MCP Protocol Lifecycle ----

// Run starts the MCP server, reading JSON-RPC requests from stdin and writing responses to stdout.
func (s *mcpServer) Run() error {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var msg jsonrpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.sendError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		if msg.JSONRPC != "2.0" {
			continue
		}

		switch msg.Method {
		case "initialize":
			s.handleInitialize(&msg)
		case "tools/list":
			s.handleToolsList(&msg)
		case "tools/call":
			s.handleToolsCall(&msg)
		case "notifications/initialized":
			// Client notification — no response needed
		default:
			s.sendError(msg.ID, -32601, fmt.Sprintf("Method not found: %s", msg.Method))
		}
	}
}

func (s *mcpServer) handleInitialize(msg *jsonrpcMessage) {
	result := map[string]interface{}{
		"protocolVersion": "0.2",
		"capabilities": map[string]interface{}{
			"tools": map[string]bool{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "aipm-mcp",
			"version": "0.2.0",
		},
	}
	s.sendResult(msg.ID, result)
}

func (s *mcpServer) handleToolsList(msg *jsonrpcMessage) {
	tools := make([]MCPTool, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, t)
	}
	s.sendResult(msg.ID, map[string]interface{}{
		"tools": tools,
	})
}

func (s *mcpServer) handleToolsCall(msg *jsonrpcMessage) {
	var call struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &call); err != nil {
		s.sendError(msg.ID, -32602, "Invalid params: "+err.Error())
		return
	}

	handler, ok := s.handlers[call.Name]
	if !ok {
		s.sendError(msg.ID, -32602, fmt.Sprintf("Tool not found: %s", call.Name))
		return
	}

	result := handler(call.Arguments)
	s.sendResult(msg.ID, result)
}

// ---- Transport helpers ----

func (s *mcpServer) sendResult(id interface{}, result interface{}) {
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.writeMessage(msg)
}

func (s *mcpServer) sendError(id interface{}, code int, message string) {
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
		},
	}
	s.writeMessage(msg)
}

func (s *mcpServer) writeMessage(msg jsonrpcMessage) {
	data, _ := json.Marshal(msg)
	fmt.Fprintf(s.writer, "%s\n", string(data))
}

// ---- Helpers ----

func getStr(m map[string]interface{}, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func splitAndTrim(s, sep string) []string {
	var result []string
	for _, part := range splitStr(s, sep) {
		part = trimStr(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitStr(s, sep string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	for {
		i := strIndex(s, sep)
		if i < 0 {
			parts = append(parts, s)
			break
		}
		parts = append(parts, s[:i])
		s = s[i+len(sep):]
	}
	return parts
}

func trimStr(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func strIndex(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
