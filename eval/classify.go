package eval

// EVAL_PIPELINE §3.3 阶段 2：user 意图分类。
// 类型：task（新意图，开新段强制边界）/ dialogue（延续讨论）/ instruction（状态约束变更）/ status（信息告知）。
// 兜底规则优先（≤8 字且含关键词 → 非任务型，省 LLM 调用）；
// 无 LLM 或低置信（<0.6）回退兜底，仍无结论保守并入当前段（dialogue）。

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type IntentType string

const (
	IntentTask        IntentType = "task"
	IntentDialogue    IntentType = "dialogue"
	IntentInstruction IntentType = "instruction"
	IntentStatus      IntentType = "status"
)

// IntentClass 分类结果。
type IntentClass struct {
	Type       IntentType
	Confidence float64
}

// IntentClassifier LLM 分类通道（测试可注入替身；nil 时全走兜底）。
type IntentClassifier interface {
	Classify(userMsg string) (IntentClass, error)
}

// ClassifyIntent 兜底规则 + LLM：低置信回退兜底，最终保守 dialogue。
func ClassifyIntent(userMsg string, llm IntentClassifier) IntentClass {
	if c, ok := ruleBasedIntent(userMsg); ok {
		return c
	}
	if llm != nil {
		if c, err := llm.Classify(userMsg); err == nil && c.Confidence >= 0.6 {
			return c
		}
	}
	if c, ok := ruleBasedIntent(userMsg); ok {
		return c
	}
	// 无 LLM/低置信/分类失败：保守并入当前段，避免误开边界
	return IntentClass{Type: IntentDialogue, Confidence: 0}
}

// ruleBasedIntent 兜底：≤8 字且含会话延续/约束类关键词 → 非任务型。
// 注意：≤8 字但无关键词（如"你是谁"）不判定，交给 LLM；无 LLM 时归 dialogue。
func ruleBasedIntent(msg string) (IntentClass, bool) {
	msg = strings.TrimSpace(msg)
	if utf8.RuneCountInString(msg) <= 8 && containsAny(msg, "继续", "执行", "查看", "推送", "不要", "暂时", "开工", "重启", "接着", "好的", "继续行动") {
		return IntentClass{Type: IntentDialogue, Confidence: 0.8}, true
	}
	return IntentClass{}, false
}

func containsAny(s string, keys ...string) bool {
	for _, k := range keys {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// LLMIntentClassifier 基于 ai.Client.SummarizeJSON（response_format=json_object）。
type LLMIntentClassifier struct {
	Client interface {
		SummarizeJSON(text, instruction string) (string, error)
	}
}

// Classify 调用 LLM 输出 {type, confidence}。
func (c *LLMIntentClassifier) Classify(userMsg string) (IntentClass, error) {
	instruction := "将用户消息分类为：task=新任务/新意图(实施、调研、运维请求)；dialogue=延续讨论(继续/查看/跟进)；instruction=状态或约束变更(不要开干、推送到远程、记录一下)；status=信息告知。只输出 JSON：{\"type\":\"task|dialogue|instruction|status\",\"confidence\":0.0-1.0}"
	out, err := c.Client.SummarizeJSON(userMsg, instruction)
	if err != nil {
		return IntentClass{}, err
	}
	var parsed struct {
		Type       IntentType `json:"type"`
		Confidence float64    `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return IntentClass{}, err
	}
	switch parsed.Type {
	case IntentTask, IntentDialogue, IntentInstruction, IntentStatus:
	default:
		return IntentClass{}, fmt.Errorf("unexpected intent type %q", parsed.Type)
	}
	if parsed.Confidence < 0 || parsed.Confidence > 1 {
		parsed.Confidence = 0
	}
	return IntentClass{Type: parsed.Type, Confidence: parsed.Confidence}, nil
}
