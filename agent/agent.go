package agent

import (
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
	}
}

// Run processes one user input against the session and returns the agent's text response.
// The session is mutated — user input, assistant decisions, and tool results are all appended.
func (a *Agent) Run(s *Session, userInput string) (string, error) {
	if a.llm == nil || !a.llm.Enabled() {
		return "", fmt.Errorf("AI 未配置。请设置 AI_ENDPOINT 和 AI_MODEL 环境变量。")
	}

	// 1. Record user message
	s.Append(Event{Role: "user", Content: userInput})

	// 2. Main loop
	for i := 0; i < a.maxIter; i++ {
		messages := BuildMessages(s)
		toolDefs := BuildToolDefs(a.tools)

		resp, err := a.llm.Chat(messages, toolDefs)
		if err != nil {
			return "", fmt.Errorf("LLM 调用失败: %w", err)
		}

		// 3. Tool calls — execute and loop
		if len(resp.ToolCalls) > 0 {
			a.executeToolCalls(s, resp.ToolCalls)
			continue
		}

		// 4. Text response — done
		if resp.Content != "" {
			s.Append(Event{Role: "assistant", Content: resp.Content})
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
	s.Append(Event{Role: "assistant", ToolCalls: agentCalls})

	// Execute each tool
	for _, c := range calls {
		result := a.execTool(c.Name, c.Args)
		s.Append(Event{
			Role:       "tool",
			ToolCallID: c.ID,
			ToolName:   c.Name,
			ToolResult: result,
		})
	}
}

// execTool runs a single tool by name and returns its output.
func (a *Agent) execTool(name string, args map[string]any) string {
	for _, t := range a.tools {
		if t.Name == name {
			return t.Exec(args, a.workDir)
		}
	}
	// Suggest similar tool names
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
