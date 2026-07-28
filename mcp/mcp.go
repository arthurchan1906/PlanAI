package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"aipmc/ai"
	"aipmc/analyze"
	"aipmc/discussion"
	"aipmc/store"
	"aipmc/vision"
	"aipmc/u"
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

// mcpClientInfo holds the client identity reported during MCP initialize.
type mcpClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// mcpServer holds registered tools and handles the protocol lifecycle.
type mcpServer struct {
	tools         map[string]MCPTool
	handlers      map[string]mcpToolHandler
	reader        *bufio.Reader
	writer        io.Writer
	ai            *ai.Client
	clientInfo    *mcpClientInfo // captured from initialize — the client's "user agent"
	// Search functions injected from root
	searchContext     func(string, int) map[string]any
	searchFTS5        func(string, int) interface{}
	searchLinear      func(string) interface{}
	aiRerank          func(string, int, interface{}) interface{}
	searchDiscussions func(string, string, string, string, int, int) ([]map[string]any, int, error)
}

func NewServer(aiClient *ai.Client,
	searchContext func(string, int) map[string]any,
	searchFTS5 func(string, int) interface{},
	searchLinear func(string) interface{},
	aiRerank func(string, int, interface{}) interface{},
	searchDiscussions func(string, string, string, string, int, int) ([]map[string]any, int, error),
) *mcpServer {
	s := &mcpServer{
		tools:             make(map[string]MCPTool),
		handlers:          make(map[string]mcpToolHandler),
		reader:            bufio.NewReader(os.Stdin),
		writer:            os.Stdout,
		ai:                aiClient,
		searchContext:     searchContext,
		searchFTS5:        searchFTS5,
		searchLinear:      searchLinear,
		aiRerank:          aiRerank,
		searchDiscussions: searchDiscussions,
	}
	s.registerTools()
	return s
}

// registerTools registers all MCP tools with descriptions and schemas.
func (s *mcpServer) registerTools() {
	// Core tools
	s.addTool(MCPTool{
		Name:        "aipm_get_briefing",
		Description: "获取当前项目简报。包含进行中的任务、PM 最新变更、进度风险、重复检测、scope 漂移、最近 Agent 活动等分析结果。Agent 在开始编码前应调用此工具获取最新上下文。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleBriefing)

	s.addTool(MCPTool{
		Name:        "aipm_search_context",
		Description: "搜索 PM 实体（task/plan/decision/bug/idea）及其关联上下文（父子关系、相关 commit、PM 决策）。搜「有没有类似的 task/plan」用这个。与 aipm_smart_search 的区别：search_context 基于关键词精确匹配 FTS5 索引，smart_search 通过 AI 语义理解做模糊关联（需 AI 端点可用）。",
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
		Description: "记录一个代码 commit。自动检测 commit 文件是否在 task 的 plan scope 内，返回关联性分析和反思提示。通过 review_status=approved + test_status=passed 可以让关联的 task 标记为 done。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id":       map[string]string{"type": "string", "description": "关联的 Task ID"},
				"title":         map[string]string{"type": "string", "description": "Commit 标题"},
				"summary":       map[string]string{"type": "string", "description": "变更摘要"},
				"files":         map[string]string{"type": "string", "description": "变更文件列表，逗号分隔"},
				"branch":        map[string]string{"type": "string", "description": "分支名"},
				"status":        map[string]string{"type": "string", "description": "commit/draft"},
				"project_path":  map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
				"review_status": map[string]string{"type": "string", "description": "可选: pending/approved/rejected，默认 pending。设为 approved 后 task 可标记 done"},
				"test_status":   map[string]string{"type": "string", "description": "可选: not_run/passed/failed，默认 not_run。设为 passed 后 task 可标记 done"},
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
				"phase":        map[string]string{"type": "string", "description": "所属 phase"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
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
				"task_id":       map[string]string{"type": "string", "description": "Task ID"},
				"status":        map[string]string{"type": "string", "description": "todo/in_progress/blocked/done"},
				"note":          map[string]string{"type": "string", "description": "状态变更说明"},
				"project_path":  map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
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
				"note":         map[string]string{"type": "string", "description": "关联说明（可选）"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
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

	// Thread (线索) tools
	s.addTool(MCPTool{
		Name:        "aipm_suggest_threads",
		Description: "启发式分析最近的 commit 历史并建议线索（thread）。注意：此工具基于算法自动聚类，Agent 应优先使用 aipm_daily_review 获取原始数据并进行自主语义分析。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleSuggestThreads)

	s.addTool(MCPTool{
		Name:        "aipm_create_thread",
		Description: "创建一条新线索（thread）。线索是一组相关 task/commit/decision/idea 的聚合视图，用于追踪跨 plan 的、非线性推进的工作流。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":   map[string]string{"type": "string", "description": "线索标题"},
				"summary": map[string]string{"type": "string", "description": "线索描述"},
			},
			Required: []string{"title"},
		},
	}, s.handleCreateThread)

	s.addTool(MCPTool{
		Name:        "aipm_add_to_thread",
		Description: "将一个实体（task/commit/decision/idea）添加到已有线索中。用于将分散在不同 plan 下的相关工作归入同一条线索。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"thread_id":   map[string]string{"type": "string", "description": "线索 ID"},
				"entity_type": map[string]string{"type": "string", "description": "实体类型 (task/commit/decision/idea/plan/bug)"},
				"entity_id":   map[string]string{"type": "string", "description": "实体 ID"},
				"note":        map[string]string{"type": "string", "description": "添加说明（可选）"},
			},
			Required: []string{"thread_id", "entity_type", "entity_id"},
		},
	}, s.handleAddToThread)

	// Daily review — gives the agent raw commit data to do its own semantic analysis
	s.addTool(MCPTool{
		Name:        "aipm_daily_review",
		Description: "获取今日或最近的 commits 及完整上下文（task、plan、文件、已有线索），供 Agent 进行语义分析和线索整理。Agent 应在每日结束时调用此工具，独立分析 commits 之间的逻辑关联，然后使用 aipm_create_thread 和 aipm_add_to_thread 创建/更新线索。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleDailyReview)

	// Smart search — AI-enhanced when available
	s.addTool(MCPTool{
		Name:        "aipm_smart_search",
		Description: "AI 语义搜索 PM 实体（task/plan/commit/bug/decision/idea）。基于 FTS5 关键词匹配 + AI 语义重排序（AI 不可用时降级为普通 FTS5 搜索）。搜自然语言描述（如「处理密友同步的那个 task」「上周修的安全 bug」）用这个。与 aipm_search_context 的区别：smart_search 适合语义模糊的自然语言查询，search_context 适合精确关键词匹配。如需搜索讨论/对话内容，用 aipm_search_discussions。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词"},
				"limit": map[string]string{"type": "integer", "description": "返回结果数量上限，默认 8"},
			},
			Required: []string{"query"},
		},
	}, s.handleSmartSearch)

	// Discussion log tools
	s.addTool(MCPTool{
		Name:        "aipm_read_discussions",
		Description: "读取其他 Agent（Claude Code/Cursor/Gemini/OpenCode 等）的对话历史。想看某个 Agent 说了什么 → source 指定来源；不传 source 则返回所有人。full=true 返回全文。未指定 last_n 时默认 15 条。传 cursor 可增量读取避免重复（上次读到 disc-xxx, 从那里继续）。禁止 sqlite3 直查数据库。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"source": map[string]string{"type": "string", "description": "想看哪个 Agent: claude-code / cursor / gemini-cli / opencode / codex-cli。例：看 Cursor 说了什么 → source=\"cursor\"。不传则看所有人。"},
				"last_n": map[string]string{"type": "integer", "description": "最近 N 条。默认 15。快速浏览用 5，深入阅读用 30（与 since / cursor 可组合）"},
				"since":  map[string]string{"type": "string", "description": "可选: ISO 时间下限 (例 2026-06-15T21:48:00)"},
				"cursor": map[string]string{"type": "string", "description": "可选: 从上次返回的 cursor 之后继续读取，避免重复（传上次返回结果中 related_context.cursor 的值）"},
				"full":         map[string]string{"type": "boolean", "description": "true=全文（互读讨论必设），false=预览约 200 字（默认）"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径，不传则读当前项目。例: /Users/dazsec/projects/EncryptDrive"},
			},
		},
	}, s.handleReadDiscussions)

	s.addTool(MCPTool{
		Name:        "aipm_search_discussions",
		Description: "按关键词搜索讨论内容（搜「谁说了关于 X 的话」）。与 aipm_read_discussions 的区别：search 按内容关键词搜，read 按 Agent 直接读。mode=full_session 展开整段 session 全文；读某 Agent 全部发言优先 read_discussions(source=..., full=true)。last_n 模式支持 cursor 增量读取。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query":        map[string]string{"type": "string", "description": "搜索关键词（与 last_n 二选一）"},
				"source":       map[string]string{"type": "string", "description": "可选: 按 agent 来源过滤 (claude-code / gemini-cli / codex-cli / codex / opencode / cursor)"},
				"type":         map[string]string{"type": "string", "description": "可选: 按消息类型过滤 (user / assistant / tool)"},
				"last_n":       map[string]string{"type": "integer", "description": "可选: 返回最近 N 条记录（与 query 二选一，优先使用 last_n）"},
				"cursor":       map[string]string{"type": "string", "description": "可选: 从上次返回的 cursor 之后继续读取（仅 last_n 模式生效）"},
				"mode":         map[string]string{"type": "string", "description": "可选: 'matches' (默认，匹配消息预览约200字)；'full_session' (展开 session 全部消息且全文不截断)"},
				"limit":        map[string]string{"type": "integer", "description": "结果数量，默认 10。full_session 模式下为 session 数量上限（≤5）"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径，不传则搜索当前项目"},
			},
		},
	}, s.handleSearchDiscussions)

	s.addTool(MCPTool{
		Name:        "aipmc_vision",
		Description: "分析 UI 截图并描述实际效果，实现「改代码→截图→看图验证→再改」的自主闭环。\\n\\n使用流程：\\n1. 修改前端/客户端代码后，用 bash 截图（screencapture / adb / xcrun 等）\\n2. 调用此工具，prompt 中附带关键代码片段 + 期望效果 + 重点关注\\n3. 视觉模型返回纯描述性分析（只描述实际看到的，不提供代码修改建议）\\n4. 你自己对比代码预期和实际效果，判断是否继续修改\\n\\nPrompt 公式（经验证效果显著）：\\n- [代码] 贴关键代码片段（5-20行）\\n- [期望] 你预期这段代码会渲染成什么效果\\n- [问题] 具体问视觉模型观察什么（颜色/位置/对齐/分界等）\\n\\n迭代控制：每轮传 --iteration N，看到轮次数字自然意识到该收手了。截图可先用 sips -Z 1024 压缩加速。\\n\\n视觉模型只是「眼睛」，你（主模型）才是「大脑」——代码决策全部由你负责。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"image_path": map[string]string{
					"type":        "string",
					"description": "本地图片文件的绝对路径（建议先用 sips -Z 1024 压缩以加速）",
				},
				"prompt": map[string]string{
					"type":        "string",
					"description": "按公式组织：[代码]关键代码片段 + [期望]预期效果 + [问题]具体观察点。视觉模型只描述实际看到的。",
				},
				"iteration": map[string]interface{}{
					"type":        "integer",
					"description": "当前是第几轮视觉检查。视觉模型会被告知轮次，帮助主模型判断何时收手（可选，默认 1，建议不超过5轮）",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "指定视觉模型 ID（可选，不指定时自动选择优先级最高的 vision 模型）",
				},
			},
			Required: []string{"image_path", "prompt"},
		},
	}, s.handleVision)
	registerTraceContext(s)
}

func (s *mcpServer) addTool(tool MCPTool, handler mcpToolHandler) {
	s.tools[tool.Name] = tool
	s.handlers[tool.Name] = handler
}

// ---- Tool Handlers ----

func (s *mcpServer) handleBriefing(args map[string]interface{}) mcpToolResult {
	briefing := analyze.BuildBriefing(s.ai, buildBriefingGraph())
	report := analyze.RunFullAnalysis()

	related := map[string]interface{}{
		"analysis_summary": report.Summary,
		"active_plans_count": len(report.Progress),
		"risks":              report.Progress,
	}

	reflection := ""
	if len(report.Orphans) > 0 {
		reflection = fmt.Sprintf("⚠️ 检测到 %d 个孤儿任务（3 天内无 commit 且无讨论）。检查这些任务是否需要 commit 或更新状态。", len(report.Orphans))
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
	result := s.searchContext(query, 10)

	// Build enriched context
	relatedIDs := []string{}
	if results, ok := result["results"].([]map[string]interface{}); ok {
		for _, h := range results {
			relatedIDs = append(relatedIDs, u.Str(h["id"]))
		}
	}

	context := map[string]interface{}{
		"total_results": result["count"],
		"related_ids":   relatedIDs,
	}

	// Check for duplicates in results
	reflection := ""
	if cnt, ok := result["count"].(int); ok && cnt == 0 {
		reflection = "[工具提示] search_context 无结果。如需搜索讨论内容使用 aipm_search_discussions，了解项目概况使用 aipm_get_briefing。"
	} else if cnt > 5 {
		reflection = fmt.Sprintf("[工具提示] 找到 %d 个相关结果，可缩小搜索范围。", cnt)
	}

	text := fmt.Sprintf("搜索 '%s' 找到 %v 个结果", query, result["count"])
	if results, ok := result["results"].([]map[string]interface{}); ok {
		for _, h := range results {
			entityType := u.Str(h["type"])
			entityID := u.Str(h["id"])
			score := ""
			if s, ok := h["score"].(float64); ok && s > 0 {
				score = fmt.Sprintf(" (相关度: %.0f%%)", s)
			} else if s, ok := h["score"].(int); ok && s > 0 {
				score = fmt.Sprintf(" (相关度: %d%%)", s)
			}
			text += fmt.Sprintf("\n- [%s] %s (%s)%s", entityType, u.Str(h["title"]), entityID, score)
			if entityID != "" {
				if sessions, err := store.LinkedDiscussionSessions(entityType, entityID, 3); err == nil && len(sessions) > 0 {
					text += fmt.Sprintf(" — 💬 %d 个讨论 session 涉及", len(sessions))
				}
			}
		}
	}

	// Enhance with L2 session summaries when available
	if summaryRows, err := store.SearchSessionSummaries(query, 3); err == nil && len(summaryRows) > 0 {
		text += "\n\n### 相关 Session 知识\n"
		for _, sr := range summaryRows {
			var l2 struct {
				Goal  string   `json:"goal"`
				Files []string `json:"files"`
			}
			if json.Unmarshal([]byte(sr.Summary), &l2) == nil && l2.Goal != "" {
				prefix := sr.SessionID
				if len(prefix) > 8 {
					prefix = prefix[:8]
				}
				text += fmt.Sprintf("- [%s] %s\n", prefix, l2.Goal)
				if len(l2.Files) > 0 {
					text += fmt.Sprintf("  文件: %s\n", strings.Join(l2.Files, ", "))
				}
			}
		}
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
	projectPath := getStr(args, "project_path", "")
	testStatus := getStr(args, "test_status", "not_run")
	reviewStatus := getStr(args, "review_status", "pending")

	var files []string
	if filesStr != "" {
		for _, f := range u.SplitAndTrim(filesStr, ",") {
			if f != "" {
				files = append(files, f)
			}
		}
	}

	commit, err := store.CreateCommit(projectPath, title, summary, "", "", branch, "", taskID, "", status, testStatus, reviewStatus, files)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 commit 失败: %v", err)}},
			IsError: true,
		}
	}

	// Analyze scope drift for this commit
	report := analyze.RunFullAnalysis()
	var driftWarnings []string
	for _, d := range report.Drifts {
		if d.CommitID == commit["id"] {
			driftWarnings = append(driftWarnings, fmt.Sprintf("⚠️ 文件 %v 可能超出 plan scope", d.OutOfScope))
		}
	}

	// Get related commits for the same task
	allCommits, _ := store.ListCommitsByTask(taskID)
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
	projectPath := getStr(args, "project_path", "")

	// Duplicate check before creating
	report := analyze.RunFullAnalysis()
	hasDuplicate := false
	for _, d := range report.Duplicates {
		if d.Title1 == title || d.Title2 == title {
			hasDuplicate = true
			break
		}
	}

	// Validate plan_id exists before creating
	if planID == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "❌ plan_id 为必填项。Task 必须关联到一个 Plan。\n\n请用 aipm_search_context 搜索已有 plan，或先创建 plan。"}},
			IsError: true,
		}
	}
	plan, err := store.GetPlan(planID)
	if err != nil || plan == nil || plan["id"] == nil {
		// Check if it's a task ID (common agent mistake: passing task ID as plan_id)
		task, _ := store.GetTaskSimple(planID)
		if task != nil && task["id"] != nil {
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
					"❌ '%s' 是一个 task ID，不是 plan ID。Task 必须属于 Plan。\n\n"+
						"正确做法：\n"+
						"1. 用 aipm_search_context 搜索已有的 plan\n"+
						"2. 或先创建一个 plan 再创建 task", planID)}},
				IsError: true,
			}
		}
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
				"❌ plan '%s' 不存在。\n\n"+
					"Task 必须关联到一个已存在的 plan。请用 aipm_search_context 搜索已有 plan，"+
					"或先创建 plan 再创建 task。", planID)}},
			IsError: true,
		}
	}

	task, err := store.CreateTask(projectPath, title, priority, status, phase, planID, nil)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 task 失败: %v", err)}},
			IsError: true,
		}
	}

	related := map[string]interface{}{
		"task":           task,
		"plan_title":     u.Str(plan["title"]),
		"plan_status":    u.Str(plan["status"]),
	}

	reflection := fmt.Sprintf("Task '%s' 已创建 (plan: %s)。", title, u.Str(plan["title"]))
	if hasDuplicate {
		reflection += " ⚠️ 可能已存在类似 task，请用 aipm_search_context 确认。"
	}
	if u.Str(plan["status"]) == "draft" {
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
	projectPath := getStr(args, "project_path", "")
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

	bug, err := store.CreateBug(projectPath, title, errMsg, severity, "open", commitID, errMsg, files, rootCause, fix, tags)
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
	projectPath := getStr(args, "project_path", "")
	content := getStr(args, "content", "")
	if taskID == "" || content == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "task_id 和 content 为必填字段"}},
			IsError: true,
		}
	}
	result, err := store.AppendTaskNote(projectPath, taskID, content)
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
	projectPath := getStr(args, "project_path", "")
	relation := getStr(args, "relation", "")
	targetType := getStr(args, "target_type", "")
	targetID := getStr(args, "target_id", "")
	note := getStr(args, "note", "")

	// Whitelist: validate relation before touching the store
	allowedRels := map[string]bool{"relates_to": true, "implements": true, "fixes": true, "blocked_by": true, "depends_on": true, "converted_to": true}
	if relation != "" && !allowedRels[relation] {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("relation '%s' is not allowed. Valid options: relates_to, implements, fixes, blocked_by, depends_on, converted_to", relation)}},
			IsError: true,
		}
	}

	if sourceType == "" || sourceID == "" || relation == "" || targetType == "" || targetID == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "所有字段均为必填: source_type, source_id, relation, target_type, target_id"}},
			IsError: true,
		}
	}

	link, err := store.CreateLink(projectPath, sourceType, sourceID, relation, targetType, targetID, note)
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
	projectPath := getStr(args, "project_path", "")
	decision := getStr(args, "decision", "")
	status := getStr(args, "status", "proposed")

	if title == "" || background == "" || decision == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "title, background, decision 为必填字段"}},
			IsError: true,
		}
	}

	dec, err := store.CreateDecision(projectPath, title, background, decision, status)
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
	projectPath := getStr(args, "project_path", "")

	if taskID == "" || status == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "task_id 和 status 为必填字段"}},
			IsError: true,
		}
	}

	_, err := store.UpdateTask(projectPath, taskID, status, note, false, false)
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

	fb, err := AddFeedback(label, content)
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
	if err := store.MarkEventsConsumed(); err != nil {
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
	report := analyze.RunFullAnalysis()
	text := fmt.Sprintf("分析完成: %s", report.Summary)

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: text},
		},
		RelatedContext: report,
		Reflection:     report.Summary,
	}
}

func (s *mcpServer) handleSmartSearch(args map[string]interface{}) mcpToolResult {
	query := getStr(args, "query", "")
	if query == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "请提供搜索关键词 (query)"}},
			IsError: true,
		}
	}
	limit := 8

	// FTS5 keyword search — returns interface{}, type-assert to usable form
	resultsRaw := s.searchFTS5(query, limit*3)
	var results []map[string]interface{}
	aiEnhanced := false

	if resultsRaw != nil {
		// Try to cast to []map[string]interface{} (adapter in root converts)
		if arr, ok := resultsRaw.([]map[string]interface{}); ok {
			results = arr
		}
	}

	// AI rerank when available
	if s.ai != nil && s.ai.Enabled() && results != nil {
		if reranked := s.aiRerank(query, limit, results); reranked != nil {
			if arr, ok := reranked.([]map[string]interface{}); ok {
				results = arr
				aiEnhanced = true
			}
		}
	}

	// Fall back to linear if FTS5 unavailable
	if results == nil {
		linearRaw := s.searchLinear(query)
		if linearRaw != nil {
			if arr, ok := linearRaw.([]map[string]interface{}); ok {
				results = arr
			}
		}
	}

	var text strings.Builder
	text.WriteString(fmt.Sprintf("搜索 '%s' 找到 %d 个结果", query, len(results)))
	if aiEnhanced {
		text.WriteString(" (AI 语义重排)")
	}
	for _, h := range results {
		text.WriteString(fmt.Sprintf("\n- [%s] %s (%s)", u.Str(h["type"]), u.Str(h["title"]), u.Str(h["id"])))
	}

	reflection := ""
	if len(results) == 0 {
		reflection = "[工具提示] smart_search 无结果。如需搜索讨论内容使用 aipm_search_discussions。"
	} else if aiEnhanced {
		reflection = "[工具提示] 结果已通过 AI 语义重排序。"
	}

	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: text.String()}},
		RelatedContext: map[string]interface{}{"results": results, "ai_enhanced": aiEnhanced},
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleReadDiscussions(args map[string]interface{}) mcpToolResult {
	source := getStr(args, "source", "")
	since := getStr(args, "since", "")
	lastN := getInt(args, "last_n", 0)
	cursor := getStr(args, "cursor", "")
	projectPath := getStr(args, "project_path", "")
	full := false
	if v, ok := args["full"].(bool); ok {
		full = v
	} else if v := getStr(args, "full", ""); v == "true" || v == "1" {
		full = true
	}

	rows, err := store.ReadDiscussions(store.ReadDiscussionsOpts{
		Source:      source,
		LastN:       lastN,
		Since:       since,
		Cursor:      cursor,
		Full:        full,
		ProjectPath: projectPath,
	})
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("读取讨论失败: %v", err)}},
			IsError: true,
		}
	}

	var header strings.Builder
	header.WriteString(fmt.Sprintf("讨论记录: %d 条", len(rows)))
	if source != "" {
		header.WriteString(fmt.Sprintf(" [source=%s]", source))
	}
	if since != "" {
		header.WriteString(fmt.Sprintf(" [since=%s]", since))
	}
	header.WriteString("\n\n")

	text := header.String() + discussion.FormatResults(rows, full)
	reflection := ""
	if len(rows) == 0 {
		if cursor != "" {
			reflection = "cursor 之后无新讨论。如需扩大范围，不传 cursor 重新调用。"
		} else {
			reflection = "未找到讨论记录。确认 source 拼写或扩大 since 时间窗。"
		}
	} else if !full {
		reflection = "内容为预览（约 200 字）。互读讨论请设 full=true。看某 Agent → source=\"cursor\" + full=true。"
	} else if source == "" {
		reflection = "已返回全文。若只看某个 Agent，加 source=\"cursor\" 或 source=\"claude-code\" 等。"
	}

	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: text}},
		RelatedContext: map[string]interface{}{"results": rows, "count": len(rows), "full": full, "cursor": discussion.CursorFromResults(rows)},
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleSearchDiscussions(args map[string]interface{}) mcpToolResult {
	query := getStr(args, "query", "")
	source := getStr(args, "source", "")
	typeFilter := getStr(args, "type", "")
	projectPath := getStr(args, "project_path", "")
	mode := getStr(args, "mode", "matches")
	limit := getInt(args, "limit", 10)
	lastN := getInt(args, "last_n", 0)
	cursor := getStr(args, "cursor", "")

	if lastN <= 0 && query == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "请提供 query（关键词搜索）或 last_n（最近 N 条记录）"}},
			IsError: true,
		}
	}

	var results []map[string]any
	var total int

	if lastN > 0 {
		// Recent-N mode: fetch most recent N records
		var err error
		results, err = store.ListRecentDiscussions(source, typeFilter, projectPath, lastN, cursor)
		if err != nil {
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("获取最近讨论失败: %v", err)}},
				IsError: true,
			}
		}
		total = len(results)
	} else {
		// Keyword search mode (existing behavior)
		var err error
		results, total, err = s.searchDiscussions(query, source, typeFilter, projectPath, 1, limit)
		if err != nil {
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("搜索讨论失败: %v", err)}},
				IsError: true,
			}
		}
	}

	// Full session mode: expand results to include complete sessions
	if mode == "full_session" && len(results) > 0 {
		// Cap sessions to avoid overwhelming output (limit controls session count in this mode)
		sessionLimit := limit
		if sessionLimit <= 0 || sessionLimit > 5 {
			sessionLimit = 5
		}
		results = expandToFullSessions(results, sessionLimit)
	} else if len(results) > limit {
		results = results[:limit]
	}

	var b strings.Builder
	if lastN > 0 {
		b.WriteString(fmt.Sprintf("最近 %d 条讨论记录", lastN))
	} else {
		b.WriteString(fmt.Sprintf("搜索讨论历史 '%s'", query))
	}
	if source != "" {
		b.WriteString(fmt.Sprintf(" [source=%s]", source))
	}
	if typeFilter != "" {
		b.WriteString(fmt.Sprintf(" [type=%s]", typeFilter))
	}
	if mode == "full_session" {
		b.WriteString(" [完整 session 模式]")
	}
	b.WriteString(fmt.Sprintf(": %d 条结果 (共 %d 条)\n", len(results), total))

	// Group by session for full_session mode display
	if mode == "full_session" {
		sessionGroups := groupBySession(results)
		for _, sg := range sessionGroups {
			b.WriteString(discussion.FormatSessionMessages(sg.sessionID, sg.messages, true))
		}
	} else {
		for _, r := range results {
			role := u.Str(r["role"])
			src := u.Str(r["source"])
			content := discussion.PreviewContent(u.Str(r["content"]), discussion.PreviewRunes)
			b.WriteString(fmt.Sprintf("\n- [%s][%s] %s  %s", role, src, u.Str(r["created_at"]), content))
		}
	}

	reflection := ""
	if len(results) == 0 {
		reflection = "未找到相关讨论记录。"
	} else if mode == "full_session" {
		reflection = fmt.Sprintf("已展开 %d 条 session 消息（全文，未截断）。", len(results))
	} else {
		reflection = fmt.Sprintf("匹配预览约 %d 字。全文请用 aipm_read_discussions(full=true) 或 mode=full_session。", discussion.PreviewRunes)
		if source != "" || typeFilter != "" {
			reflection += fmt.Sprintf(" 已过滤 source=%s type=%s。", source, typeFilter)
		}
	}

	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: b.String()}},
		RelatedContext: map[string]interface{}{"results": results, "total": total, "mode": mode},
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleLogDiscussion(args map[string]interface{}) mcpToolResult {
	content := getStr(args, "content", "")
	role := getStr(args, "role", "assistant")
	session := getStr(args, "session", "")

	if content == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "content 为必填项"}}, IsError: true}
	}

	res, err := store.LogDiscussion(session, role, "mcp", content, "")
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("记录讨论失败: %v", err)}}, IsError: true}
	}

	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 讨论已记录 [%s]", res["id"])}},
		RelatedContext: res,
	}
}

// ---- Thread (线索) Handlers ----

func (s *mcpServer) handleSuggestThreads(args map[string]interface{}) mcpToolResult {
	suggestions := analyze.AnalyzeThreadSuggestions()
	status := analyze.AnalyzeThreadStatus()

	var text string
	if len(suggestions) == 0 {
		text = "未发现新的线索建议。"
	} else {
		text = fmt.Sprintf("发现 %d 条线索建议：\n", len(suggestions))
		for _, sug := range suggestions {
			text += fmt.Sprintf("- **%s** — %s (score: %.0f%%)\n", sug.SuggestedTitle, sug.Rationale, sug.Score*100)
		}
	}

	paused := []string{}
	for _, ts := range status {
		if ts.Paused {
			paused = append(paused, fmt.Sprintf("%s (%d 天无活动)", ts.ThreadTitle, ts.DaysSinceLastActivity))
		}
	}
	if len(paused) > 0 {
		text += fmt.Sprintf("\n⏸️ 暂停的线索: %s", strings.Join(paused, ", "))
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: text},
		},
		RelatedContext: map[string]any{
			"suggestions":   suggestions,
			"thread_status": status,
		},
		Reflection: fmt.Sprintf("建议 %d 条新线索，%d 条线索暂停中", len(suggestions), len(paused)),
	}
}

func (s *mcpServer) handleCreateThread(args map[string]interface{}) mcpToolResult {
	title := getStr(args, "title", "")
	summary := getStr(args, "summary", "")

	if title == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "title 为必填项"}},
			IsError: true,
		}
	}

	t, err := store.CreateThread(title, summary, "agent")
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建线索失败: %v", err)}},
			IsError: true,
		}
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("线索已创建: %s [%s]", title, t["id"])},
		},
		RelatedContext: t,
		Reflection:     fmt.Sprintf("线索 '%s' 已创建", title),
	}
}

func (s *mcpServer) handleAddToThread(args map[string]interface{}) mcpToolResult {
	threadID := getStr(args, "thread_id", "")
	entityType := getStr(args, "entity_type", "")
	entityID := getStr(args, "entity_id", "")
	note := getStr(args, "note", "")

	if threadID == "" || entityType == "" || entityID == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "thread_id, entity_type, entity_id 为必填项"}},
			IsError: true,
		}
	}

	t, err := store.AddToThread(threadID, entityType, entityID, note)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("添加失败: %v", err)}},
			IsError: true,
		}
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("已将 %s/%s 添加到线索 %s", entityType, entityID, threadID)},
		},
		RelatedContext: t,
		Reflection:     fmt.Sprintf("已添加到线索 '%s'", u.Str(t["title"])),
	}
}

func (s *mcpServer) handleDailyReview(args map[string]interface{}) mcpToolResult {
	commits, err := store.ListRecentCommitsWithContext(100)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("获取 commit 失败: %v", err)}},
			IsError: true,
		}
	}

	threads, _ := store.ListThreads("active")
	suggestions := analyze.AnalyzeThreadSuggestions()
	status := analyze.AnalyzeThreadStatus()

	var text strings.Builder
	text.WriteString(fmt.Sprintf("## 每日复盘 — 共 %d 条 commit\n\n", len(commits)))
	text.WriteString("请独立分析以下 commits 之间的语义关联（而非仅依赖启发式建议），然后：\n")
	text.WriteString("1. 识别逻辑上属于同一工作流的 commits，用 aipm_create_thread 创建线索\n")
	text.WriteString("2. 对匹配已有线索的 commits，用 aipm_add_to_thread 追加\n")
	text.WriteString("3. 每个线索应有一个**有意义的标题**和 **summary**（说明这条线索在做什么、为什么重要）\n")
	text.WriteString("4. 不要盲目接受启发式建议 — 用你的判断力\n\n")

	if len(threads) > 0 {
		text.WriteString("### 已有线索（可追加）\n")
		for _, t := range threads[:min(5, len(threads))] {
			items := t["items"]
			itemCount := 0
			if arr, ok := items.([]map[string]any); ok {
				itemCount = len(arr)
			}
			text.WriteString(fmt.Sprintf("- %s [%s] (%d items)\n", u.Str(t["title"]), u.Str(t["id"]), itemCount))
		}
		text.WriteString("\n")
	}

	if len(suggestions) > 0 {
		text.WriteString("### 启发式建议（仅供参考，不要盲从）\n")
		for _, sug := range suggestions {
			text.WriteString(fmt.Sprintf("- %s (score: %.0f%%)\n", sug.SuggestedTitle, sug.Score*100))
		}
		text.WriteString("\n")
	}

	// Commit list
	text.WriteString("### 近期 Commits（含上下文）\n")
	for i, c := range commits[:min(30, len(commits))] {
		text.WriteString(fmt.Sprintf("%d. `%s` — **%s**\n", i+1, c.ID[:min(8, len(c.ID))], c.Title))
		if c.TaskTitle != "" {
			text.WriteString(fmt.Sprintf("   task: %s [%s]", c.TaskTitle, c.TaskID))
		}
		if c.PlanTitle != "" {
			text.WriteString(fmt.Sprintf(" | plan: %s", c.PlanTitle))
		}
		if len(c.Files) > 0 {
			topFiles := c.Files[:min(3, len(c.Files))]
			text.WriteString(fmt.Sprintf(" | files: %s", strings.Join(topFiles, ", ")))
			if len(c.Files) > 3 {
				text.WriteString(fmt.Sprintf(" +%d more", len(c.Files)-3))
			}
		}
		if len(c.InThreads) > 0 {
			text.WriteString(fmt.Sprintf(" | 已有线索: %s", strings.Join(c.InThreads, ", ")))
		}
		text.WriteString("\n")
	}

	paused := []string{}
	for _, ts := range status {
		if ts.Paused {
			paused = append(paused, fmt.Sprintf("%s (%d 天无活动)", ts.ThreadTitle, ts.DaysSinceLastActivity))
		}
	}
	if len(paused) > 0 {
		text.WriteString(fmt.Sprintf("\n⏸️ 暂停的线索: %s\n", strings.Join(paused, ", ")))
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: text.String()},
		},
		RelatedContext: map[string]any{
			"commits":          commits,
			"existing_threads": threads,
			"suggestions":      suggestions,
			"thread_status":    status,
		},
		Reflection: fmt.Sprintf("已提供 %d 条 commit 供分析，%d 条启发式建议，%d 条已有线索", len(commits), len(suggestions), len(threads)),
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

// supportedProtocolVersions lists MCP protocol versions this server supports
// (date-based versions per the MCP spec). The server echoes back the client's
// requested version if it's in this list; otherwise it negotiates down.
var supportedProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

func (s *mcpServer) handleInitialize(msg *jsonrpcMessage) {
	// Extract the client's requested protocol version and client info ("user agent").
	clientVersion := ""
	if msg.Params != nil {
		var params struct {
			ProtocolVersion string        `json:"protocolVersion"`
			ClientInfo      mcpClientInfo `json:"clientInfo"`
		}
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			clientVersion = params.ProtocolVersion
			// Capture client identity for debugging / analytics.
			// This is cross-platform: all MCP clients (Claude Code, Cursor, etc.)
			// send clientInfo regardless of host OS.
			if params.ClientInfo.Name != "" {
				s.clientInfo = &params.ClientInfo
				fmt.Fprintf(os.Stderr, "[aipm-mcp] client connected: %s v%s (protocol %s)\n",
					params.ClientInfo.Name, params.ClientInfo.Version, clientVersion)
			}
		}
	}

	// Echo back the client's version if we support it; otherwise fall back to
	// the oldest stable version for maximum compatibility.
	protoVersion := "2024-11-05"
	if supportedProtocolVersions[clientVersion] {
		protoVersion = clientVersion
	}

	result := map[string]interface{}{
		"protocolVersion": protoVersion,
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

// recordMCPError writes an mcp_error event when an MCP tool call fails.
// This feeds into the INJECT pipeline so the next LLM request gets a
// corrective hint about the failed operation.
func recordMCPError(toolName string, args map[string]interface{}, result mcpToolResult) {
	// Extract the target entity ID for dedup
	entityID := ""
	for _, k := range []string{"task_id", "commit_id", "plan_id", "bug_id", "decision_id"} {
		if v, ok := args[k].(string); ok && v != "" {
			entityID = v
			break
		}
	}
	if entityID == "" {
		entityID = toolName // fallback: dedup by tool name
	}

	// Dedup: skip if same entity already has an unconsumed mcp_error
	if store.HasUnconsumedEvent("mcp_error", entityID) {
		return
	}

	// Extract error text
	errText := ""
	for _, c := range result.Content {
		if c.Type == "text" {
			errText += c.Text
		}
	}

	summary := fmt.Sprintf("工具 %s 失败: %s", toolName, u.TruncateStr(errText, 120))
	store.CreateEvent("mcp_error", toolName, entityID, summary)
	u.LogShared("MCP-ERR", "tool=%s entity=%s error=%s", 
		toolName, entityID[:min(len(entityID), 16)], u.TruncateStr(errText, 60))
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

	// Log MCP tool usage to discussion_log — MCP tools are invisible to
	// Claude Code hooks, so we log them here for full traceability.
	mcpLogDiscussion(call.Name, call.Arguments, result)

	// Also write to shared observability log
	status := "OK"
	if result.IsError {
		status = "ERR"
		// P3: record MCP error as event for next INJECT correction hint
		recordMCPError(call.Name, call.Arguments, result)
	}
	u.LogShared("MCP", "tool=%s status=%s | %s", call.Name, status, mcpLogSummary(call.Name, call.Arguments))


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

// mcpLogDiscussion logs an MCP tool call to the discussion log.
// MCP tools are invisible to Claude Code hooks, so we log them here
// for full traceability in the PM dashboard.
func mcpLogDiscussion(toolName string, args map[string]interface{}, result mcpToolResult) {
	// Build a concise human-readable summary
	summary := "📡 " + toolName
	switch {
	case result.IsError:
		summary += " ❌"
	default:
		summary += " ✅"
	}

	// Include the first key argument as context
	switch toolName {
	case "aipm_search_context", "aipm_smart_search", "aipm_search_discussions", "aipm_read_discussions":
		if q, ok := args["query"].(string); ok && q != "" {
			if len(q) > 60 {
				q = q[:60] + "..."
			}
			summary += " \"" + q + "\""
		}
		if ln, ok := args["last_n"]; ok {
			summary += fmt.Sprintf(" last_n=%v", ln)
		}
		if m, ok := args["mode"].(string); ok && m != "" {
			summary += " mode=" + m
		}
	case "aipm_create_task":
		if t, ok := args["title"].(string); ok {
			summary += " \"" + t + "\""
		}
	case "aipm_record_commit":
		if t, ok := args["title"].(string); ok {
			summary += " \"" + t + "\""
		}
	case "aipm_record_bug":
		if t, ok := args["title"].(string); ok {
			summary += " \"" + t + "\""
		}
	case "aipm_update_task_status":
		if tid, ok := args["task_id"].(string); ok {
			summary += " " + tid
		}
		if st, ok := args["status"].(string); ok {
			summary += " →" + st
		}
	case "aipm_append_task_note":
		if tid, ok := args["task_id"].(string); ok {
			summary += " " + tid
		}
	case "aipm_link_entities":
		if rel, ok := args["relation"].(string); ok {
			summary += " " + rel
		}
	case "aipm_record_decision":
		if t, ok := args["title"].(string); ok {
			summary += " \"" + t + "\""
		}
	}

	// Include reflection in metadata
	var metaJSON string
	if result.Reflection != "" {
		type mcpMeta struct {
			Type       string `json:"type"`
			Tool       string `json:"tool"`
			Reflection string `json:"reflection"`
		}
		meta := mcpMeta{Type: "mcp_tool", Tool: toolName, Reflection: result.Reflection}
		if b, err := json.Marshal(meta); err == nil {
			metaJSON = string(b)
		}
	}

	store.LogDiscussion("", "assistant", "claude-code", summary, metaJSON)
}

// mcpLogSummary extracts a concise identifier from MCP tool arguments for logging.
func mcpLogSummary(tool string, args map[string]interface{}) string {
	switch tool {
	case "aipm_record_commit":
		return fmt.Sprintf("task=%s title=%s", strArg(args, "task_id"), truncArg(args, "title", 60))
	case "aipm_create_task":
		return fmt.Sprintf("title=%s plan=%s", truncArg(args, "title", 60), strArg(args, "plan_id"))
	case "aipm_record_bug":
		return fmt.Sprintf("title=%s severity=%s", truncArg(args, "title", 60), strArg(args, "severity"))
	case "aipm_update_task_status":
		return fmt.Sprintf("task=%s status=%s", strArg(args, "task_id"), strArg(args, "status"))
	case "aipm_search_context", "aipm_smart_search", "aipm_search_discussions":
		return fmt.Sprintf("q=%s", truncArg(args, "query", 50))
	case "aipm_read_discussions":
		return fmt.Sprintf("src=%s last_n=%s", strArg(args, "source"), strArg(args, "last_n"))
	case "aipm_link_entities":
		return fmt.Sprintf("%s.%s -> %s.%s", strArg(args, "source_type"), strArg(args, "source_id"), strArg(args, "target_type"), strArg(args, "target_id"))
	case "aipm_record_decision":
		return fmt.Sprintf("title=%s", truncArg(args, "title", 60))
	case "aipm_create_thread":
		return fmt.Sprintf("title=%s", truncArg(args, "title", 60))
	case "aipm_append_task_note":
		return fmt.Sprintf("task=%s", strArg(args, "task_id"))
	default:
		return ""
	}
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	if v == "" {
		return "-"
	}
	return v
}

func truncArg(args map[string]interface{}, key string, max int) string {
	s := strArg(args, key)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
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

// getInt reads an integer parameter from MCP tool args.
// Clients may send numbers as float64 (JSON), int/int64 (native bridges), or string.
func getInt(m map[string]interface{}, key string, def int) int {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case string:
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i
		}
	}
	return def
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

// ---- Discussion search helpers ----

// sessionGroup holds a session ID and its messages (ordered by time).
type sessionGroup struct {
	sessionID string
	messages  []map[string]any
}

// expandToFullSessions takes search results and expands them to include all
// messages from each result's session. Sessions are deduplicated.
func expandToFullSessions(matches []map[string]any, maxSessions int) []map[string]any {
	// Collect unique session IDs in order of first appearance
	seen := map[string]bool{}
	var sessionIDs []string
	for _, m := range matches {
		sid := u.Str(m["session_id"])
		if sid != "" && !seen[sid] {
			seen[sid] = true
			sessionIDs = append(sessionIDs, sid)
		}
	}

	if maxSessions > 0 && len(sessionIDs) > maxSessions {
		sessionIDs = sessionIDs[:maxSessions]
	}

	var allMessages []map[string]any
	for _, sid := range sessionIDs {
		msgs, err := store.GetSessionMessages(sid)
		if err != nil {
			// Fall back to the original match if we can't get full session
			for _, m := range matches {
				if u.Str(m["session_id"]) == sid {
					allMessages = append(allMessages, m)
					break
				}
			}
			continue
		}
		for i := range msgs {
			allMessages = append(allMessages, msgs[i])
		}
	}
	if allMessages == nil {
		allMessages = []map[string]any{}
	}
	return allMessages
}

// groupBySession groups messages by session_id, preserving chronological order within each session.
func groupBySession(messages []map[string]any) []sessionGroup {
	order := []string{}
	groups := map[string][]map[string]any{}
	for _, m := range messages {
		sid := u.Str(m["session_id"])
		if _, ok := groups[sid]; !ok {
			order = append(order, sid)
		}
		groups[sid] = append(groups[sid], m)
	}

	var result []sessionGroup
	for _, sid := range order {
		result = append(result, sessionGroup{sessionID: sid, messages: groups[sid]})
	}
	if result == nil {
		result = []sessionGroup{}
	}
	return result
}

// handleVision processes the aipmc_vision MCP tool call.
func (s *mcpServer) handleVision(args map[string]interface{}) mcpToolResult {
	imagePath := getStr(args, "image_path", "")
	prompt := getStr(args, "prompt", "")
	iteration := getInt(args, "iteration", 1)

	if imagePath == "" || prompt == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "缺少必填参数 image_path 或 prompt"}},
			IsError: true,
		}
	}

	result := vision.RunVision(imagePath, prompt, iteration, getStr(args, "model", ""))

	if !result.OK {

		u.LogShared("MCP", "tool=aipmc_vision status=ERR err=%s image=%s iter=%d", result.Error, imagePath, iteration)
		msg := fmt.Sprintf("视觉分析失败 (%s): %s", result.Error, result.Message)
		switch result.Error {
		case "no_vision_model":
			msg = "没有可用的视觉模型。请先在 Settings → LLM 网关 中配置 tags 包含 \"vision\" 的模型（如 qwen3.5-4b-vision），并确保 llama-server 已启动。"
		case "timeout":
			msg = "视觉模型响应超时。可能原因：1) llama-server 未启动 2) 模型加载中（首次较慢）3) 图片太大。建议重试或换更快的模型。"
		case "unavailable":
			msg = "视觉模型服务不可用（llama-server 可能未启动或已崩溃）。请检查本地 llama-server 是否正常运行。"
		case "network":
			msg = "无法连接视觉模型服务。请确认 llama-server 已启动且端口 8080 可访问。"
		}
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: msg}},
			IsError: true,
		}
	}

	info := result.Text
	if result.Resize != "" {
		info = fmt.Sprintf("[%s (%s)] %s", result.Model, result.Resize, result.Text)
	}

	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: info}},
	}
}
