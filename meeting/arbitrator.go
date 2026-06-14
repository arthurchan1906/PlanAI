package meeting

import (
	"fmt"
	"strings"

	"aipmc/ai"
)

// TurnSummary is a simplified turn for arbitration prompts.
type TurnSummary struct {
	SpeakerType string
	SpeakerID   string
	Content     string
	AddressTo   string
}

const arbitratorSystemPrompt = "你是一个会议仲裁者。只输出下一个发言的 agent_id，不要解释。"

// pickNextSpeaker uses AI to choose the next agent when nobody volunteers.
func pickNextSpeaker(summarizer ai.Summarizer, topic, agentRolesContext string, recent []TurnSummary) (string, string, error) {
	if summarizer == nil {
		return "", "", fmt.Errorf("AI not configured: arbitration requires AI endpoint")
	}

	var b strings.Builder
	b.WriteString("你是一个会议仲裁者。当前多 Agent 技术讨论中无人主动发言，你需要选择下一个发言人。\n\n")
	b.WriteString(fmt.Sprintf("## 会议主题\n%s\n\n", topic))
	b.WriteString(fmt.Sprintf("## Agent 角色\n%s\n\n", agentRolesContext))
	b.WriteString("## 最近发言 (按时间顺序)\n")
	for _, t := range recent {
		who := t.SpeakerID
		if t.SpeakerType == "human" {
			who = "PM"
		}
		content := t.Content
		if len(content) > 120 {
			content = content[:120] + "..."
		}
		line := fmt.Sprintf("- [%s] %s: %s", who, who, content)
		if t.AddressTo != "" {
			line += fmt.Sprintf(" (↑ 向 %s 提问)", t.AddressTo)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n## 选择规则\n")
	b.WriteString("1. 角色与当前话题的关联性\n")
	b.WriteString("2. 发言频率均衡 — 不要让一个人连续说太多轮\n")
	b.WriteString("3. 如果有 agent 被 @提问但尚未回应 — 优先选他\n")
	b.WriteString("4. 轮转均衡 — 确保不同视角都被听到\n\n")
	b.WriteString("只输出 agent_id（单个词），不要解释。")

	result, err := summarizer.Summarize(b.String(), arbitratorSystemPrompt)
	if err != nil {
		return "", "", fmt.Errorf("arbitration failed: %w", err)
	}

	result = strings.TrimSpace(result)
	if idx := strings.IndexByte(result, '\n'); idx > 0 {
		result = strings.TrimSpace(result[:idx])
	}

	return result, "AI 仲裁选择", nil
}
