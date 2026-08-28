package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

func registerTraceContext(s *mcpServer) {
	s.addTool(MCPTool{
		Name:        "aipm_trace_context",
		Description: "查询实体间的关联关系。数据来源: graph_edges 表(系统自动计算的文件/会话关联) + FK 主关联(commit→task→plan→roadmap)。支持 out/in/both 方向 + min_weight 过滤 + limit 分页。适用场景: 追溯 task↔commit↔session 关联链、验证 link_entities 创建的关联是否生效。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"from_type":  map[string]string{"type": "string", "description": "起点类型: session/commit/task/plan/decision/bug/idea"},
				"from_id":    map[string]string{"type": "string", "description": "起点 ID"},
				"direction":  map[string]string{"type": "string", "description": "方向: out/in/both"},
				"min_weight": map[string]string{"type": "number", "description": "最小权重，默认 0"},
				"limit":      map[string]string{"type": "number", "description": "最多返回边数，默认 50"},
			},
			Required: []string{"from_type", "from_id"},
		},
	}, s.handleTraceContext)
}

func (s *mcpServer) handleTraceContext(args map[string]interface{}) mcpToolResult {
	fromType := getStr(args, "from_type", "")
	fromID := getStr(args, "from_id", "")
	direction := getStr(args, "direction", "both")
	minWeight := getFloat(args, "min_weight", 0)
	limit := int(getFloat(args, "limit", 50))
	if limit <= 0 {
		limit = 200
	}

	allowed := map[string]bool{"session": true, "commit": true, "task": true, "plan": true, "decision": true, "bug": true, "idea": true}
	if fromType == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "缺少必填参数 from_type（起点类型: session/commit/task/plan/decision/bug/idea）"}}, IsError: true}
	}
	if fromID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "缺少必填参数 from_id（起点 ID）"}}, IsError: true}
	}
	if !allowed[fromType] {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("from_type 无效: %s（允许: session/commit/task/plan/decision/bug/idea）", fromType)}}, IsError: true}
	}
	if direction != "in" && direction != "out" && direction != "both" {
		direction = "both"
	}

	jsonStr, err := store.TraceContextJSON(fromType, fromID, direction, minWeight, limit)
	if err != nil {
		u.LogShared("MCP", "tool=aipm_trace_context status=ERR src=%s err=%v", mcpClientName(s.clientInfo), err)
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("图查询失败: %v", err)}}, IsError: true}
	}

	u.LogShared("MCP", "tool=aipm_trace_context status=OK src=%s from=%s/%s dir=%s", mcpClientName(s.clientInfo), fromType, u.Prefix(fromID, 12), direction)
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: jsonStr}}}
}

func getFloat(m map[string]interface{}, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		case int:
			return float64(n)
		}
	}
	return def
}

func buildBriefingGraph() string {
	// Query graph_edges directly — session_summaries uses different session IDs
	sessions, err := store.ListSessionsWithEdges(3)
	if err != nil || len(sessions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Graph\n\n")
	for _, sid := range sessions {
		tr, err := store.TraceContext("session", sid, "out", 0.3, 20)
		if err != nil || tr.Summary.TotalEdges == 0 {
			continue
		}
		prefix := sid
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		sb.WriteString(fmt.Sprintf("- session %s: %d edges (ft=%d, ss=%d)\n",
			prefix, tr.Summary.TotalEdges,
			tr.Summary.ByEdgeType["file_touch"],
			tr.Summary.ByEdgeType["same_session"]))
	}
	if sb.Len() == len("\n## Graph\n\n") {
		return ""
	}
	return sb.String()
}
