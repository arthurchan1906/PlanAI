package proxy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"aipmc/u"
)

// discussion_dedup.go — proxy 层 discussion 读取内容去重（8/17 讨论共识）。
//
// 问题：agent 多次调用 aipm_read_discussions（最近 N 条/无 cursor 增量）时，
// 返回的 disc-* 内容在对话历史里反复出现，重复占用上下文窗口。
//
// 方案：转发前扫描请求体里的 tool result 文本，按行首 disc-* id 去重——
// 同一 id 二次出现时替换为一行占位符（保留 id 锚点，agent 仍可引用）。
//
// 三条硬约束（Claude review 8/17 对齐）：
//  1. 幂等性：占位符以 '[' 开头，不匹配 disc-* 块起始模式 → 二次处理不再
//     改写 → body 稳定 → deepseek prefix cache 不被去重反复打断。
//  2. 识别边界：只处理 tool result（function_call_output / tool_result /
//     role=tool）的 output 文本，不动 call_id/消息结构，不误伤普通消息里
//     引用的 disc-* id。
//  3. 数据边界：只在转发层改写，绝不写回 discussion_log（保护 M0 对账）。

var discIDRe = regexp.MustCompile(`^disc-\d{8}-\d{6}-[0-9a-f]{6} `)

const dedupPlaceholder = "已在上文出现，内容省略（如需内容可单条重读）"

// placeholderRe recognizes our own placeholder lines so a second pass keeps
// them as standalone lines instead of folding them into a neighbouring block
// (byte-level idempotency — Claude review 8/17 constraint #1).
var placeholderRe = regexp.MustCompile(`^\[disc-\d{8}-\d{6}-[0-9a-f]{6} 已在上文出现`)

// DedupeDiscussionContent rewrites tool-result discussion blocks so each
// disc-* id appears at most once per request. Returns the rewritten body,
// the number of duplicated blocks collapsed, and the saved rune count.
// Idempotent: running it again on its own output is a no-op.
func DedupeDiscussionContent(body []byte, agent string) ([]byte, int, int) {
	switch agent {
	case "claude":
		return dedupeAnthropic(body)
	case "codex":
		return dedupeCodex(body)
	default:
		// OpenAI chat completions (role=tool) fallback.
		return dedupeOpenAI(body)
	}
}

func dedupeCodex(body []byte) ([]byte, int, int) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body, 0, 0
	}
	input, ok := raw["input"].([]any)
	if !ok {
		return body, 0, 0
	}
	// seen 跨所有 tool result 共享：同一 disc id 在多个消息里出现也要去重
	// （agent 多次 read_discussions 的典型形态是多个 function_call_output）。
	seen := make(map[string]bool)
	totalBlocks, totalSaved := 0, 0
	changed := false
	for i, item := range input {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if im["type"] != "function_call_output" {
			continue
		}
		out, ok := im["output"].(string)
		if !ok || !strings.Contains(out, "disc-") {
			continue
		}
		deduped, blocks, saved := dedupeTextWithSeen(out, seen)
		if blocks == 0 {
			continue
		}
		im["output"] = deduped
		input[i] = im
		changed = true
		totalBlocks += blocks
		totalSaved += saved
	}
	if !changed {
		return body, 0, 0
	}
	raw["input"] = input
	b, err := json.Marshal(raw)
	if err != nil {
		return body, 0, 0
	}
	return b, totalBlocks, totalSaved
}

func dedupeAnthropic(body []byte) ([]byte, int, int) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body, 0, 0
	}
	messages, ok := raw["messages"].([]any)
	if !ok {
		return body, 0, 0
	}
	seen := make(map[string]bool)
	totalBlocks, totalSaved := 0, 0
	changed := false
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for j, block := range content {
			cb, ok := block.(map[string]any)
			if !ok || cb["type"] != "tool_result" {
				continue
			}
			switch c := cb["content"].(type) {
			case string:
				if !strings.Contains(c, "disc-") {
					continue
				}
				deduped, blocks, saved := dedupeTextWithSeen(c, seen)
				if blocks == 0 {
					continue
				}
				cb["content"] = deduped
				content[j] = cb
				changed = true
				totalBlocks += blocks
				totalSaved += saved
			case []any:
				// tool_result content may be a list of text blocks.
				subChanged := false
				for k, sub := range c {
					sb, ok := sub.(map[string]any)
					if !ok || sb["type"] != "text" {
						continue
					}
					txt, ok := sb["text"].(string)
					if !ok || !strings.Contains(txt, "disc-") {
						continue
					}
					deduped, blocks, saved := dedupeTextWithSeen(txt, seen)
					if blocks == 0 {
						continue
					}
					sb["text"] = deduped
					c[k] = sb
					subChanged = true
					totalBlocks += blocks
					totalSaved += saved
				}
				if subChanged {
					cb["content"] = c
					content[j] = cb
					changed = true
				}
			}
		}
		if changed {
			messages[i] = msg
		}
	}
	if !changed {
		return body, 0, 0
	}
	raw["messages"] = messages
	b, err := json.Marshal(raw)
	if err != nil {
		return body, 0, 0
	}
	return b, totalBlocks, totalSaved
}

func dedupeOpenAI(body []byte) ([]byte, int, int) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body, 0, 0
	}
	messages, ok := raw["messages"].([]any)
	if !ok {
		return body, 0, 0
	}
	seen := make(map[string]bool)
	totalBlocks, totalSaved := 0, 0
	changed := false
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] != "tool" {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok || !strings.Contains(content, "disc-") {
			continue
		}
		deduped, blocks, saved := dedupeTextWithSeen(content, seen)
		if blocks == 0 {
			continue
		}
		msg["content"] = deduped
		messages[i] = msg
		changed = true
		totalBlocks += blocks
		totalSaved += saved
	}
	if !changed {
		return body, 0, 0
	}
	raw["messages"] = messages
	b, err := json.Marshal(raw)
	if err != nil {
		return body, 0, 0
	}
	return b, totalBlocks, totalSaved
}

// dedupeText collapses repeated disc-* blocks in a tool-result text.
// A block starts at a line matching "disc-YYYYMMDD-HHMMSS-xxxxxx ".
// The first occurrence is kept verbatim; later occurrences are replaced
// with a one-line placeholder preserving the id anchor.
func dedupeText(text string) (string, int, int) {
	return dedupeTextWithSeen(text, make(map[string]bool))
}

// dedupeTextWithSeen is dedupeText with a caller-owned seen map, so
// duplicates across multiple tool-result messages collapse too.
func dedupeTextWithSeen(text string, seen map[string]bool) (string, int, int) {
	lines := strings.Split(text, "\n")
	var out []string
	var block []string
	curID := ""
	blocks, saved := 0, 0

	trimTrailingEmpty := func(rows []string) []string {
		for len(rows) > 0 && rows[len(rows)-1] == "" {
			rows = rows[:len(rows)-1]
		}
		return rows
	}

	flush := func() {
		if curID != "" {
			// 只有块内容剥离尾随空行；普通文本（header/footer）原样保留。
			block = trimTrailingEmpty(block)
		}
		if len(block) == 0 {
			curID = ""
			return
		}
		joined := strings.Join(block, "\n")
		switch {
		case curID == "":
			out = append(out, joined)
		case seen[curID]:
			p := "[" + curID + " " + dedupPlaceholder + "]"
			out = append(out, p)
			blocks++
			saved += len([]rune(joined)) - len([]rune(p))
		default:
			seen[curID] = true
			out = append(out, joined)
		}
		block = nil
		curID = ""
	}

	for _, ln := range lines {
		if placeholderRe.MatchString(ln) {
			// Our own placeholder: standalone line, never folded into a block.
			flush()
			out = append(out, ln)
			continue
		}
		if m := discIDRe.FindString(ln); m != "" {
			flush()
			curID = strings.TrimSuffix(m, " ")
			block = append(block, ln)
			continue
		}
		block = append(block, ln)
	}
	flush()
	return strings.Join(out, "\n"), blocks, saved
}

// dedupSummary formats the [DEDUP] observability log line.
func dedupSummary(agent string, blocks, saved int) string {
	return fmt.Sprintf("agent=%s blocks=%d saved_chars=%d", agent, blocks, saved)
}

// dedupeRequestBody applies discussion dedup to a raw request body before
// forwarding, logging [DEDUP] only when duplicates were actually collapsed
// (keeps steady-state logs quiet). Returns the (possibly rewritten) body.
func dedupeRequestBody(rawBody []byte, agent string) []byte {
	db, blocks, saved := DedupeDiscussionContent(rawBody, agent)
	if blocks == 0 {
		return rawBody
	}
	u.LogShared("DEDUP", "%s", dedupSummary(agent, blocks, saved))
	return db
}
