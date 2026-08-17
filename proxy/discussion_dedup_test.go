package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// 模拟 read_discussions 返回的真实格式（FormatResults）。
const blockA = "disc-20260817-130000-abcdef 2026-08-17T13:00:00 [assistant][claude-code][sid=s1]\n第一段讨论内容\n\n"
const blockB = "disc-20260817-130001-123456 2026-08-17T13:00:01 [user][codex-cli][sid=s2]\n第二段讨论内容\n\n"

// 幂等性（Claude review 8/17 第一条约束）：占位符不再匹配 disc-* 块起始，
// 二次处理必须零改写，保证 deepseek prefix cache 稳定。
func TestDedupeTextIdempotent(t *testing.T) {
	text := blockA + blockA + blockB
	once, blocks, saved := dedupeText(text)
	if blocks != 1 || saved <= 0 {
		t.Fatalf("first pass: blocks=%d saved=%d, want 1 / >0", blocks, saved)
	}
	twice, blocks2, saved2 := dedupeText(once)
	if blocks2 != 0 || saved2 != 0 {
		t.Fatalf("second pass must be no-op (idempotent): blocks=%d saved=%d", blocks2, saved2)
	}
	if twice != once {
		t.Fatalf("second pass changed body:\n%s\n---\n%s", twice, once)
	}
	// 首次出现保留原文，重复出现替换为占位符（含 id 锚点）。
	if !strings.Contains(once, "第一段讨论内容") {
		t.Errorf("first occurrence must be kept verbatim:\n%s", once)
	}
	if !strings.Contains(once, "[disc-20260817-130000-abcdef 已在上文出现，内容省略（如需内容可单条重读）]") {
		t.Errorf("duplicate must become placeholder with id anchor:\n%s", once)
	}
	if strings.Count(once, "第一段讨论内容") != 1 {
		t.Errorf("content must appear exactly once:\n%s", once)
	}
}

// 非 discussion 文本（无 disc- 行首 id）必须原样返回。
func TestDedupeTextLeavesNonDiscussionAlone(t *testing.T) {
	text := "普通工具输出\n{\"count\": 3}\n提到了 disc-20260817-130000-abcdef 但不在行首\n"
	out, blocks, saved := dedupeText(text)
	if blocks != 0 || saved != 0 || out != text {
		t.Fatalf("non-discussion text changed: blocks=%d saved=%d\n%s", blocks, saved, out)
	}
}

// 漏洞 A 修复验证（Claude review 8/17）：最小编辑——手工构造原始 body，
// 去重后非替换点字节必须完全一致（顶层 key 顺序、数字 1.0、原始 <测试> 不转义）。
func TestDedupeMinimalEditFidelity(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","stream":true,"input":[` +
		`{"type":"function_call_output","call_id":"c1","output":"讨论记录: 2 条\n\ndisc-20260817-130000-abcdef 2026-08-17T13:00:00 [assistant][claude-code][sid=s1]\n第一段讨论内容\n\n"},` +
		`{"type":"function_call_output","call_id":"c2","output":"讨论记录: 2 条\n\ndisc-20260817-130000-abcdef 2026-08-17T13:00:00 [assistant][claude-code][sid=s1]\n第一段讨论内容\n\n"}],` +
		`"meta":{"count":1.0,"tags":["<测试> 1.0"]}}`)
	out, blocks, saved := DedupeDiscussionContent(body, "codex")
	if blocks != 1 || saved <= 0 {
		t.Fatalf("blocks=%d saved=%d, want 1 / >0", blocks, saved)
	}
	s := string(out)
	for _, prefix := range []string{
		`{"model":"deepseek-v4-flash","stream":true,"input":[`,
		`{"type":"function_call_output","call_id":"c1","output":"讨论记录: 2 条`,
		`{"type":"function_call_output","call_id":"c2","output":"讨论记录: 2 条`,
		`"meta":{"count":1.0,"tags":["<测试> 1.0"]}}`,
	} {
		if !strings.Contains(s, prefix) {
			t.Errorf("byte fidelity broken, missing %q\n%s", prefix, s)
		}
	}
	if strings.Contains(s, `\u003c`) {
		t.Errorf("HTML escaping introduced (<测试> must stay raw):\n%s", s)
	}
	if strings.Count(s, "第一段讨论内容") != 1 {
		t.Errorf("content must appear exactly once:\n%s", s)
	}
	if !strings.Contains(s, "[disc-20260817-130000-abcdef 已在上文出现") {
		t.Errorf("duplicate must become placeholder:\n%s", s)
	}
	out2, blocks2, _ := DedupeDiscussionContent(out, "codex")
	if blocks2 != 0 || string(out2) != string(out) {
		t.Fatalf("second pass must be no-op: blocks=%d", blocks2)
	}
}

// codex Responses 格式：input 数组里的 function_call_output。
func TestDedupeCodexResponses(t *testing.T) {
	payload := map[string]any{
		"model": "deepseek-v4-flash",
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "看看大家在聊什么"}}},
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "讨论记录: 2 条\n\n" + blockA + blockB},
			map[string]any{"type": "function_call_output", "call_id": "call_2", "output": "讨论记录: 2 条\n\n" + blockA + blockB},
			// assistant 消息里引用 disc id 不应被处理（识别边界）。
			map[string]any{"type": "message", "role": "assistant", "content": "参考 disc-20260817-130000-abcdef 的结论"},
		},
	}
	body, _ := json.Marshal(payload)
	out, blocks, saved := DedupeDiscussionContent(body, "codex")
	// 两个 function_call_output 各含 discA+discB：第二次出现时两者都占位。
	if blocks != 2 || saved <= 0 {
		t.Fatalf("codex: blocks=%d saved=%d, want 2 / >0", blocks, saved)
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	input := raw["input"].([]any)
	if !strings.Contains(input[1].(map[string]any)["output"].(string), "第一段讨论内容") {
		t.Error("first function_call_output must keep full content")
	}
	second := input[2].(map[string]any)["output"].(string)
	if !strings.Contains(second, "已在上文出现，内容省略") {
		t.Errorf("second output must contain placeholder, got:\n%s", second)
	}
	// 幂等：再跑一次零改写。
	out2, blocks2, _ := DedupeDiscussionContent(out, "codex")
	if blocks2 != 0 || string(out2) != string(out) {
		t.Fatalf("codex second pass must be no-op: blocks=%d", blocks2)
	}
}

// anthropic 格式：messages 里 tool_result content 为 string。
func TestDedupeAnthropicStringContent(t *testing.T) {
	payload := map[string]any{
		"model": "claude-3-5-sonnet",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": "讨论记录: 2 条\n\n" + blockA + blockB},
				map[string]any{"type": "tool_result", "tool_use_id": "tu_2", "content": "讨论记录: 2 条\n\n" + blockA + blockB},
			}},
		},
	}
	body, _ := json.Marshal(payload)
	out, blocks, saved := DedupeDiscussionContent(body, "claude")
	if blocks != 2 || saved <= 0 {
		t.Fatalf("anthropic: blocks=%d saved=%d, want 2 / >0", blocks, saved)
	}
	var raw map[string]any
	json.Unmarshal(out, &raw)
	messages := raw["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	second := content[1].(map[string]any)["content"].(string)
	if !strings.Contains(second, "已在上文出现") {
		t.Errorf("anthropic second tool_result must be placeholder, got:\n%s", second)
	}
	if _, blocks2, _ := DedupeDiscussionContent(out, "claude"); blocks2 != 0 {
		t.Fatal("anthropic second pass must be no-op")
	}
}

// anthropic tool_result content 为 text 块数组。
func TestDedupeAnthropicBlockContent(t *testing.T) {
	payload := map[string]any{
		"model": "claude-3-5-sonnet",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": []any{
					map[string]any{"type": "text", "text": "讨论记录: 2 条\n\n" + blockA + blockB},
				}},
				map[string]any{"type": "tool_result", "tool_use_id": "tu_2", "content": []any{
					map[string]any{"type": "text", "text": "讨论记录: 2 条\n\n" + blockA + blockB},
				}},
			}},
		},
	}
	body, _ := json.Marshal(payload)
	out, blocks, _ := DedupeDiscussionContent(body, "claude")
	if blocks != 2 {
		t.Fatalf("anthropic blocks: blocks=%d, want 2", blocks)
	}
	if _, blocks2, _ := DedupeDiscussionContent(out, "claude"); blocks2 != 0 {
		t.Fatal("anthropic blocks second pass must be no-op")
	}
}

// openai chat completions：role=tool 消息。
func TestDedupeOpenAIChat(t *testing.T) {
	payload := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "tool", "tool_call_id": "tc_1", "content": "讨论记录: 2 条\n\n" + blockA + blockB},
			map[string]any{"role": "tool", "tool_call_id": "tc_2", "content": "讨论记录: 2 条\n\n" + blockA + blockB},
		},
	}
	body, _ := json.Marshal(payload)
	out, blocks, _ := DedupeDiscussionContent(body, "opencode")
	if blocks != 2 {
		t.Fatalf("openai: blocks=%d, want 2", blocks)
	}
	if _, blocks2, _ := DedupeDiscussionContent(out, "opencode"); blocks2 != 0 {
		t.Fatal("openai second pass must be no-op")
	}
}
