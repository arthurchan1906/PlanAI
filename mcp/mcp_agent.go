package mcp

import (
	"fmt"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// registerAgentTools adds assignment MCP tools.
func (s *mcpServer) registerAgentTools() {
	s.addTool(MCPTool{
		Name:        "aipm_get_my_assignments",
		Description: "获取分配给当前 Agent 的任务清单。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"agent_id": map[string]string{"type": "string", "description": "Agent ID"},
			},
			Required: []string{"agent_id"},
		},
	}, s.handleGetMyAssignments)

	s.addTool(MCPTool{
		Name:        "aipm_claim_assignment",
		Description: "认领一个分配的任务。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"assignment_id": map[string]string{"type": "string", "description": "分配 ID"},
			},
			Required: []string{"assignment_id"},
		},
	}, s.handleClaimAssignment)

	s.addTool(MCPTool{
		Name:        "aipm_complete_assignment",
		Description: "标记分配任务完成，附带结果摘要。",
		InputSchema: MCPInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"assignment_id": map[string]string{"type": "string", "description": "分配 ID"},
				"summary":       map[string]string{"type": "string", "description": "完成结果摘要"},
			},
			Required: []string{"assignment_id"},
		},
	}, s.handleCompleteAssignment)
}

func (s *mcpServer) handleGetMyAssignments(args map[string]interface{}) mcpToolResult {
	agentID := getStr(args, "agent_id", "")
	if agentID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 agent_id"}}, IsError: true}
	}
	assignments, err := store.ListAssignments(agentID, "")
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("获取分配失败: %v", err)}}, IsError: true}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## 你的分配清单 (%d 项)\n\n", len(assignments)))
	for _, a := range assignments {
		b.WriteString(fmt.Sprintf("- [%s] **%s** (%s)", u.Str(a["id"]), u.Str(a["role"]), u.Str(a["status"])))
		if tid := u.Str(a["task_id"]); tid != "" {
			b.WriteString(fmt.Sprintf(" — task: %s", tid))
		}
		b.WriteString(fmt.Sprintf("\n  scope: %s\n", u.Str(a["scope"])))
	}
	reflection := ""
	pending := 0
	for _, a := range assignments {
		if u.Str(a["status"]) == "assigned" {
			pending++
		}
	}
	if pending > 0 {
		reflection = fmt.Sprintf("你有 %d 个待认领的分配。使用 aipm_claim_assignment 认领后开始工作。", pending)
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: b.String()}},
		RelatedContext: map[string]interface{}{"assignments": assignments},
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleClaimAssignment(args map[string]interface{}) mcpToolResult {
	assignmentID := getStr(args, "assignment_id", "")
	if assignmentID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 assignment_id"}}, IsError: true}
	}
	a, err := store.UpdateAssignment(assignmentID, map[string]any{"status": "in_progress", "claimed": true})
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("认领失败: %v", err)}}, IsError: true}
	}
	store.RecordAudit("agent", u.Str(a["agent_id"]), "claim_assignment", "assignment", assignmentID, "Agent claimed assignment")
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 已认领: %s (%s)", u.Str(a["role"]), u.Str(a["scope"]))}},
		RelatedContext: map[string]interface{}{"assignment": a},
	}
}

func (s *mcpServer) handleCompleteAssignment(args map[string]interface{}) mcpToolResult {
	assignmentID := getStr(args, "assignment_id", "")
	summary := getStr(args, "summary", "")
	if assignmentID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 assignment_id"}}, IsError: true}
	}
	payload := map[string]any{"status": "done", "completed": true}
	if summary != "" {
		payload["scope"] = summary
	}
	a, err := store.UpdateAssignment(assignmentID, payload)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("完成标记失败: %v", err)}}, IsError: true}
	}
	store.RecordAudit("agent", u.Str(a["agent_id"]), "complete_assignment", "assignment", assignmentID, "Agent completed assignment")
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("✅ 分配已完成: %s", u.Str(a["role"]))}},
		RelatedContext: map[string]interface{}{"assignment": a},
		Reflection:     "请确认所有相关工作已通过 aipm_record_commit 记录。",
	}
}
