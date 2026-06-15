package mcp

import (
	"fmt"
	"strings"

	"aipmc/meeting"
	"aipmc/store"
	"aipmc/u"
)

// registerAgentTools adds assignment MCP tools (meeting turn/wait tools removed in v1).
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

func (s *mcpServer) handleConfirmAttendance(args map[string]interface{}) mcpToolResult {
	meetingID := getStr(args, "meeting_id", "")
	agentID := getStr(args, "agent_id", "")
	if meetingID == "" || agentID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 meeting_id 和 agent_id"}}, IsError: true}
	}
	result, err := store.ConfirmMeetingAttendance(meetingID, agentID)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("确认参会失败: %v", err)}}, IsError: true}
	}
	store.RecordAudit("agent", agentID, "confirm_attendance", "meeting", meetingID, "Agent confirmed attendance")
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: "✅ 已确认参会"}},
		RelatedContext: result,
	}
}

func (s *mcpServer) handleGetMeetingTurn(args map[string]interface{}) mcpToolResult {
	roomID := getStr(args, "room_id", "")
	turnID := getStr(args, "turn_id", "")
	if roomID == "" || turnID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 room_id 和 turn_id"}}, IsError: true}
	}
	agentID := getStr(args, "agent_id", "")
	sinceTurn := getInt(args, "since_turn", 0)
	room, err := store.GetMeetingRoom(roomID)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("会议不存在: %v", err)}}, IsError: true}
	}
	turn, err := store.GetMeetingTurn(turnID)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("轮次不存在: %v", err)}}, IsError: true}
	}
	currentTurnNum := 0
	if tn, ok := turn["turn_number"].(string); ok {
		fmt.Sscanf(tn, "%d", &currentTurnNum)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## 会议: %s\n\n", u.Str(room["title"])))
	b.WriteString(fmt.Sprintf("### 会议主题\n%s\n\n", u.Str(room["topic"])))
	b.WriteString(fmt.Sprintf("### 会议模式\n%s\n\n", u.Str(room["meeting_mode"])))
	if roles := u.Str(room["agent_roles_context"]); roles != "" {
		b.WriteString(fmt.Sprintf("### 参会角色\n%s\n\n", roles))
	}
	allTurns, _ := store.ListMeetingTurns(roomID)
	incremental := sinceTurn > 0
	b.WriteString(fmt.Sprintf("### PM/仲裁对你的提问 (Turn %d)\n%s\n\n", currentTurnNum, u.Str(turn["question"])))
	b.WriteString("### 之前的发言")
	if incremental {
		b.WriteString(fmt.Sprintf(" (Turn %d-%d 新内容)", sinceTurn+1, currentTurnNum-1))
	}
	b.WriteString("\n")
	hasPrevious := false
	for _, t := range allTurns {
		turnNum := 0
		if tn, ok := t["turn_number"].(string); ok {
			fmt.Sscanf(tn, "%d", &turnNum)
		}
		if u.Str(t["id"]) == turnID {
			break
		}
		if incremental && turnNum <= sinceTurn {
			continue
		}
		hasPrevious = true
		sp := "PM"
		if u.Str(t["speaker_type"]) == "agent" {
			sp = u.Str(t["speaker_id"])
		}
		txt := u.Str(t["question"])
		if r := u.Str(t["response"]); r != "" {
			txt = r
		}
		line := fmt.Sprintf("- [T%d] %s: %s", turnNum, sp, txt)
		if addr := u.Str(t["address_to"]); addr != "" {
			line += fmt.Sprintf(" (→ %s)", addr)
		}
		if rp := u.Str(t["reply_to"]); rp != "" {
			line += " (@reply)"
		}
		b.WriteString(line + "\n")
	}
	if !hasPrevious {
		if incremental {
			b.WriteString("(没有新发言)\n")
		} else {
			b.WriteString("(你是第一位发言者)\n")
		}
	}
	if agentID != "" {
		store.UpdateLastSeenTurn(roomID, agentID, currentTurnNum)
		store.MarkTurnProcessing(turnID)
	}
	reflection := "请在理解上下文后，使用 aipm_respond_in_meeting 提交你的意见。"
	if incremental {
		reflection += fmt.Sprintf(" (增量同步: Turn %d-%d)", sinceTurn+1, currentTurnNum-1)
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: b.String()}},
		RelatedContext: map[string]interface{}{"room": room, "turn": turn, "previous_turns": allTurns, "incremental": incremental},
		Reflection:     reflection,
	}
}

func (s *mcpServer) handleRespondInMeeting(args map[string]interface{}) mcpToolResult {
	turnID := getStr(args, "turn_id", "")
	response := getStr(args, "response", "")
	if turnID == "" || response == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 turn_id 和 response"}}, IsError: true}
	}
	turn, err := store.RespondMeetingTurn(turnID, response)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("回应失败: %v", err)}}, IsError: true}
	}
	store.RecordAudit("agent", u.Str(turn["speaker_id"]), "respond_meeting", "meeting_turn", turnID, "Agent responded in meeting")
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: "✅ 回应已提交"}},
		RelatedContext: map[string]interface{}{"turn": turn},
		Reflection:     "你的意见已记录。等待 PM 的下一个指示或仲裁。",
	}
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

func (s *mcpServer) handleSpeakInMeeting(args map[string]interface{}) mcpToolResult {
	roomID := getStr(args, "room_id", "")
	agentID := getStr(args, "agent_id", "")
	content := getStr(args, "content", "")
	replyTo := getStr(args, "reply_to", "")
	addressTo := getStr(args, "address_to", "")
	if roomID == "" || agentID == "" || content == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 room_id, agent_id, content"}}, IsError: true}
	}
	turn, err := meeting.AgentSpeak(roomID, agentID, content, replyTo, addressTo)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("发言失败: %v", err)}}, IsError: true}
	}
	id := u.Str(turn["id"])
	store.RecordAudit("agent", agentID, "speak_meeting", "meeting_turn", id, "Agent spoke voluntarily in meeting")
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: "✅ 你的发言已提交到会议"}},
		RelatedContext: map[string]interface{}{"meeting_id": roomID, "turn_id": id, "reply_to": replyTo, "address_to": addressTo},
		Reflection:     "你的发言已记录。如需补充，可以再次调用 aipm_speak_in_meeting。",
	}
}

func (s *mcpServer) handleArbitrateNext(args map[string]interface{}) mcpToolResult {
	roomID := getStr(args, "room_id", "")
	if roomID == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "请提供 room_id"}}, IsError: true}
	}
	result, err := meeting.ArbitrateNext(s.ai, roomID)
	if err != nil {
		return mcpToolResult{
			Content:    []mcpContent{{Type: "text", Text: fmt.Sprintf("仲裁失败: %v。请 PM 手动点名。", err)}},
			IsError:    true,
			Reflection: "配置 AI_ENDPOINT 后可使用自动仲裁功能。",
		}
	}
	id := ""
	if result.Turn != nil {
		id = u.Str(result.Turn["id"])
	}
	return mcpToolResult{
		Content:        []mcpContent{{Type: "text", Text: fmt.Sprintf("🔮 AI 仲裁: 下一个发言人是 **%s** (%s)", result.NextAgent, result.Reason)}},
		RelatedContext: map[string]interface{}{"next_agent": result.NextAgent, "reason": result.Reason, "turn_id": id},
		Reflection:     fmt.Sprintf("%s 被选中发言。请在 briefing 中查看新 turn。", result.NextAgent),
	}
}
