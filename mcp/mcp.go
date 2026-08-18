package mcp

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"aipmc/ai"
	"aipmc/analyze"
	pmdb "aipmc/db"
	"aipmc/discussion"
	"aipmc/store"
	"aipmc/u"
	"aipmc/vision"
)

// ============================================================
// MCP Server — JSON-RPC over stdio (Phase 2)
// ============================================================

// MCPTool defines a tool that the MCP server exposes to the Agent.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema MCPInputSchema `json:"inputSchema"`
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

// mcpClientName returns the MCP client's declared user agent, normalized to
// discussion_log source naming (claude-code / codex-cli / cursor / ...).
// Each agent runs its own stdio MCP process, so clientInfo is stable for the
// lifetime of the connection (set once at initialize) — no cross-agent residue.
func mcpClientName(ci *mcpClientInfo) string {
	if ci == nil || ci.Name == "" {
		return ""
	}
	switch strings.ToLower(ci.Name) {
	case "claude", "claude-code", "claudecode":
		return "claude-code"
	case "codex", "codex-cli", "codex-mcp-client":
		return "codex-cli"
	case "cursor":
		return "cursor"
	case "opencode":
		return "opencode"
	case "gemini", "gemini-cli":
		return "gemini-cli"
	}
	return ci.Name
}

// mcpServer holds registered tools and handles the protocol lifecycle.
type mcpServer struct {
	tools      map[string]MCPTool
	handlers   map[string]mcpToolHandler
	reader     *bufio.Reader
	writer     io.Writer
	ai         *ai.Client
	clientInfo *mcpClientInfo // captured from initialize — the client's "user agent"
	// Search functions injected from root
	searchContext     func(string, int) map[string]any
	searchFTS5        func(string, int) interface{}
	searchLinear      func(string) interface{}
	aiRerank          func(string, int, interface{}) interface{}
	searchDiscussions func(string, string, string, string, string, string, int, int) ([]map[string]any, int, error)
}

func NewServer(aiClient *ai.Client,
	searchContext func(string, int) map[string]any,
	searchFTS5 func(string, int) interface{},
	searchLinear func(string) interface{},
	aiRerank func(string, int, interface{}) interface{},
	searchDiscussions func(string, string, string, string, string, string, int, int) ([]map[string]any, int, error),
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
		Description: "获取当前项目简报。包含进行中的任务、PM 最新变更、进度风险、重复检测、scope 漂移、最近 Agent 活动等分析结果。Agent 在开始编码前应调用此工具获取最新上下文。支持 level=summary（执行摘要，省 token）与 level=full（完整分析，默认）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"level": map[string]string{"type": "string", "description": "可选: 'summary'=执行摘要（计数级+前 5 条，约 1-3KB，省 token）；'full'=完整分析（默认）。快速状态检查用 summary，深挖细节用 full。"},
			},
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

	// Entity query tools — precise Get/List for PM entities
	// Usage pattern: search_context 发现实体 → get_xxx 查看详情 → 决策下一步操作
	s.addTool(MCPTool{
		Name:        "aipm_get_task",
		Description: "当你从 search_context/smart_search 结果中看到一个 task ID，或从 commit/plan 中获知 task ID 后，调用此工具查看该 task 的完整详情。\n\n返回：task 的标题、状态（todo/in_progress/blocked/done）、优先级（P0/P1/P2）、所属 phase、关联 plan、所有 commit 记录（含 review/test 状态）、最新备注。\n\n与 aipm_list_tasks 的区别：get_task 需要已知 task_id，返回单个 task 的完整信息；list_tasks 按条件过滤返回多个 task 的摘要列表。如果不知道 task_id，先用 aipm_search_context 搜索。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id": map[string]string{"type": "string", "description": "Task ID。可从 search_context/smart_search 搜索结果、plan 详情、或 commit 的 task_id 字段中获取"},
			},
			Required: []string{"task_id"},
		},
	}, s.handleGetTask)

	s.addTool(MCPTool{
		Name:        "aipm_list_tasks",
		Description: "当你需要查看某个 plan 下有哪些 task、或需要找某个状态的所有 task 时调用。常用场景：(1) 创建新 task 前，检查同一个 plan 下是否已有类似 task (2) 查看自己当前有哪些 in_progress 的 task (3) 找 blocked 状态的 task 排查阻塞原因。\n\n返回：匹配条件的 task 摘要列表（ID + 标题 + 状态 + 优先级 + phase）。不返回 commit 详情和备注全文——需要详情时对结果中的 task ID 调用 aipm_get_task。\n\n与 aipm_search_context 的区别：list_tasks 按 status/plan_id 精确过滤，适合浏览型查询；search_context 按关键词模糊匹配，适合「搜有没有类似的 task」。两者互补：先用 search_context 搜关键词，确认 plan_id 后再用 list_tasks 列出该 plan 的全部 task。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"status":  map[string]string{"type": "string", "description": "过滤 task 状态。可选值: todo / in_progress / blocked / done。不传则返回全部"},
				"plan_id": map[string]string{"type": "string", "description": "过滤所属 Plan。plan_id 可从 aipm_search_context 结果或 aipm_list_plans 中获取。不传则跨所有 plan"},
			},
		},
	}, s.handleListTasks)

	s.addTool(MCPTool{
		Name:        "aipm_get_commit",
		Description: "当你从 search_context/task 详情/日常 review 中看到 commit ID 后，调用此工具查看该 commit 的完整记录。\n\n返回：commit 标题、摘要、关联的 task_id 和 decision_id、review 状态（pending/approved/rejected/auto）、test 状态（not_run/passed/failed/auto）、变更文件列表、分支名、创建时间。\n\n与 aipm_list_commits 的区别：get_commit 需要已知 commit_id，返回单个 commit 的完整信息；list_commits 按 task_id/status 过滤返回多个 commit 的摘要列表。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"commit_id": map[string]string{"type": "string", "description": "Commit ID。可从 search_context 结果、task 详情中的 commits 列表、或 aipm_list_commits 中获取"},
			},
			Required: []string{"commit_id"},
		},
	}, s.handleGetCommit)

	s.addTool(MCPTool{
		Name:        "aipm_list_commits",
		Description: "当你需要查看某个 task 下的所有 commit、或按状态过滤 commit 时调用。常用场景：(1) 检查一个 task 是否已有足够的 commit 来标记 done (2) 查看最近的提交活动 (3) 排查某个 task 的 commit 历史。\n\n返回：匹配条件的 commit 摘要列表（ID + 标题 + status + review_status）。不返回文件列表全文——需要详情时对结果中的 commit ID 调用 aipm_get_commit。\n\n提示：如果知道 task_id，用 task_id 参数过滤最精确；如果只想看最近的 commit，用 limit 参数控制数量即可。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id": map[string]string{"type": "string", "description": "按关联的 Task ID 过滤。task_id 可从 aipm_search_context 或 aipm_list_tasks 结果中获取"},
				"status":  map[string]string{"type": "string", "description": "按 commit 状态过滤。可选值: committed / draft / merged"},
				"limit":   map[string]string{"type": "integer", "description": "返回数量上限，默认 50。最近创建的 commit 优先返回"},
				"offset":  map[string]string{"type": "integer", "description": "分页偏移量，配合 limit 实现翻页。默认 0。"},
				"orphan":  map[string]string{"type": "boolean", "description": "设为 true 时只返回未关联 task 的孤儿 commit（task_id 为空）。"},
			},
		},
	}, s.handleListCommits)

	s.addTool(MCPTool{
		Name:        "aipm_get_plan",
		Description: "当你从 search_context/task/roadmap 中看到 plan ID 后，调用此工具查看该 plan 的完整规划。\n\n返回：plan 标题、目标（goal）、状态（draft/active/done/deprecated）、优先级、所属 roadmap_id 和 vision_id、scope（范围）、risks（风险）、assumptions（假设）、关联的所有 task IDs。\n\n重要：在创建 task 之前，先用 aipm_get_plan 确认该 plan 的目标和 scope——确保新 task 与 plan 方向一致。创建 task 时必须提供 plan_id，用此工具可验证 plan_id 是否正确。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"plan_id": map[string]string{"type": "string", "description": "Plan ID。可从 aipm_search_context 搜索结果、task 详情、或 aipm_list_plans 中获取"},
			},
			Required: []string{"plan_id"},
		},
	}, s.handleGetPlan)

	s.addTool(MCPTool{
		Name:        "aipm_list_plans",
		Description: "当你需要找「应该在哪个 plan 下创建 task」或查看所有活跃 plan 时调用。这是创建 task 前的必经步骤——因为 aipm_create_task 需要 plan_id。\n\n返回：匹配条件的 plan 摘要列表（ID + 标题 + 状态 + 优先级）。\n\n常用模式：(1) 用 status=active 过滤出活跃 plan，从中选择合适的 plan_id (2) 用 roadmap_id 过滤查看某个 roadmap 下的所有 plan (3) 不带参数列出全部 plan。\n\n与 aipm_search_context 的区别：list_plans 按 status/roadmap_id 精确过滤，返回结构化列表；search_context 按关键词模糊匹配 plan 标题和内容。两者互补：不确定关键词时用 list_plans 浏览，知道具体名称时用 search_context 搜索。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"roadmap_id": map[string]string{"type": "string", "description": "按 Roadmap ID 过滤。roadmap_id 可从 aipm_search_context 中获取"},
				"status":     map[string]string{"type": "string", "description": "按状态过滤。可选值: draft / active / done / deprecated。推荐用 active 找可用的 plan"},
			},
		},
	}, s.handleListPlans)

	s.addTool(MCPTool{
		Name:        "aipm_get_bug",
		Description: "当你从 search_context 结果或 commit 详情中看到 bug ID 后，调用此工具查看该 bug 的完整记录。\n\n返回：bug 标题、严重级别（critical/major/minor）、状态（open/in_progress/resolved/closed）、完整错误信息、根因分析、修复方案、标签、关联的 commit_id。\n\n与 aipm_list_bugs 的区别：get_bug 需要已知 bug_id，返回单个 bug 的完整信息；list_bugs 按 severity/status 过滤返回多个 bug 的摘要列表。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"bug_id": map[string]string{"type": "string", "description": "Bug ID。可从 search_context 搜索结果、commit 详情、或 aipm_list_bugs 中获取"},
			},
			Required: []string{"bug_id"},
		},
	}, s.handleGetBug)

	s.addTool(MCPTool{
		Name:        "aipm_list_bugs",
		Description: "当你需要查看有哪些未解决的 bug、或按严重级别排查问题时调用。常用场景：(1) 开始编码前检查是否有相关的 open bug (2) 定期查看 critical 级别的 bug 是否需要优先处理。\n\n返回：匹配条件的 bug 摘要列表（ID + 标题 + severity + status）。不返回错误详情和根因分析——需要时对结果中的 bug ID 调用 aipm_get_bug。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"status":   map[string]string{"type": "string", "description": "按状态过滤。可选值: open / in_progress / resolved / closed。推荐用 open 查看未解决的 bug"},
				"severity": map[string]string{"type": "string", "description": "按严重级别过滤。可选值: critical / major / minor"},
				"limit":    map[string]string{"type": "integer", "description": "返回数量上限，默认 20"},
				"offset":   map[string]string{"type": "integer", "description": "分页偏移量，配合 limit 实现翻页。默认 0"},
			},
		},
	}, s.handleListBugs)

	s.addTool(MCPTool{
		Name:        "aipm_get_decision",
		Description: "当你从 search_context 结果或 task 关联中看到 decision ID 后，调用此工具查看该架构/技术决策的完整内容。\n\n返回：decision 标题、状态（proposed/accepted/deprecated）、日期、背景（background）、决策内容（decision_text）。\n\n在实现新功能或做技术选型时，先用 aipm_search_context 或 aipm_list_decisions 查找相关决策，再用此工具查看详情——避免重复讨论已定的技术方向。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"decision_id": map[string]string{"type": "string", "description": "Decision ID。可从 search_context 搜索结果、task 的 related_decisions 字段、或 aipm_list_decisions 中获取"},
			},
			Required: []string{"decision_id"},
		},
	}, s.handleGetDecision)

	s.addTool(MCPTool{
		Name:        "aipm_list_decisions",
		Description: "当你需要了解项目中有哪些已做的技术决策、或做新决策前检查是否已有相关决策时调用。通常与 aipm_get_decision 配合使用：先用 list 浏览决策列表，找到感兴趣的决策 ID 后再用 get 查看完整内容。\n\n返回：全部 decision 的摘要列表（ID + 标题 + 状态 + 日期），按日期降序排列。注意：此工具不支持过滤参数，如需按关键词搜索特定决策，请用 aipm_search_context。",
		InputSchema: MCPInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleListDecisions)

	s.addTool(MCPTool{
		Name:        "aipm_record_commit",
		Description: "每次完成一轮代码修改并 git commit 后，调用此工具将 commit 记录到 PM 系统中。这是连接「代码修改」和「task 跟踪」的关键桥梁——不记录 commit 会导致 task 无法标记为 done。\n\n调用时机：git commit 完成后立即调用。即使 commit 尚未 push 也可以记录。\n\n参数要点：task_id（必填）、commit_hash（必填，`git rev-parse HEAD` 获取完整 SHA）是必填项。review_status 设为 approved、test_status 设为 passed 可以让关联 task 通过 done-gate 检查。如果不确定填什么，review_status 和 test_status 不传即可（默认 pending/not_run）。\n\n自动行为：会检测 commit 中的文件是否超出 task 所属 plan 的 scope，如超出会返回 scope drift 警告。",
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
				"commit_hash":   map[string]string{"type": "string", "description": "必填: git SHA 哈希值（`git rev-parse HEAD` 获取完整 SHA）。用于精确去重与溯源。"},
				"test_status":   map[string]string{"type": "string", "description": "可选: not_run/passed/failed，默认 not_run。设为 passed 后 task 可标记 done"},
			},
			Required: []string{"task_id", "title"},
		},
	}, s.handleRecordCommit)

	s.addTool(MCPTool{
		Name:        "aipm_record_commits",
		Description: "批量记录多个 commit 到同一个 task。一次调用替代多次 record_commit，减少 API 往返。\n\n参数: task_id(必填)、commits(必填, 数组，每项含 title/commit_hash(必填)/files/summary)。\n返回: 成功/失败计数和详情。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id":       map[string]string{"type": "string", "description": "关联的 Task ID。必填"},
				"commits":       map[string]string{"type": "array", "description": "Commit 数组。每项: {title, commit_hash(必填, git rev-parse HEAD 获取), files(可选,逗号分隔), summary(可选)}"},
				"branch":        map[string]string{"type": "string", "description": "分支名，默认 main"},
				"status":        map[string]string{"type": "string", "description": "commit/draft，默认 committed"},
				"project_path":  map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
				"review_status": map[string]string{"type": "string", "description": "可选: pending/approved/rejected，默认 pending"},
				"test_status":   map[string]string{"type": "string", "description": "可选: not_run/passed/failed，默认 not_run"},
			},
			Required: []string{"task_id", "commits"},
		},
	}, s.handleRecordCommits)

	s.addTool(MCPTool{
		Name:        "aipm_create_task",
		Description: "创建一个新 Task。创建前必须确定 task 所属的 plan——如果不确定 plan_id，先用 aipm_list_plans 或 aipm_search_context 查找合适的 plan。\n\n必填参数：title（任务标题）、plan_id（所属 plan）。可选参数：priority（P0/P1/P2，默认 P1）、status（todo/in_progress，默认 todo）、phase（所属阶段，默认 general）。\n\n自动行为：(1) 标题重复检测——如果已有相似标题的 task，会返回警告 (2) 自动回填 roadmap_id——从 plan 中继承 (3) plan 状态检查——如果 plan 已 done/deprecated，会提示冲突。\n\n常见错误：把 task_id 当成 plan_id 传入。plan_id 以 plan- 开头，task_id 以 task- 开头。如果不确定，先用 aipm_search_context 搜索确认。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":        map[string]string{"type": "string", "description": "Task 标题"},
				"plan_id":      map[string]string{"type": "string", "description": "所属 Plan ID"},
				"priority":     map[string]string{"type": "string", "description": "P0/P1/P2"},
				"status":       map[string]string{"type": "string", "description": "todo/in_progress"},
				"phase":        map[string]string{"type": "string", "description": "所属 phase"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
			},
			Required: []string{"title", "plan_id"},
		},
	}, s.handleCreateTask)

	// P1: Entity write tools — create plan/roadmap, update existing entities
	s.addTool(MCPTool{
		Name:        "aipm_create_plan",
		Description: "创建一个新 Plan。Plan 是 task 的容器——每个 task 必须属于一个 plan。创建 plan 前必须先有 roadmap（plan 需要 roadmap_id）。\n\n必填参数：title（plan 名称）、roadmap_id（所属 roadmap）。roadmap_id 可通过 aipm_search_context 搜索获取。\n\n可选参数：goal（plan 目标，建议填写）、priority（P0/P1/P2，默认 P1）、status（draft/active，默认 draft，确定后改为 active）、vision_id（所属 vision）。\n\n典型流程：(1) aipm_search_context 搜 roadmap → (2) aipm_create_plan → (3) aipm_create_task 在 plan 下创建 task。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":      map[string]string{"type": "string", "description": "Plan 标题"},
				"roadmap_id": map[string]string{"type": "string", "description": "所属 Roadmap ID。必填。用 aipm_search_context 搜索 roadmap 获取"},
				"goal":       map[string]string{"type": "string", "description": "Plan 目标/描述（建议填写）"},
				"priority":   map[string]string{"type": "string", "description": "优先级: P0/P1/P2，默认 P1"},
				"status":     map[string]string{"type": "string", "description": "状态: draft/active，默认 draft"},
				"vision_id":  map[string]string{"type": "string", "description": "所属 Vision ID（可选）"},
			},
			Required: []string{"title", "roadmap_id"},
		},
	}, s.handleCreatePlan)

	s.addTool(MCPTool{
		Name:        "aipm_create_roadmap",
		Description: "创建一个新 Roadmap。Roadmap 是 plan 的容器——plan 需要关联到 roadmap。Roadmap 代表一个时间线/里程碑/大版本。\n\n必填参数：title（roadmap 名称）。\n\n可选参数：target_date（目标日期，格式 YYYY-MM-DD）、status（planned/active/done，默认 planned）、priority（P0/P1/P2，默认 P1）、vision_id（所属 vision）。\n\n典型流程：(1) aipm_create_roadmap → (2) aipm_create_plan(roadmap_id=...) → (3) aipm_create_task(plan_id=...)。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":       map[string]string{"type": "string", "description": "Roadmap 标题"},
				"target_date": map[string]string{"type": "string", "description": "目标日期，格式 YYYY-MM-DD（可选）"},
				"status":      map[string]string{"type": "string", "description": "状态: planned/active/done，默认 planned"},
				"priority":    map[string]string{"type": "string", "description": "优先级: P0/P1/P2，默认 P1"},
				"vision_id":   map[string]string{"type": "string", "description": "所属 Vision ID（可选）"},
			},
			Required: []string{"title"},
		},
	}, s.handleCreateRoadmap)

	// Update tools — modify existing entities
	s.addTool(MCPTool{
		Name:        "aipm_update_task",
		Description: "更新 Task 的字段（标题、优先级、phase、状态等）。与 aipm_update_task_status 的区别：update_task_status 只改 status 且带 done-gate 检查；update_task 可以同时修改多个字段，不触发 done-gate。\n\n适用场景：(1) 修正 task 标题 (2) 调整优先级 (3) 更换所属 phase (4) 同时修改多个属性。\n\n参数：task_id（必填）。以下至少填一个：title、priority（P0/P1/P2）、status、phase、note（会更新 task 的 last_note）。注意改 status 不会触发 done-gate，如需 done 验证请用 aipm_update_task_status。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id":  map[string]string{"type": "string", "description": "Task ID。必填"},
				"title":    map[string]string{"type": "string", "description": "新标题（可选）"},
				"priority": map[string]string{"type": "string", "description": "新优先级: P0/P1/P2（可选）"},
				"status":   map[string]string{"type": "string", "description": "新状态。注意：此方式不触发 done-gate 检查（可选）"},
				"phase":    map[string]string{"type": "string", "description": "新 phase（可选）"},
				"note":     map[string]string{"type": "string", "description": "变更说明，会更新 last_note（可选）"},
			},
			Required: []string{"task_id"},
		},
	}, s.handleUpdateTask)

	s.addTool(MCPTool{
		Name:        "aipm_update_commit",
		Description: "更新 Commit 的元数据——review 状态、test 状态、commit 状态、摘要等。\n\n常用场景：(1) 代码 review 通过后，将 review_status 改为 approved (2) 测试通过后，将 test_status 改为 passed (3) commit 合并后，将 status 改为 merged。\n\n注意：review_status=approved 且 test_status=passed（或 auto）的 commit 才能让关联 task 通过 done-gate。\n\n参数：commit_id（必填）。以下可选填一个或多个：status（committed/draft/merged）、review_status（pending/approved/rejected）、test_status（not_run/passed/failed）、summary。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"commit_id":     map[string]string{"type": "string", "description": "Commit ID。必填"},
				"status":        map[string]string{"type": "string", "description": "Commit 状态: committed/draft/merged（可选）"},
				"review_status": map[string]string{"type": "string", "description": "Review 状态: pending/approved/rejected（可选）"},
				"task_id":       map[string]string{"type": "string", "description": "可选: 将 commit 重新分配到不同的 task。用于修正错误的绑定。"},
				"test_status":   map[string]string{"type": "string", "description": "Test 状态: not_run/passed/failed（可选）"},
				"summary":       map[string]string{"type": "string", "description": "变更摘要（可选）"},
			},
			Required: []string{"commit_id"},
		},
	}, s.handleUpdateCommit)

	s.addTool(MCPTool{
		Name:        "aipm_update_bug",
		Description: "更新 Bug 的字段——状态、严重级别、修复方案等。\n\n常用场景：(1) 开始修 bug 时，将 status 改为 in_progress (2) 修复完成时，将 status 改为 resolved 并填写 fix (3) 调整严重级别。\n\n参数：bug_id（必填）。以下可选填一个或多个：status（open/in_progress/resolved/closed）、severity（critical/major/minor）、fix（修复方案描述）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"bug_id":   map[string]string{"type": "string", "description": "Bug ID。必填"},
				"status":   map[string]string{"type": "string", "description": "新状态: open/in_progress/resolved/closed（可选）"},
				"severity": map[string]string{"type": "string", "description": "新严重级别: critical/major/minor（可选）"},
				"fix":      map[string]string{"type": "string", "description": "修复方案（可选）"},
			},
			Required: []string{"bug_id"},
		},
	}, s.handleUpdateBug)

	s.addTool(MCPTool{
		Name:        "aipm_update_decision",
		Description: "更新 Decision 的状态。当决策从「提议」变为「已接受」或「已废弃」时调用。\n\n状态流转：proposed（提议中）→ accepted（已接受）→ deprecated（已废弃）。\n\n参数：decision_id（必填）、status（必填，proposed/accepted/deprecated）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"decision_id": map[string]string{"type": "string", "description": "Decision ID。必填"},
				"status":      map[string]string{"type": "string", "description": "新状态: proposed/accepted/deprecated。必填"},
			},
			Required: []string{"decision_id", "status"},
		},
	}, s.handleUpdateDecision)

	s.addTool(MCPTool{
		Name:        "aipm_update_plan",
		Description: "更新 Plan 的字段——状态、标题、目标、优先级等。\n\n常用场景：(1) plan 进入实施阶段，status 从 draft→active (2) plan 完成，status→done (3) 废弃 plan，status→deprecated (4) 修正标题或目标。\n\n参数：plan_id（必填）。以下可选填一个或多个：status（draft/active/done/deprecated）、title、goal、priority（P0/P1/P2）。\n\n注意：将 status 设为 deprecated 等同于废弃该 plan——不会删除数据，但标记为不再活跃。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"plan_id":  map[string]string{"type": "string", "description": "Plan ID。必填"},
				"status":   map[string]string{"type": "string", "description": "新状态: draft/active/done/deprecated（可选）"},
				"title":    map[string]string{"type": "string", "description": "新标题（可选）"},
				"goal":     map[string]string{"type": "string", "description": "新目标描述（可选）"},
				"priority": map[string]string{"type": "string", "description": "新优先级: P0/P1/P2（可选）"},
			},
			Required: []string{"plan_id"},
		},
	}, s.handleUpdatePlan)

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
		Description: "记录一个 Bug。当你在编码或测试过程中发现了一个明确的 bug（而非 feature request 或待讨论的问题）时调用。\n\n必填参数：title（bug 简述）、error（完整错误信息/日志/截图描述）、root_cause（根因分析——是什么导致了这个问题）、fix（修复方案——你打算怎么修或已经怎么修了）。\n\n可选参数：severity（critical/major/minor）、commit_id（引发此 bug 的 commit 或修复此 bug 的 commit）、tags（逗号分隔的标签）。\n\n提示：如果 bug 是某个 commit 引入的，用 commit_id 关联该 commit；如果 bug 已修复，记录修复 commit 后再调用 aipm_link_entities 将 bug 和修复 commit 关联起来。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"title":      map[string]string{"type": "string", "description": "Bug 标题"},
				"error":      map[string]string{"type": "string", "description": "完整错误信息"},
				"files":      map[string]string{"type": "string", "description": "相关文件，逗号分隔"},
				"root_cause": map[string]string{"type": "string", "description": "根因分析"},
				"fix":        map[string]string{"type": "string", "description": "修复方案"},
				"severity":   map[string]string{"type": "string", "description": "critical/major/minor"},
				"tags":       map[string]string{"type": "string", "description": "标签，逗号分隔"},
				"commit_id":  map[string]string{"type": "string", "description": "关联的 commit ID（可选）"},
			},
			Required: []string{"title", "error", "root_cause", "fix"},
		},
	}, s.handleRecordBug)

	s.addTool(MCPTool{
		Name:        "aipm_update_task_status",
		Description: "更新 Task 状态。当 task 的工作状态发生变化时调用——开始工作、遇到阻塞、完成任务等。\n\n状态流转：todo → in_progress（开始编码时）→ blocked（被外部依赖阻塞时）→ done（完成时）。\n\ndone-gate 检查：标记 done 前会自动检查该 task 是否有关联的 commit（通过 aipm_record_commit 记录的）且 commit 的 review_status 为 approved/auto、test_status 为 passed/auto。如果没有符合条件的 commit，done 操作会失败并返回错误提示。此时需要先用 aipm_record_commit 记录代码提交。\n\n参数：task_id（必填）、status（必填，todo/in_progress/blocked/done）、note（可选，状态变更说明）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"task_id":      map[string]string{"type": "string", "description": "Task ID"},
				"status":       map[string]string{"type": "string", "description": "todo/in_progress/blocked/done"},
				"note":         map[string]string{"type": "string", "description": "状态变更说明"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
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
		Name:        "aipm_mark_event_processed",
		Description: "标记指定实体的事件为「已处理」（区别于 mark_consumed 的「已读」）。当 Agent 已实际解决事件所指问题——如已将孤儿 commit 绑定到 task、已完成 task、已修复工具错误——调用此工具标记，使 D2 指标能区分已读/已处理。参数：entity_id（必填，commit/task 等实体 ID，与事件 entity_id 对应）、event_type（可选，如 commit_orphan/task_stale_file/hotspot_untracked/mcp_error，不传则标记该实体全部未处理事件）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"entity_id":  map[string]string{"type": "string", "description": "实体 ID（commit id / task id / 文件路径 / 工具名等，与事件 entity_id 一致）"},
				"event_type": map[string]string{"type": "string", "description": "可选: 事件类型，如 commit_orphan / task_stale_file / hotspot_untracked / mcp_error"},
			},
			Required: []string{"entity_id"},
		},
	}, s.handleMarkEventProcessed)

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
		Description: "在两个实体之间建立关联关系。当两个 PM 实体存在逻辑联系时调用——让系统能够追踪「这个 bug 是被哪个 commit 修复的」「这个 task 阻塞了哪个 task」「这个 decision 影响了哪些 task」。\n\n关系类型：fixes（修复——commit 修复 bug、task 解决 bug）、relates_to（相关——两个 task 或 commit 之间弱关联）、blocked_by（被阻塞——task 等待另一个 task 完成）、implements（实现——commit 实现了某个 decision）、depends_on（依赖）。\n\n参数：source_type + source_id（源实体）、relation（关系类型）、target_type + target_id（目标实体）。实体类型可选值：task / commit / bug / decision / idea / plan。\n\n示例：记录一个 commit 修复了一个 bug → source_type=commit, source_id=commit-xxx, relation=fixes, target_type=bug, target_id=bug-xxx。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"source_type":  map[string]string{"type": "string", "description": "源实体类型 (task/commit/bug/decision/idea/plan)"},
				"source_id":    map[string]string{"type": "string", "description": "源实体 ID"},
				"relation":     map[string]string{"type": "string", "description": "关系 (fixes/relates_to/blocked_by/implements/depends_on)"},
				"target_type":  map[string]string{"type": "string", "description": "目标实体类型"},
				"target_id":    map[string]string{"type": "string", "description": "目标实体 ID"},
				"note":         map[string]string{"type": "string", "description": "关联说明（可选）"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径。例: /Users/dazsec/projects/EncryptDrive"},
			},
			Required: []string{"source_type", "source_id", "relation", "target_type", "target_id"},
		},
	}, s.handleLinkEntities)

	s.addTool(MCPTool{
		Name:        "aipm_record_decision",
		Description: "记录一个架构或技术决策。当你在实现过程中做出了一个会影响后续开发方向的技术选择时调用——例如选择了某个库、确定了某种数据格式、采用了某种架构模式。\n\n必填参数：title（决策标题）、background（背景——当时面临什么问题，有哪些约束）、decision（决策内容——你选择了什么方案，为什么）。\n\n可选参数：status（proposed/accepted/deprecated，默认 proposed，确定后改为 accepted）。\n\n提示：做新决策前，先用 aipm_list_decisions 或 aipm_search_context 查看是否已有相关决策——避免推翻已有决策或重复讨论。",
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

	s.addTool(MCPTool{
		Name:        "aipm_feedback_triage",
		Description: "标记一条远程反馈（feedback）为已处理，写入本地 ~/.aipmc/feedback_triage.json 并启动 30 天复测窗口（B13）。处理前先用 aipm_daily_review 查看未处理反馈列表；bug 类应先用 aipm_record_bug 记录，suggestion 类应评估是否入 plan，再调用本工具标记。参数：feedback_id（必填，反馈 ID 数字）、note（可选，处理说明）。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"feedback_id": map[string]string{"type": "string", "description": "反馈 ID（必填，如 21）"},
				"note":        map[string]string{"type": "string", "description": "处理说明（可选），如关联的 bug_id / task_id 或结论"},
			},
			Required: []string{"feedback_id"},
		},
	}, s.handleFeedbackTriage)

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
		Description: "读取其他 Agent（Claude Code/Cursor/Gemini/OpenCode 等）的对话历史。想看某个 Agent 说了什么 → source 指定来源；不传 source 则返回所有人。full=true 返回全文。未指定 last_n 时默认 15 条。传 cursor 可增量读取避免重复（上次读到 disc-xxx, 从那里继续）。预览中被截断的长消息带 id=disc-xxx 线索，可用 id= 参数单条展开全文。禁止 sqlite3 直查数据库。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"source":       map[string]string{"type": "string", "description": "想看哪个 Agent: claude-code / cursor / gemini-cli / opencode / codex-cli。例：看 Cursor 说了什么 → source=\"cursor\"。不传则看所有人。"},
				"session_id":   map[string]string{"type": "string", "description": "可选: 只看某个具体 session（同一 source 可能有多个同名 agent 进程/会话，如多个 codex）。session_id 可从 aipm_list_sessions 获取。"},
				"last_n":       map[string]string{"type": "integer", "description": "最近 N 条。默认 15。快速浏览用 5，深入阅读用 30（与 since / cursor 可组合）"},
				"since":        map[string]string{"type": "string", "description": "可选: ISO 时间下限 (例 2026-06-15T21:48:00)"},
				"id":           map[string]string{"type": "string", "description": "可选: 按消息 ID 展开单条全文（B7）。预览输出中被截断的长消息会标注 [已截断 全文 N 字，展开: aipm_read_discussions id=disc-xxx]——把 disc-xxx 传入本参数即可只拉该条全文，无需 full=true 拉整个 session。"},
				"cursor":       map[string]string{"type": "string", "description": "可选: 从上次返回的 cursor 之后继续读取，避免重复（传上次返回结果中 related_context.cursor 的值）"},
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
				"session_id":   map[string]string{"type": "string", "description": "可选: 只看某个具体 session（区分同名 agent 进程）。session_id 可从 aipm_list_sessions 获取。"},
				"type":         map[string]string{"type": "string", "description": "可选: 按消息类型过滤 (user / assistant / tool)"},
				"last_n":       map[string]string{"type": "integer", "description": "可选: 返回最近 N 条记录（与 query 二选一，优先使用 last_n）"},
				"cursor":       map[string]string{"type": "string", "description": "可选: 从上次返回的 cursor 之后继续读取（仅 last_n 模式生效）"},
				"mode":         map[string]string{"type": "string", "description": "可选: 'matches' (默认，匹配消息预览约200字)；'full_session' (展开 session 全部消息且全文不截断)"},
				"since":        map[string]string{"type": "string", "description": "可选: ISO 时间下限 (例 2026-07-01T00:00:00)。keyword 搜索默认近 30 天窗口（M4 时间窗原则，Claude review 8/17）；要搜更早内容请显式传 since（如 2026-01-01T00:00:00 表示全历史）。"},
				"limit":        map[string]string{"type": "integer", "description": "结果数量，默认 10。full_session 模式下为 session 数量上限（≤5）"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径，不传则搜索当前项目"},
			},
		},
	}, s.handleSearchDiscussions)

	s.addTool(MCPTool{
		Name:        "aipm_list_sessions",
		Description: "查看当前活跃的 Agent 会话（公共状态板）：每个 agent 进程（session）正在做什么、最后活跃时间、最近的 user prompt。状态列 = 最近 user prompt（自动登记）或 agent 显式声明（aipm_update_status，优先）。同一 source 下有多个同名进程（如多个 codex）时，用返回的 session_id 配合 aipm_read_discussions(session_id=...) 精准查看某一个。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"source":       map[string]string{"type": "string", "description": "可选: 只看某个来源 (claude-code / codex-cli / cursor / gemini-cli / opencode)。不传则看全部。"},
				"since":        map[string]string{"type": "string", "description": "可选: ISO 时间下限 (例 2026-08-14T12:00:00)。默认最近 24 小时内有活动的 session。"},
				"limit":        map[string]string{"type": "integer", "description": "可选: 返回条数上限，默认 10。"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径，不传则读当前项目。"},
			},
		},
	}, s.handleListSessions)

	s.addTool(MCPTool{
		Name:        "aipm_update_status",
		Description: "声明/更新「我正在做什么」。仅在开始处理一个新问题时调用；琐碎跟进（继续、小修）不需要——user prompt 会自动登记。显式声明优先于自动登记，声明后不会被自动覆盖。其他 agent 通过 aipm_list_sessions 公共查询看到。不传 session_id 时自动归属到本 source 最近活跃的会话；若同 source 有多个活跃会话（如两个 codex），自动归属会失败，需先用 aipm_list_sessions 查自己的 session_id 显式传入。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"status":       map[string]string{"type": "string", "description": "必填: 当前正在处理的问题描述，如「修复 proxy token 认证」"},
				"session_id":   map[string]string{"type": "string", "description": "可选: 目标会话 ID。不传时自动归属到本 agent 来源最近活跃的会话；同 source 有多个活跃会话时必须显式传入（用 aipm_list_sessions 查询）。"},
				"project_path": map[string]string{"type": "string", "description": "可选: 目标项目路径，不传则写当前项目。"},
			},
			Required: []string{"status"},
		},
	}, s.handleUpdateStatus)

	s.addTool(MCPTool{
		Name:        "aipmc_vision",
		Description: "分析 UI 截图并描述实际效果，实现「改代码→截图→看图验证→再改」的自主闭环。\\n\\n使用流程：\\n1. 修改前端/客户端代码后，用 bash 截图（screencapture / adb / xcrun 等）\\n2. 调用此工具，prompt 中附带关键代码片段 + 期望效果 + 重点关注\\n3. 视觉模型返回纯描述性分析（只描述实际看到的，不提供代码修改建议）\\n4. 你自己对比代码预期和实际效果，判断是否继续修改\\n\\nPrompt 公式（经验证效果显著）：\\n- [代码] 贴关键代码片段（5-20行）\\n- [期望] 你预期这段代码会渲染成什么效果\\n- [问题] 具体问视觉模型观察什么（颜色/位置/对齐/分界等）\\n\\n提示：vision 只描述你指定观察的内容，不会主动报告遗漏。如果第一次返回的结果不清晰、与预期不符、或你想确认某个细节，建议再调用一次，换一个更聚焦的问题角度。修改代码后也建议重新截图验证。\\n\\n迭代控制：每轮传 --iteration N，看到轮次数字自然意识到该收手了。截图可先用 sips -Z 1024 压缩加速。\\n\\n视觉模型只是「眼睛」，你（主模型）才是「大脑」——代码决策全部由你负责。",
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
	// B8：两级摘要。level=summary 执行摘要（省 token），level=full 完整分析（默认，向后兼容）。
	level := getStr(args, "level", "full")
	if level != "summary" && level != "full" {
		level = "full"
	}
	args["level"] = level
	briefing, eventIDs := analyze.BuildBriefingLevel(s.ai, buildBriefingGraph(), level)
	report := analyze.RunFullAnalysis()

	// W2（8/13）事件→动作漏斗 surfaced 记录：简报展示了哪些 unconsumed 事件、发给谁。
	// session 为尽力推导（serve 进程无 session 上下文，从 hook 的 discussion_log 取
	// 该 agent 最近会话）——首调/空库为空时以 (src, ts) 兜底对齐。
	if src := mcpClientName(s.clientInfo); len(eventIDs) > 0 {
		sid := recentSessionFor(src)
		u.LogShared("BRIEFING", "events=%d ids=[%s] src=%s session=%s", len(eventIDs), strings.Join(eventIDs, ","), src, sid)
	}

	related := map[string]interface{}{
		"analysis_summary":   report.Summary,
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

// recentSessionFor 尽力取该 agent 当前会话 id：MCP serve 进程没有 session 上下文
// （mcpLogDiscussion 写 discussion_log 时 session 为空），改用 hook 写入的
// discussion_log 中该 agent 最近一条非空 session_id（同一 agent 同时只有一个活跃会话）。
func recentSessionFor(src string) string {
	if src == "" {
		return ""
	}
	db, err := pmdb.Open()
	if err != nil {
		return ""
	}
	defer db.Close()
	var sid string
	if err := db.QueryRow("SELECT session_id FROM discussion_log WHERE source = ? AND session_id != '' ORDER BY created_at DESC LIMIT 1", src).Scan(&sid); err != nil {
		return ""
	}
	return sid
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

// ---- Entity Query Handlers ----

func (s *mcpServer) handleGetTask(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "task_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "task_id 为必填项"}}, IsError: true}
	}
	task, err := store.GetTask(id)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 task 失败: %v", err)}}, IsError: true}
	}
	text := formatTaskDetail(task)
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: task}
}

func (s *mcpServer) handleListTasks(args map[string]interface{}) mcpToolResult {
	status := getStr(args, "status", "")
	planID := getStr(args, "plan_id", "")
	tasks, err := store.ListTasks(status, planID)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 tasks 失败: %v", err)}}, IsError: true}
	}
	if len(tasks) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "未找到匹配的 task。"}}}
	}
	text := fmt.Sprintf("找到 %d 个 task:\n", len(tasks))
	for _, t := range tasks {
		text += fmt.Sprintf("- [%s] %s (status=%s priority=%s phase=%s)\n", t.ID, t.Title, t.Status, t.Priority, t.Phase)
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: map[string]any{"count": len(tasks), "tasks": tasks}}
}

func (s *mcpServer) handleGetCommit(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "commit_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "commit_id 为必填项"}}, IsError: true}
	}
	commit, err := store.GetCommit(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("未找到该 commit（commit_id=%s）。", id)}}, IsError: true}
		}
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 commit 失败: %v", err)}}, IsError: true}
	}
	text := fmt.Sprintf("Commit: %s\n标题: %s\n状态: %s | review: %s | test: %s\nTask: %s\n文件: %s",
		commit["id"], commit["title"], commit["status"], commit["review_status"], commit["test_status"],
		commit["task_id"], commit["files_json"])
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: commit}
}

func (s *mcpServer) handleListCommits(args map[string]interface{}) mcpToolResult {
	taskID := getStr(args, "task_id", "")
	status := getStr(args, "status", "")
	limit := getInt(args, "limit", 50)
	offset := getInt(args, "offset", 0)
	orphan := getBool(args, "orphan", false)
	if limit <= 0 {
		limit = 50
	}

	var commits []map[string]any
	var err error

	if orphan {
		commits, err = store.ListOrphanCommits(limit, offset)
	} else if offset > 0 {
		commits, err = store.ListCommitsWithOffset(status, taskID, "", "", limit, offset)
	} else {
		commits, err = store.ListCommits(status, taskID, "", "", limit)
	}

	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 commits 失败: %v", err)}}, IsError: true}
	}
	if len(commits) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "未找到匹配的 commit。"}}}
	}
	text := fmt.Sprintf("找到 %d 个 commit:\n", len(commits))
	if offset > 0 {
		text = fmt.Sprintf("找到 %d 个 commit (offset=%d):\n", len(commits), offset)
	}
	for _, c := range commits {
		text += fmt.Sprintf("- [%s] %s (status=%s review=%s)\n", c["id"], c["title"], c["status"], c["review_status"])
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: map[string]any{"count": len(commits), "commits": commits}}
}

func (s *mcpServer) handleGetPlan(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "plan_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "plan_id 为必填项"}}, IsError: true}
	}
	plan, err := store.GetPlan(id)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 plan 失败: %v", err)}}, IsError: true}
	}
	text := fmt.Sprintf("Plan: %s\n目标: %s\n状态: %s | 优先级: %s\nRoadmap: %s | Vision: %s",
		plan["title"], plan["goal"], plan["status"], plan["priority"], plan["roadmap_id"], plan["vision_id"])
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: plan}
}

func (s *mcpServer) handleListPlans(args map[string]interface{}) mcpToolResult {
	roadmapID := getStr(args, "roadmap_id", "")
	status := getStr(args, "status", "")
	plans, err := store.ListPlans(roadmapID, status)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 plans 失败: %v", err)}}, IsError: true}
	}
	if len(plans) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "未找到匹配的 plan。"}}}
	}
	text := fmt.Sprintf("找到 %d 个 plan:\n", len(plans))
	for _, p := range plans {
		text += fmt.Sprintf("- [%s] %s (status=%s priority=%s)\n", p["id"], p["title"], p["status"], p["priority"])
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: map[string]any{"count": len(plans), "plans": plans}}
}

func (s *mcpServer) handleGetBug(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "bug_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "bug_id 为必填项"}}, IsError: true}
	}
	bug, err := store.GetBug(id)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 bug 失败: %v", err)}}, IsError: true}
	}
	text := fmt.Sprintf("Bug: %s\n严重级别: %s | 状态: %s\n错误: %s\n根因: %s\n修复: %s",
		bug["title"], bug["severity"], bug["status"], bug["error"], bug["root_cause"], bug["fix"])
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: bug}
}

func (s *mcpServer) handleListBugs(args map[string]interface{}) mcpToolResult {
	status := getStr(args, "status", "")
	severity := getStr(args, "severity", "")
	limit := getInt(args, "limit", 20)
	offset := getInt(args, "offset", 0)
	if limit <= 0 {
		limit = 20
	}
	bugs, err := store.ListBugs(status, severity, "", limit, offset)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 bugs 失败: %v", err)}}, IsError: true}
	}
	if len(bugs) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "未找到匹配的 bug。"}}}
	}
	text := fmt.Sprintf("找到 %d 个 bug:\n", len(bugs))
	for _, b := range bugs {
		text += fmt.Sprintf("- [%s] %s (severity=%s status=%s)\n", b["id"], b["title"], b["severity"], b["status"])
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: map[string]any{"count": len(bugs), "bugs": bugs}}
}

func (s *mcpServer) handleGetDecision(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "decision_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "decision_id 为必填项"}}, IsError: true}
	}
	decision, err := store.GetDecision(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("未找到该 decision（decision_id=%s）。", id)}}, IsError: true}
		}
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 decision 失败: %v", err)}}, IsError: true}
	}
	text := formatDecisionText(decision)
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: decision}
}

// formatDecisionText renders a decision row for MCP output. ScanDecisionRow
// maps the decision_text column to the "decision" key; use a safe string
// getter so a missing key renders "" instead of Go's %!s(<nil>).
func formatDecisionText(d map[string]any) string {
	return fmt.Sprintf("Decision: %s\n状态: %s | 日期: %s\n背景: %s\n决策: %s",
		getStr(d, "title", ""),
		getStr(d, "status", ""),
		getStr(d, "date", ""),
		getStr(d, "background", ""),
		getStr(d, "decision", ""))
}

func (s *mcpServer) handleListDecisions(args map[string]interface{}) mcpToolResult {
	decisions, err := store.ListDecisions()
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询 decisions 失败: %v", err)}}, IsError: true}
	}
	if len(decisions) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "未找到任何 decision。"}}}
	}
	text := fmt.Sprintf("找到 %d 个 decision:\n", len(decisions))
	for _, d := range decisions {
		text += fmt.Sprintf("- [%s] %s (status=%s date=%s)\n", d["id"], d["title"], d["status"], d["date"])
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, RelatedContext: map[string]any{"count": len(decisions), "decisions": decisions}}
}

// formatTaskDetail formats a task map for display, extracting key fields.
func formatTaskDetail(task map[string]any) string {
	title := ""
	if v, ok := task["title"].(string); ok {
		title = v
	}
	status := ""
	if v, ok := task["status"].(string); ok {
		status = v
	}
	priority := ""
	if v, ok := task["priority"].(string); ok {
		priority = v
	}
	phase := ""
	if v, ok := task["phase"].(string); ok {
		phase = v
	}
	planID := ""
	if v, ok := task["plan_id"].(string); ok {
		planID = v
	}
	lastNote := ""
	if v, ok := task["last_note"].(string); ok {
		lastNote = v
	}
	text := fmt.Sprintf("Task: %s\n状态: %s | 优先级: %s | Phase: %s\nPlan: %s", title, status, priority, phase, planID)
	if lastNote != "" {
		text += fmt.Sprintf("\n备注: %s", lastNote)
	}
	return text
}

// ---- P1: Entity Write Handlers ----

func (s *mcpServer) handleCreatePlan(args map[string]interface{}) mcpToolResult {
	title := getStr(args, "title", "")
	roadmapID := getStr(args, "roadmap_id", "")
	if title == "" || roadmapID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "title 和 roadmap_id 为必填项。roadmap_id 可通过 aipm_search_context 搜索获取。"}}, IsError: true}
	}
	goal := getStr(args, "goal", "")
	priority := getStr(args, "priority", "P1")
	status := getStr(args, "status", "draft")
	visionID := getStr(args, "vision_id", "")
	plan, err := store.CreatePlan(title, goal, roadmapID, visionID, priority, status, nil, nil, nil, nil)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 plan 失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Plan 已创建: [%s] %s", plan["id"], title)}},
		RelatedContext: plan,
		Reflection:     fmt.Sprintf("Plan '%s' 已创建（roadmap=%s, status=%s）。下一步：用 aipm_create_task 在此 plan 下创建 task，plan_id=%s", title, roadmapID, status, plan["id"]),
	}
}

func (s *mcpServer) handleCreateRoadmap(args map[string]interface{}) mcpToolResult {
	title := getStr(args, "title", "")
	if title == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "title 为必填项"}}, IsError: true}
	}
	targetDate := getStr(args, "target_date", "")
	visionID := getStr(args, "vision_id", "")
	status := getStr(args, "status", "planned")
	priority := getStr(args, "priority", "P1")
	roadmap, err := store.CreateRoadmap(title, targetDate, visionID, status, priority)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("创建 roadmap 失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Roadmap 已创建: [%s] %s", roadmap["id"], title)}},
		RelatedContext: roadmap,
		Reflection:     fmt.Sprintf("Roadmap '%s' 已创建。下一步：用 aipm_create_plan 在此 roadmap 下创建 plan，roadmap_id=%s", title, roadmap["id"]),
	}
}

func (s *mcpServer) handleUpdateTask(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "task_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "task_id 为必填项"}}, IsError: true}
	}
	stat := getStr(args, "status", "")
	note := getStr(args, "note", "")
	appendNote := note != ""
	result, err := store.UpdateTask("", id, stat, note, true, appendNote)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新 task 失败: %v", err)}}, IsError: true}
	}
	reflection := fmt.Sprintf("Task %s 已更新。", id)
	if stat != "" {
		reflection += fmt.Sprintf(" 状态: %s", stat)
	}
	if note != "" {
		reflection += fmt.Sprintf(" 备注已追加。")
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Task 已更新: %s", id)}},
		RelatedContext: result,
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleUpdateCommit(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "commit_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "commit_id 为必填项"}}, IsError: true}
	}
	payload := map[string]any{}
	if v := getStr(args, "task_id", ""); v != "" {
		// Validate task exists before reassigning
		db, err := pmdb.Open()
		if err == nil {
			var _x int
			if db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", v).Scan(&_x) != nil {
				db.Close()
				return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("task 不存在: %s", v)}}, IsError: true}
			}
			db.Close()
		}
		payload["task_id"] = v
	}
	if v := getStr(args, "status", ""); v != "" {
		payload["status"] = v
	}
	if v := getStr(args, "review_status", ""); v != "" {
		payload["review_status"] = v
	}
	if v := getStr(args, "test_status", ""); v != "" {
		payload["test_status"] = v
	}
	if v := getStr(args, "summary", ""); v != "" {
		payload["summary"] = v
	}
	if len(payload) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "至少需要提供一个要更新的字段（status/review_status/test_status/summary）"}}, IsError: true}
	}
	commit, err := store.UpdateCommit(id, payload)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新 commit 失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Commit 已更新: %s", id)}},
		RelatedContext: commit,
	}
}

func (s *mcpServer) handleUpdateBug(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "bug_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "bug_id 为必填项"}}, IsError: true}
	}
	payload := map[string]any{}
	if v := getStr(args, "status", ""); v != "" {
		payload["status"] = v
	}
	if v := getStr(args, "severity", ""); v != "" {
		payload["severity"] = v
	}
	if v := getStr(args, "fix", ""); v != "" {
		payload["fix"] = v
	}
	if len(payload) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "至少需要提供一个要更新的字段（status/severity/fix）"}}, IsError: true}
	}
	bug, err := store.UpdateBug(id, payload)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新 bug 失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Bug 已更新: %s", id)}},
		RelatedContext: bug,
	}
}

func (s *mcpServer) handleUpdateDecision(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "decision_id", "")
	status := getStr(args, "status", "")
	if id == "" || status == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "decision_id 和 status 为必填项"}}, IsError: true}
	}
	decision, err := store.UpdateDecisionStatus(id, status)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新 decision 失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Decision 状态已更新: %s → %s", id, status)}},
		RelatedContext: decision,
	}
}

func (s *mcpServer) handleUpdatePlan(args map[string]interface{}) mcpToolResult {
	id := getStr(args, "plan_id", "")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "plan_id 为必填项"}}, IsError: true}
	}
	payload := map[string]any{}
	if v := getStr(args, "status", ""); v != "" {
		payload["status"] = v
	}
	if v := getStr(args, "title", ""); v != "" {
		payload["title"] = v
	}
	if v := getStr(args, "goal", ""); v != "" {
		payload["goal"] = v
	}
	if v := getStr(args, "priority", ""); v != "" {
		payload["priority"] = v
	}
	if len(payload) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "至少需要提供一个要更新的字段（status/title/goal/priority）"}}, IsError: true}
	}
	plan, err := store.UpdatePlan(id, payload)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新 plan 失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ Plan 已更新: %s", id)}},
		RelatedContext: plan,
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
	commitHash := getStr(args, "commit_hash", "")
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

	// Dedup: if commit_hash provided, check for existing record first
	if commitHash != "" {
		db, err := pmdb.OpenProject(projectPath)
		if err == nil {
			var existingID, existingTask string
			// Bidirectional prefix match: the hook stores full 40-char hashes
			// while agents may pass a short hash — exact match would miss and
			// create a duplicate row. Empty stored hashes never match.
			db.QueryRow("SELECT id, COALESCE(task_id,'') FROM commits WHERE commit_hash IS NOT NULL AND commit_hash != '' AND (? LIKE commit_hash || '%' OR commit_hash LIKE ? || '%') LIMIT 1",
				commitHash, commitHash).Scan(&existingID, &existingTask)
			db.Close()
			if existingID != "" {
				return s.recordCommitDedup(projectPath, existingID, existingTask, taskID, title, files)
			}
		}
	}

	commit, err := store.CreateCommit(projectPath, title, summary, "", "", branch, commitHash, taskID, "", status, testStatus, reviewStatus, files)
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
		"commit":         commit,
		"task_commits":   len(allCommits),
		"drift_warnings": driftWarnings,
	}

	// Remind agent to link orphan commits to a task.
	var reflection string
	if taskID == "" {
		reflection = fmt.Sprintf("⚠️ Commit '%s' 未关联 task（orphan commit）。"+
			"请立即: (1) aipm_search_context 搜索是否有匹配的已有 task，"+
			"(2) 如无则 aipm_create_task 创建新 task，"+
			"(3) aipm_update_commit(commit_id=\"%s\", task_id=\"<找到的task_id>\") 绑定。", title, commit["id"])
	} else {
		reflection = fmt.Sprintf("Commit '%s' 已记录。下一步：用 aipm_update_task_status(task_id=\"%s\", status=\"done\") 标记 task 完成，或用 aipm_update_commit 更新 review/test 状态。", title, taskID)
		if len(driftWarnings) > 0 {
			reflection += " " + driftWarnings[0]
			reflection += " 建议: 确认这些文件是否应属于当前 task，如是请更新 plan scope。"
		}
		if len(allCommits) == 1 {
			reflection += " 这是此 task 的第一个 commit。"
		}
	}
	// #22: bug 状态卫生 — commit 含修复语义时提示核对 open bug，防止
	// bug 修完但状态仍停在 open（状态与实现脱节）。
	if bugHint := commitBugStatusHint(title, summary); bugHint != "" {
		reflection += " " + bugHint
	}

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: fmt.Sprintf("✅ Commit 已创建: %s [%s]", commit["id"], title)},
		},
		RelatedContext: related,
		Reflection:     reflection,
	}
}

// commitBugStatusHint returns a reminder to sync open-bug status when a
// commit's title/summary carries fix semantics. Empty when nothing to hint.
func commitBugStatusHint(title, summary string) string {
	if !hasFixKeyword(title + " " + summary) {
		return ""
	}
	openBugs, err := store.ListBugs("open", "", "", 5, 0)
	if err != nil {
		return ""
	}
	if len(openBugs) == 0 {
		return ""
	}
	names := make([]string, 0, len(openBugs))
	for _, b := range openBugs {
		names = append(names, fmt.Sprintf("%s(%s)", b["id"], b["title"]))
	}
	return fmt.Sprintf("⚠️ Commit 含修复语义，当前有 %d 个 open bug 未闭环: %s。若本提交修复了某个，请用 aipm_update_bug(bug_id=..., status=\"resolved\", fix=...) 更新。",
		len(openBugs), strings.Join(names, ", "))
}

func hasFixKeyword(s string) bool {
	lower := strings.ToLower(s)
	for _, k := range []string{"fix", "修复", "resolve", "resolved", "close", "closed"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// recordCommitDedup handles a commit that already exists — typically recorded
// by the git hook (which has no task context) moments before the agent calls
// record_commit. The hook row has task_id=”, so this is the one chance to
// bind the orphan to a task: we backfill task_id instead of silently
// returning "already exists" (previously orphaned commits could never be
// linked through record_commit).
func (s *mcpServer) recordCommitDedup(projectPath, existingID, existingTask, taskID, title string, files []string) mcpToolResult {
	if taskID == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
				"Commit 已存在 (commit_hash 去重): %s [%s]。该 commit 未关联 task（hook 记录）。请用 aipm_update_commit(commit_id=\"%s\", task_id=\"<task_id>\") 绑定，或用 aipm_search_context 查找匹配 task。",
				existingID, title, existingID)}},
			RelatedContext: map[string]interface{}{"dedup": true, "existing_id": existingID, "orphan": true},
		}
	}
	if existingTask != "" && existingTask != taskID {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
				"Commit 已存在且已绑定 task %s，与传入 %s 冲突。如需重绑请用 aipm_update_commit(commit_id=\"%s\", task_id=\"<正确task_id>\")。",
				existingTask, taskID, existingID)}},
			IsError: true,
		}
	}

	outcome, err := store.BackfillCommitTask(projectPath, existingID, taskID, title, files)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("回填 task 绑定失败: %v", err)}}, IsError: true}
	}
	switch outcome {
	case store.BackfillBound:
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
				"✅ Commit 已存在，已回填 task 绑定: %s → %s（hook 记录的孤儿 commit 已关联）", existingID, taskID)}},
			RelatedContext: map[string]interface{}{"dedup": true, "existing_id": existingID, "task_id": taskID, "backfilled": true},
		}
	default: // BackfillNoop / BackfillSynced
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
				"Commit 已存在 (commit_hash 去重): %s [%s]，task 绑定一致。", existingID, title)}},
			RelatedContext: map[string]interface{}{"dedup": true, "existing_id": existingID, "task_id": existingTask},
		}
	}
}

func (s *mcpServer) handleRecordCommits(args map[string]interface{}) mcpToolResult {
	taskID := getStr(args, "task_id", "")
	branch := getStr(args, "branch", "main")
	status := getStr(args, "status", "committed")
	projectPath := getStr(args, "project_path", "")
	testStatus := getStr(args, "test_status", "not_run")
	reviewStatus := getStr(args, "review_status", "pending")

	// Parse commits array from raw JSON args
	commitsRaw, ok := args["commits"]
	if !ok {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "commits 为必填项"}}, IsError: true}
	}

	var items []store.BatchCommitItem

	switch v := commitsRaw.(type) {
	case []interface{}:
		for _, ci := range v {
			c, ok := ci.(map[string]interface{})
			if !ok {
				continue
			}
			item := store.BatchCommitItem{
				Title:      getStr(c, "title", ""),
				CommitHash: getStr(c, "commit_hash", ""),
				Summary:    getStr(c, "summary", ""),
			}
			if fs := getStr(c, "files", ""); fs != "" {
				for _, f := range strings.Split(fs, ",") {
					f = strings.TrimSpace(f)
					if f != "" {
						item.Files = append(item.Files, f)
					}
				}
			}
			if item.Title != "" {
				items = append(items, item)
			}
		}
	case string:
		var parsed []map[string]interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			for _, c := range parsed {
				it := store.BatchCommitItem{
					Title:      getStr(c, "title", ""),
					CommitHash: getStr(c, "commit_hash", ""),
					Summary:    getStr(c, "summary", ""),
				}
				if fs := getStr(c, "files", ""); fs != "" {
					for _, f := range strings.Split(fs, ",") {
						f = strings.TrimSpace(f)
						if f != "" {
							it.Files = append(it.Files, f)
						}
					}
				}
				if it.Title != "" {
					items = append(items, it)
				}
			}
		}
	}

	if len(items) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "commits 数组为空或格式错误"}}, IsError: true}
	}

	result, err := store.BatchCreateCommits(projectPath, taskID, branch, status, testStatus, reviewStatus, items)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("批量创建 commit 失败: %v", err)}}, IsError: true}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\u2705 批量记录完成: %d/%d 成功", result.Success, result.Total))
	if result.Failed > 0 {
		sb.WriteString(fmt.Sprintf(", %d 失败", result.Failed))
		for _, d := range result.Details {
			if !d.Success {
				sb.WriteString(fmt.Sprintf("\n  [#%d] %s", d.Index, d.Error))
			}
		}
	}

	reflection := ""
	if taskID == "" {
		reflection = "⚠️ 这些 commit 未关联 task（orphan）。请: (1) aipm_search_context 搜索已有 task (2) 如无则 aipm_create_task 新建 (3) aipm_update_commit 逐个绑定。"
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: sb.String()}},
		RelatedContext: result,
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
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			// Real failure (e.g. database is locked) — surface it, don't
			// misreport as "plan does not exist" (bug-20260805-134225-085427).
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("❌ 查询 plan '%s' 失败: %v。请稍后重试。", planID, err)}},
				IsError: true,
			}
		}
		// sql.ErrNoRows → plan truly doesn't exist.
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
	if plan == nil || plan["id"] == nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("❌ plan '%s' 不存在。\n\n请用 aipm_search_context 搜索已有 plan，或先创建 plan 再创建 task。", planID)}},
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
		"task":        task,
		"plan_title":  u.Str(plan["title"]),
		"plan_status": u.Str(plan["status"]),
	}

	reflection := fmt.Sprintf("Task '%s' 已创建 (plan: %s)。下一步：设置 status=in_progress 开始工作，编码完成后用 aipm_record_commit(task_id=\"%s\") 记录 commit。", title, u.Str(plan["title"]), task["id"])
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

	reflection := fmt.Sprintf("Bug '%s' 已记录 (severity: %s)。下一步：修复后用 aipm_update_bug(bug_id=\"%s\", status=\"resolved\", fix=\"...\") 更新状态，或用 aipm_link_entities 关联修复 commit。", title, severity, bug["id"])
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
		reflection += " 下一步：用 aipm_analyze 检查 scope drift，或用 aipm_catch_up 查看项目最新状态。"
	}
	if status == "in_progress" {
		reflection += fmt.Sprintf(" 下一步：编码完成后用 aipm_record_commit(task_id=\"%s\") 记录 commit。", taskID)
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

func (s *mcpServer) handleFeedbackTriage(args map[string]interface{}) mcpToolResult {
	feedbackID := getStr(args, "feedback_id", "")
	if feedbackID == "" {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "feedback_id 为必填项"}},
			IsError: true,
		}
	}
	note := getStr(args, "note", "")

	if err := MarkFeedbackProcessed(feedbackID); err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("标记失败: %v", err)}},
			IsError: true,
		}
	}

	// 返回剩余未处理数量，帮助 agent 决定是否继续 triage
	feedbacks, _ := ListFeedbacks("")
	unprocessed, inWindow := FeedbackTriageSnapshot(feedbacks, 30)
	suffix := ""
	if note != "" {
		suffix = fmt.Sprintf("（备注: %s）", note)
	}
	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 反馈 #%s 已标记为已处理，30 天复测窗口已启动%s", feedbackID, suffix)}},
		RelatedContext: map[string]any{
			"feedback_id":          feedbackID,
			"note":                 note,
			"remaining_unprocessed": len(unprocessed),
			"in_recheck_window":     inWindow,
		},
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

func (s *mcpServer) handleMarkEventProcessed(args map[string]interface{}) mcpToolResult {
	entityID := getStr(args, "entity_id", "")
	if entityID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "entity_id 为必填项"}}, IsError: true}
	}
	eventType := getStr(args, "event_type", "")
	db, err := pmdb.Open()
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("打开数据库失败: %v", err)}}, IsError: true}
	}
	defer db.Close()
	var count int
	var qerr error
	if eventType != "" {
		qerr = db.QueryRow("SELECT COUNT(*) FROM events WHERE type = ? AND entity_id = ? AND processed_by_agent = 0", eventType, entityID).Scan(&count)
	} else {
		qerr = db.QueryRow("SELECT COUNT(*) FROM events WHERE entity_id = ? AND processed_by_agent = 0", entityID).Scan(&count)
	}
	if qerr != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询事件失败: %v", qerr)}}, IsError: true}
	}
	if count == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "该实体没有未处理事件（可能已标记或不存在）"}}}
	}
	var res sql.Result
	if eventType != "" {
		res, err = db.Exec("UPDATE events SET processed_by_agent = 1 WHERE type = ? AND entity_id = ? AND processed_by_agent = 0", eventType, entityID)
	} else {
		res, err = db.Exec("UPDATE events SET processed_by_agent = 1 WHERE entity_id = ? AND processed_by_agent = 0", entityID)
	}
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("标记失败: %v", err)}}, IsError: true}
	}
	n, _ := res.RowsAffected()
	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 已标记 %d 个事件为已处理（entity=%s type=%s）", n, entityID, eventType)}},
	}
}

func (s *mcpServer) handleAnalyze(args map[string]interface{}) mcpToolResult {
	report := analyze.RunFullAnalysis()
	text := formatAnalyzeDetail(report)

	return mcpToolResult{
		Content: []mcpContent{
			{Type: "text", Text: text},
		},
		RelatedContext: report,
		Reflection:     report.Summary,
	}
}

// formatAnalyzeDetail renders drill-down detail for aipm_analyze (#25):
// duplicates/conflicts carry entity IDs and hit reasons, so the caller can
// jump to aipm_get_task/get_plan instead of staring at a one-line summary.
func formatAnalyzeDetail(r analyze.AnalyzeReport) string {
	var b strings.Builder
	b.WriteString("分析完成: " + r.Summary + "\n\n")

	const cap = 5
	if len(r.Duplicates) > 0 {
		b.WriteString(fmt.Sprintf("### 重复（%d 对）\n", len(r.Duplicates)))
		for _, d := range r.Duplicates[:min(cap, len(r.Duplicates))] {
			b.WriteString(fmt.Sprintf("- **%s** [%s] ≈ **%s** [%s] (%.0f%%)\n", d.Title1, d.ID1, d.Title2, d.ID2, d.Similarity*100))
		}
		if len(r.Duplicates) > cap {
			b.WriteString(fmt.Sprintf("  … 共 %d 对。下钻: aipm_get_task(id) 查看详情\n", len(r.Duplicates)))
		}
		b.WriteString("\n")
	}
	if len(r.Conflicts) > 0 {
		b.WriteString(fmt.Sprintf("### 冲突（%d 条）\n", len(r.Conflicts)))
		for _, c := range r.Conflicts[:min(cap, len(r.Conflicts))] {
			b.WriteString(fmt.Sprintf("- **%s** [%s] 与 **%s** [%s] 冲突（plan %s）: %s\n", c.Title1, c.TaskID1, c.Title2, c.TaskID2, c.PlanID, c.Reason))
		}
		if len(r.Conflicts) > cap {
			b.WriteString(fmt.Sprintf("  … 共 %d 条\n", len(r.Conflicts)))
		}
		b.WriteString("\n")
	}
	if len(r.Drifts) > 0 {
		b.WriteString(fmt.Sprintf("### Scope 漂移（%d 条，最近 50 commit 窗口）\n", len(r.Drifts)))
		for _, d := range r.Drifts[:min(cap, len(r.Drifts))] {
			b.WriteString(fmt.Sprintf("- %s: 文件 %v 超出 plan scope（commit %s）\n", d.CommitTitle, d.OutOfScope, d.CommitID))
		}
		if len(r.Drifts) > cap {
			b.WriteString(fmt.Sprintf("  … 共 %d 条。下钻: aipm_get_commit(id) 或 aipm_get_task(id)\n", len(r.Drifts)))
		}
		b.WriteString("\n")
	}
	if len(r.Blocked) > 0 {
		b.WriteString(fmt.Sprintf("### 阻塞（%d 个，阻塞 %d 天起）\n", len(r.Blocked), r.Blocked[0].DaysBlocked))
		b.WriteString("\n")
	}
	if len(r.Orphans) > 0 {
		b.WriteString(fmt.Sprintf("### 孤儿任务（%d 个）\n", len(r.Orphans)))
		for _, o := range r.Orphans[:min(cap, len(r.Orphans))] {
			b.WriteString(fmt.Sprintf("- **%s** [%s]\n", o.TaskTitle, o.TaskID))
		}
		b.WriteString("\n")
	}
	if len(r.Impacts) > 0 {
		b.WriteString(fmt.Sprintf("### 决策影响（%d 项）\n", len(r.Impacts)))
		for _, imp := range r.Impacts[:min(cap, len(r.Impacts))] {
			b.WriteString(fmt.Sprintf("- Decision **%s** 影响 %d plans, %d tasks\n", imp.DecisionTitle, len(imp.AffectedPlans), len(imp.AffectedTasks)))
		}
		b.WriteString("\n")
	}
	if len(r.CrossTasks) > 0 {
		b.WriteString(fmt.Sprintf("### 跨 task 文件关联（%d 条）\n", len(r.CrossTasks)))
		b.WriteString("\n")
	}
	b.WriteString("[结构化明细见 related_context；下钻: aipm_get_task / aipm_get_plan / aipm_get_commit]\n")
	return b.String()
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
	sessionID := getStr(args, "session_id", "")
	discID := getStr(args, "id", "")
	since := getStr(args, "since", "")
	lastN := getInt(args, "last_n", 0)
	cursor := getStr(args, "cursor", "")
	if lastN <= 0 {
		// store 层默认 15；此处规范化让日志显示实际生效值（8/17 补录）。
		lastN = 15
		args["last_n"] = lastN
	}
	projectPath := getStr(args, "project_path", "")
	full := false
	if v, ok := args["full"].(bool); ok {
		full = v
	} else if v := getStr(args, "full", ""); v == "true" || v == "1" {
		full = true
	}

	if discID != "" {
		// B7：按预览中的 disc-xxx 线索单条展开全文，不拉整个 session。
		full = true
		lastN = 1
		args["full"] = true
		args["last_n"] = 1
	}
	rows, err := store.ReadDiscussions(store.ReadDiscussionsOpts{
		Source:      source,
		SessionID:   sessionID,
		ID:          discID,
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
	if sessionID != "" {
		header.WriteString(fmt.Sprintf(" [session=%s]", sessionID))
	}
	if discID != "" {
		header.WriteString(fmt.Sprintf(" [id=%s 单条全文]", discID))
	}
	if since != "" {
		header.WriteString(fmt.Sprintf(" [since=%s]", since))
	}
	header.WriteString("\n\n")

	text := header.String() + discussion.FormatResults(rows, full)
	reflection := ""
	if len(rows) == 0 {
		if discID != "" {
			reflection = fmt.Sprintf("未找到 id=%s。ID 需为预览输出的 disc-xxx 形式，或检查是否被 substantive 过滤（工具输出类消息不返回）。", discID)
		} else if cursor != "" {
			reflection = "cursor 之后无新讨论。如需扩大范围，不传 cursor 重新调用。"
		} else {
			reflection = "未找到讨论记录。确认 source 拼写或扩大 since 时间窗。"
		}
	} else if discID != "" {
		reflection = fmt.Sprintf("已展开单条全文（id=%s）。", discID)
	} else if !full {
		reflection = "内容为预览（约 200 字）。互读讨论请设 full=true。看某 Agent → source=\"cursor\" + full=true。"
	} else if sessionID != "" {
		reflection = fmt.Sprintf("已返回 session=%s 的全文。", sessionID)
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
	sessionID := getStr(args, "session_id", "")
	typeFilter := getStr(args, "type", "")
	projectPath := getStr(args, "project_path", "")
	mode := getStr(args, "mode", "matches")
	since := getStr(args, "since", "")
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
		results, err = store.ListRecentDiscussions(source, typeFilter, sessionID, projectPath, since, lastN, cursor)
		if err != nil {
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("获取最近讨论失败: %v", err)}},
				IsError: true,
			}
		}
		total = len(results)
	} else {
		// Keyword search mode (existing behavior)
		if since == "" {
			// B3 默认窗口（Claude review 8/17）：不传 since 时若不设默认，
			// agent 仍会扫全历史，「无时间窗盲区」没有真正消失。与
			// list_sessions 的默认窗口哲学对齐，keyword 搜索默认近 30 天。
			since = defaultSearchWindow(time.Now())
			// 写回 args，让 mcpLogSummary 记录生效窗口（8/17 补录）。
			args["since"] = since
		}
		var err error
		results, total, err = s.searchDiscussions(query, source, sessionID, typeFilter, projectPath, since, 1, limit)
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
	if sessionID != "" {
		b.WriteString(fmt.Sprintf(" [session=%s]", sessionID))
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
			content := u.Str(r["content"])
			if query != "" {
				content = discussion.SnippetContent(content, query, 60)
			} else {
				content = discussion.PreviewContent(content, discussion.PreviewRunes)
			}
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
		if source != "" || sessionID != "" || typeFilter != "" {
			reflection += fmt.Sprintf(" 已过滤 source=%s session=%s type=%s。", source, sessionID, typeFilter)
		}
	}

	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: b.String()}},
		// count=实际返回条数：让 [MCP] 日志 n= 提取生效（Claude review 8/17，
		// read 已有 count，search 原本只有 total 导致 n= 永不出现）。
		RelatedContext: map[string]interface{}{"results": results, "total": total, "count": len(results), "mode": mode},
		Reflection:     reflection,
	}
}

// defaultSearchWindow returns the default since lower bound for keyword
// discussion search: 30 days back (Claude review 8/17 — bounded window per
// the M4 time-window principle, instead of unbounded full-history scans).
func defaultSearchWindow(now time.Time) string {
	return now.AddDate(0, 0, -30).Format("2006-01-02T15:04:05")
}

// handleListSessions serves aipm_list_sessions: the public "who is doing what"
// status board across all agent processes (same-source peers included).
func (s *mcpServer) handleListSessions(args map[string]interface{}) mcpToolResult {
	source := getStr(args, "source", "")
	projectPath := getStr(args, "project_path", "")
	since := getStr(args, "since", "")
	limit := getInt(args, "limit", 0)
	if since == "" {
		since = time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05")
	}
	sessions, err := store.ListActiveSessions(projectPath, source, since, limit)
	if err != nil {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("查询活跃会话失败: %v", err)}},
			IsError: true,
		}
	}
	if len(sessions) == 0 {
		return mcpToolResult{
			Content:    []mcpContent{{Type: "text", Text: "(最近 24 小时无活跃 agent 会话)"}},
			Reflection: "无活跃会话。扩大 since 时间窗，或确认项目路径。",
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("活跃 Agent 会话: %d 个\n\n", len(sessions)))
	for _, s := range sessions {
		status := strings.TrimSpace(s.Status)
		if status == "" {
			status = "(未登记状态)"
		}
		b.WriteString(fmt.Sprintf("- [%s] %s\n", s.Source, s.SessionID))
		b.WriteString(fmt.Sprintf("  状态: %s\n", discussion.PreviewContent(status, 120)))
		if s.StatusUpdatedAt != "" {
			b.WriteString(fmt.Sprintf("  状态更新: %s\n", s.StatusUpdatedAt))
		}
		b.WriteString(fmt.Sprintf("  活跃: %s ~ %s (user:%d tool:%d)\n", s.FirstSeen, s.LastSeen, s.UserPromptCount, s.ToolCallCount))
		if len(s.UserPrompts) > 0 {
			b.WriteString(fmt.Sprintf("  最近 prompt: %s\n", discussion.PreviewContent(s.UserPrompts[0], 120)))
		}
		b.WriteString("\n")
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: b.String()}},
		RelatedContext: map[string]interface{}{"sessions": sessions, "count": len(sessions)},
		Reflection:     "用返回的 session_id 配合 aipm_read_discussions(session_id=...) 可精准读某个同名 agent 的讨论。",
	}
}

// handleUpdateStatus serves aipm_update_status: an agent declares what it is
// working on right now. Without an explicit session_id it resolves to the
// caller source's most recently active session (within the last 30 minutes).
func (s *mcpServer) handleUpdateStatus(args map[string]interface{}) mcpToolResult {
	status := getStr(args, "status", "")
	sessionID := getStr(args, "session_id", "")
	projectPath := getStr(args, "project_path", "")
	if status == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "status 为必填项"}}, IsError: true}
	}
	source := mcpClientName(s.clientInfo)
	if source == "" {
		source = "unknown"
	}
	if sessionID == "" {
		since := time.Now().Add(-30 * time.Minute).Format("2006-01-02T15:04:05")
		candidates, err := store.ListActiveSessions(projectPath, source, since, 5)
		if err != nil {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("解析归属会话失败: %v", err)}}, IsError: true}
		}
		if len(candidates) == 0 {
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("未检测到 %s 最近 30 分钟内的活跃会话，请显式传 session_id（用 aipm_list_sessions 查看）", source)}},
				IsError: true,
			}
		}
		if len(candidates) > 1 {
			var ids []string
			for _, c := range candidates {
				ids = append(ids, c.SessionID)
			}
			return mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("检测到多个活跃 %s 会话，无法自动归属，请显式传 session_id: %s", source, strings.Join(ids, " / "))}},
				IsError: true,
			}
		}
		sessionID = candidates[0].SessionID
	}
	if err := store.UpdateAgentStatus(source, sessionID, status, projectPath); err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("更新状态失败: %v", err)}}, IsError: true}
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 状态已更新 [%s][%s]: %s", source, sessionID, status)}},
		RelatedContext: map[string]interface{}{"source": source, "session_id": sessionID},
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
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 讨论已记录 [%s]", res["id"])}},
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

	// Feedback 例行 triage（B9/B13）：未处理列表 + 30 天复测窗口计数
	feedbacks, _ := ListFeedbacks("")
	unprocessed, inWindow := FeedbackTriageSnapshot(feedbacks, 30)
	if len(feedbacks) > 0 {
		text.WriteString("### Feedback 例行 Triage（B9/B13）\n")
		text.WriteString(fmt.Sprintf("未处理反馈 **%d** 条 | 30 天复测窗口内已处理 **%d** 条（复测同类反馈是否消失）\n", len(unprocessed), inWindow))
		for i, f := range unprocessed[:min(10, len(unprocessed))] {
			id, _ := f["id"].(float64)
			label := u.Str(f["label"])
			if label == "" {
				label = "—"
			}
			content := u.TruncateText(u.Str(f["content"]), 120)
			text.WriteString(fmt.Sprintf("%d. #%.0f [%s] %s\n", i+1, id, label, content))
		}
		if len(unprocessed) > 10 {
			text.WriteString(fmt.Sprintf("… 其余 %d 条未列出\n", len(unprocessed)-10))
		}
		if len(unprocessed) > 0 {
			text.WriteString("处理流程：bug 类先用 aipm_record_bug 记录，suggestion 类评估是否入 plan，然后用 aipm_feedback_triage 标记已处理（30 天复测窗口启动）。\n")
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
			"feedback": map[string]any{
				"total":       len(feedbacks),
				"unprocessed": unprocessed,
				"in_window":   inWindow,
			},
		},
		Reflection: fmt.Sprintf("已提供 %d 条 commit 供分析，%d 条启发式建议，%d 条已有线索，feedback 未处理 %d 条", len(commits), len(suggestions), len(threads), len(unprocessed)),
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
		s.handleLine(line)
	}
}

// handleLine processes one JSON-RPC line. A panic inside any tool handler must
// not kill the whole stdio server: recover here and reply with an error.
func (s *mcpServer) handleLine(line string) {
	var msgID interface{}
	defer func() {
		if r := recover(); r != nil {
			u.LogShared("MCP", "panic recovered: %v", r)
			s.sendError(msgID, -32603, "Internal error: "+fmt.Sprint(r))
		}
	}()

	var msg jsonrpcMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		s.sendError(nil, -32700, "Parse error: "+err.Error())
		return
	}

	if msg.JSONRPC != "2.0" {
		return
	}
	msgID = msg.ID

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
func recordMCPError(toolName, src string, args map[string]interface{}, result mcpToolResult) {
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

	// Extract error text
	errText := ""
	for _, c := range result.Content {
		if c.Type == "text" {
			errText += c.Text
		}
	}

	// 观测层不丢：MCP-ERR 日志无条件输出（metrics 计数依赖它）。8/12 修复：
	// 此前 LogShared 在 dedup return 之后，同 entity 重复 ERR 时连日志都不记
	// （实测 201 条 status=ERR 仅 75 条有 MCP-ERR 行 → 126 条缺口）。
	u.LogShared("MCP-ERR", "tool=%s src=%s entity=%s reason=%s error=%s",
		toolName, src, entityID[:min(len(entityID), 16)], classifyMCPErr(errText), u.TruncateStr(errText, 60))

	// INJECT 提示去重保留：同 entity 已有未消费 mcp_error 事件时不重复注入。
	if store.HasUnconsumedEvent("mcp_error", entityID) {
		return
	}

	summary := fmt.Sprintf("工具 %s 失败: %s", toolName, u.TruncateStr(errText, 120))
	store.CreateEvent("mcp_error", toolName, entityID, summary)
}

// classifyMCPErr buckets an MCP error by nature for the metrics layer:
// business_reject = 业务规则拒绝（如 done-gate 无已验证 commit）——agent 行为问题，非系统故障；
// idempotent = 幂等重复（如 commit 已绑定同 task）——无害重复操作；
// system_fault = 其余（DB 锁、解析失败等）——需要人工关注。
func classifyMCPErr(errText string) string {
	switch {
	case strings.Contains(errText, "cannot be marked done"):
		return "business_reject"
	case strings.Contains(errText, "Commit 已存在且已绑定"):
		return "idempotent"
	default:
		return "system_fault"
	}
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
	src := mcpClientName(s.clientInfo)
	name := ""
	if s.clientInfo != nil {
		name = s.clientInfo.Name
	}

	// Log MCP tool usage to discussion_log — MCP tools are invisible to
	// Claude Code hooks, so we log them here for full traceability.
	mcpLogDiscussion(src, call.Name, call.Arguments, result)

	// Also write to shared observability log
	status := "OK"
	if result.IsError {
		status = "ERR"
		// P3: record MCP error as event for next INJECT correction hint
		recordMCPError(call.Name, src, call.Arguments, result)
	}
	summary := mcpLogSummary(call.Name, call.Arguments)
	if rc, ok := result.RelatedContext.(map[string]interface{}); ok {
		if n, ok := rc["count"]; ok {
			switch v := n.(type) {
			case int:
				summary += fmt.Sprintf(" n=%d", v)
			case float64:
				summary += fmt.Sprintf(" n=%d", int(v))
			}
		}
	}
	u.LogShared("MCP", "tool=%s status=%s src=%s name=%s | %s", call.Name, status, src, name, summary)

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
func mcpLogDiscussion(src, toolName string, args map[string]interface{}, result mcpToolResult) {
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

	// Attribute the call to the real MCP client (from initialize clientInfo),
	// not a hardcoded agent — codex/gemini/cursor calls were all misattributed.
	if src == "" {
		src = "unknown"
	}
	store.LogDiscussion("", "assistant", src, summary, metaJSON)
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
		s := "q=" + truncArg(args, "query", 50)
		if tool == "aipm_search_discussions" {
			// 8/17 补录：keyword 模式显示生效 since（handler 已写回默认窗），
			// last_n 模式显示 last_n，query 模式显示 limit（Claude review 8/17
			// 小瑕疵：query 模式恒显示 last_n=0 有误导）。
			s += fmt.Sprintf(" since=%s", strArg(args, "since"))
			if ln := intArg(args, "last_n", 0); ln > 0 {
				s += fmt.Sprintf(" last_n=%d", ln)
			} else {
				s += fmt.Sprintf(" limit=%d", intArg(args, "limit", 10))
			}
		}
		return s
	case "aipm_read_discussions":
		// 8/17 补录：cursor 增量模式是否被使用（去重方案的可观测性前提）。
		c := strArg(args, "cursor")
		if len(c) > 12 {
			c = c[:12] + "..."
		}
		return fmt.Sprintf("src=%s last_n=%d since=%s cursor=%s", strArg(args, "source"), intArg(args, "last_n", 15), strArg(args, "since"), c)
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

func intArg(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return def
}

func truncArg(args map[string]interface{}, key string, max int) string {
	s := strArg(args, key)
	if len(s) > max {
		// 8/12: 裸字节切片会切坏中文 rune 产生非法 UTF-8（见 u.TruncateStr），
		// 回退到 rune 边界。
		for max > 0 && !utf8.RuneStart(s[max]) {
			max--
		}
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
func getBool(m map[string]interface{}, key string, def bool) bool {
	if v, ok := m[key]; ok {
		switch vv := v.(type) {
		case bool:
			return vv
		case string:
			return vv == "true" || vv == "1"
		}
	}
	return def
}

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
