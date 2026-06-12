package agent

import (
	"encoding/json"
	"fmt"
	"strings" 

	"aipmc/ai"
)

// Agent is a minimal coding agent with a tool-using LLM loop.
type Agent struct {
	llm     *ai.Client
	tools   []Tool
	maxIter int
	workDir string

	// Source identifier for hook logging (e.g. "aipmc-chat", "aipmc-web").
	Source string

	// OnEvent is called after each event is appended to the session.
	// sessionID, role, source, content, metadataJSON
	OnEvent func(sessionID, role, source, content, metadataJSON string)

	// CaptureTraces enables recording raw LLM request/response per turn.
	CaptureTraces bool
}

// New creates a new Agent.
// llm must be a configured ai.Client with Chat() support.
// workDir is the project root directory — all file operations are relative to it.
func New(llm *ai.Client, workDir string) *Agent {
	return &Agent{
		llm:     llm,
		tools:   DefaultTools(),
		maxIter: 30,
		workDir: workDir,
		Source:  "aipmc",
	}
}

// Run processes one user input against the session and returns the agent's text response.
// The session is mutated — user input, assistant decisions, and tool results are all appended.
func (a *Agent) Run(s *Session, userInput string) (string, error) {
	if a.llm == nil || !a.llm.Enabled() {
		return "", fmt.Errorf("AI 未配置。请设置 AI_ENDPOINT 和 AI_MODEL 环境变量。")
	}

	// 1. Record user message
	evt := Event{Role: "user", Content: userInput}
	s.Append(evt)
	a.emitEvent(s.ID, evt)

	// 2. Main loop
	for i := 0; i < a.maxIter; i++ {
		messages := BuildMessages(s)
		toolDefs := BuildToolDefs(a.tools)

		resp, err := a.llm.Chat(messages, toolDefs)
		if err != nil {
			return "", fmt.Errorf("LLM 调用失败: %w", err)
		}

			// Trace: capture raw request/response for debugging
			if a.CaptureTraces {
				reqJSON, _ := json.Marshal(messages)
				respJSON, _ := json.Marshal(resp)
				s.AddTrace(TraceTurn{
					Turn:     i,
					Request:  string(reqJSON),
					Response: string(respJSON),
				})
			}

		// 3. Tool calls — execute and loop
		if len(resp.ToolCalls) > 0 {
			a.executeToolCalls(s, resp.ToolCalls)
			continue
		}

		// 4. Text response — done
		if resp.Content != "" {
			evt := Event{Role: "assistant", Content: resp.Content}
			s.Append(evt)
			a.emitEvent(s.ID, evt)
			return resp.Content, nil
		}

		return "", fmt.Errorf("LLM 返回了空响应（既无文本也无工具调用）")
	}

	return "", fmt.Errorf("超出最大迭代次数 (%d)。agent 可能陷入了循环。", a.maxIter)
}

// executeToolCalls records the assistant's tool choice and runs each tool.
func (a *Agent) executeToolCalls(s *Session, calls []ai.ToolCall) {
	// Record assistant's decision to invoke tools
	agentCalls := make([]ToolCall, len(calls))
	for i, c := range calls {
		agentCalls[i] = ToolCall{
			ID:   c.ID,
			Name: c.Name,
			Args: c.Args,
		}
	}
	evt := Event{Role: "assistant", ToolCalls: agentCalls}
	s.Append(evt)
	a.emitEvent(s.ID, evt)

	// Execute each tool
	for _, c := range calls {
		result := a.execTool(c.Name, c.Args)
		tev := Event{
			Role:       "tool",
			ToolCallID: c.ID,
			ToolName:   c.Name,
			ToolResult: result,
		}
		s.Append(tev)
		a.emitEvent(s.ID, tev)
	}
}

// execTool runs a single tool by name and returns its output.
func (a *Agent) execTool(name string, args map[string]any) string {
	for _, t := range a.tools {
		if t.Name == name {
			return t.Exec(args, a.workDir)
		}
	}
	suggestions := []string{}
	for _, t := range a.tools {
		if strings.Contains(t.Name, name) || strings.Contains(name, t.Name) {
			suggestions = append(suggestions, t.Name)
		}
	}
	if len(suggestions) > 0 {
		return fmt.Sprintf("未知工具: %s。可用的相似工具: %s", name, strings.Join(suggestions, ", "))
	}
	return fmt.Sprintf("未知工具: %s。可用工具: read_file, write_file, edit_file, bash", name)
}

// ── Event emission (hook integration) ────────────────────────────────

// emitEvent formats an event for discussion_log and calls OnEvent if set.
func (a *Agent) emitEvent(sessionID string, e Event) {
	if a.OnEvent == nil {
		return
	}

	role := e.Role
	content := ""
	meta := map[string]any{"type": e.Role}

	switch e.Role {
	case "user":
		content = e.Content

	case "assistant":
		if len(e.ToolCalls) > 0 {
			// Format tool calls like existing hooks: 📝 path, 🔧 cmd, etc.
			meta["tool_calls"] = e.ToolCalls
			parts := []string{}
			for _, tc := range e.ToolCalls {
				parts = append(parts, formatToolCall(tc))
			}
			content = strings.Join(parts, "\n")
		} else {
			content = e.Content
		}

	case "tool":
		content = e.ToolResult
		meta["tool_name"] = e.ToolName
		meta["tool_call_id"] = e.ToolCallID
		// Truncate long results for readability
		if len(content) > 500 {
			content = content[:500] + "..."
		}
	}

	if content == "" {
		return
	}

	metaJSON, _ := json.Marshal(meta)
	a.OnEvent(sessionID, role, a.Source, content, string(metaJSON))
}

// formatToolCall returns a human-readable description of a tool call,
// using the same icon convention as the external agent hooks.
func formatToolCall(tc ToolCall) string {
	switch tc.Name {
	case "read_file":
		fp := getStr(tc.Args, "file_path")
		if fp != "" {
			return "👁 " + fp
		}
	case "write_file":
		fp := getStr(tc.Args, "file_path")
		if fp != "" {
			return "🆕 " + fp
		}
	case "edit_file":
		fp := getStr(tc.Args, "file_path")
		oldStr := getStr(tc.Args, "old_string")
		newStr := getStr(tc.Args, "new_string")
		if fp != "" {
			s := "📝 " + fp
			if oldStr != "" {
				s += "\n- " + strings.TrimSpace(oldStr)
			}
			if newStr != "" {
				s += "\n+ " + strings.TrimSpace(newStr)
			}
			return s
		}
	case "bash":
		cmd := getStr(tc.Args, "command")
		if cmd != "" {
			preview := cmd
			if len(preview) > 150 {
				preview = preview[:150] + "..."
			}
			return "🔧 " + preview
		}
	}
	return "🛠 " + tc.Name
}
