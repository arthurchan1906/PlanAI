package agent

import (
	"encoding/json"

	"aipmc/ai"
)

// BuildMessages converts session events into OpenAI-compatible chat messages.
// The system prompt is prepended as the first message, followed by all session events.
func BuildMessages(s *Session) []ai.ChatMessage {
	messages := []ai.ChatMessage{
		{Role: "system", Content: systemPrompt()},
	}

	for _, evt := range s.Events {
		switch evt.Role {
		case "user":
			messages = append(messages, ai.ChatMessage{
				Role:    "user",
				Content: evt.Content,
			})

		case "assistant":
			if len(evt.ToolCalls) > 0 {
				// Assistant chose to invoke tools
				tcMsgs := make([]ai.ToolCallMsg, len(evt.ToolCalls))
				for i, tc := range evt.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Args)
					tcMsgs[i] = ai.ToolCallMsg{
						ID:   tc.ID,
						Type: "function",
						Function: ai.ToolCallFunction{
							Name:      tc.Name,
							Arguments: string(argsJSON),
						},
					}
				}
				messages = append(messages, ai.ChatMessage{
					Role:      "assistant",
					ToolCalls: tcMsgs,
				})
			} else {
				// Assistant text response
				messages = append(messages, ai.ChatMessage{
					Role:    "assistant",
					Content: evt.Content,
				})
			}

		case "tool":
			messages = append(messages, ai.ChatMessage{
				Role:       "tool",
				Content:    evt.ToolResult,
				ToolCallID: evt.ToolCallID,
				Name:       evt.ToolName,
			})
		}
	}

	return messages
}

// BuildToolDefs converts agent Tools into OpenAI tool definitions.
func BuildToolDefs(tools []Tool) []ai.ToolDef {
	defs := make([]ai.ToolDef, len(tools))
	for i, t := range tools {
		defs[i] = ai.ToolDef{
			Type: "function",
			Function: ai.ToolDefFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		}
	}
	return defs
}

// systemPrompt returns the minimal coding agent system prompt.
func systemPrompt() string {
	return `你是一个编程助手，工作在用户的项目目录中。你可以阅读代码、修改文件、运行命令。

## 可用工具
- read_file  — 读取文件内容。修改文件前务必先读取。
- write_file — 创建或覆盖文件。用于新建文件或完全重写。
- edit_file  — 精准替换文件中的文本。old_string 必须唯一匹配。
- bash       — 执行 shell 命令。用于编译、测试、git、搜索代码等。

## 工作方式
1. 修改代码前先 read_file 理解现有逻辑
2. 用 edit_file 做精准修改（比 write_file 更安全）
3. 修改后用 bash 跑编译或测试验证
4. 不确定时用 bash("grep -r ...") 搜索代码库
5. 每次操作后简洁说明做了什么

## 重要规则
- 不要猜测文件内容，用工具读取
- edit_file 的 old_string 必须与文件内容完全一致（包括缩进和空行）
- 一次只做一个小的、可验证的改动
- 如果 old_string 匹配失败，用 read_file 确认文件当前内容后重试`
}
