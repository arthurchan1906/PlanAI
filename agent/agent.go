package agent

import (
	"encoding/json"

	"fmt"

	"strings"

	"aipmc/ai"
	"aipmc/u"
)

// Agent is a minimal coding agent with a tool-using LLM loop.

type Agent struct {
	llm *ai.Client

	tools []Tool

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

		llm: llm,

		tools: DefaultTools(),

		maxIter: 30,

		workDir: workDir,

		Source: "aipmc",
	}

}

// Run processes one user input against the session and returns the agent's text response.

func (a *Agent) Run(s *Session, userInput string) (string, error) {

	return a.runSession(s, userInput, nil)

}

func (a *Agent) runSession(s *Session, userInput string, cb *StreamCallbacks) (string, error) {

	if a.llm == nil || !a.llm.Enabled() {

		return "", fmt.Errorf("AI 未配置。请设置 AI_ENDPOINT 和 AI_MODEL 环境变量。")

	}

	evt := Event{Role: "user", Content: userInput}

	s.Append(evt)

	a.emitEvent(s.ID, evt)

	for i := 0; i < a.maxIter; i++ {

		messages := BuildMessages(s)

		toolDefs := BuildToolDefs(a.tools)

		var resp *ai.ChatResponse

		var err error

		if cb != nil && cb.OnToken != nil {

			resp, err = a.llm.ChatStream(messages, toolDefs, cb.OnToken)

		} else {

			resp, err = a.llm.Chat(messages, toolDefs)

		}

		if err != nil {

			return "", fmt.Errorf("LLM 调用失败: %w", err)

		}

		if a.CaptureTraces {

			reqJSON, _ := json.Marshal(messages)

			respJSON, _ := json.Marshal(resp)

			s.AddTrace(TraceTurn{

				Turn: i,

				Request: string(reqJSON),

				Response: string(respJSON),
			})

		}

		if len(resp.ToolCalls) > 0 {

			a.executeToolCalls(s, resp.ToolCalls, cb)

			continue

		}

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

func (a *Agent) executeToolCalls(s *Session, calls []ai.ToolCall, cb *StreamCallbacks) {

	agentCalls := make([]ToolCall, len(calls))

	for i, c := range calls {

		agentCalls[i] = ToolCall{

			ID: c.ID,

			Name: c.Name,

			Args: c.Args,
		}

		if cb != nil && cb.OnToolStart != nil {

			cb.OnToolStart(c.ID, c.Name, c.Args)

		}

	}

	evt := Event{Role: "assistant", ToolCalls: agentCalls}

	s.Append(evt)

	a.emitEvent(s.ID, evt)

	for _, c := range calls {

		result := a.execTool(c.Name, c.Args)

		if cb != nil && cb.OnToolResult != nil {

			cb.OnToolResult(c.ID, c.Name, result)

		}

		tev := Event{

			Role: "tool",

			ToolCallID: c.ID,

			ToolName: c.Name,

			ToolResult: result,
		}

		s.Append(tev)

		a.emitEvent(s.ID, tev)

	}

}

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

			preview := u.TruncateStr(cmd, 150)

			return "🔧 " + preview

		}

	}

	return "🛠 " + tc.Name

}
