package proxy

import (
	"bytes"
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
// 方案：转发前扫描请求体里的 tool result 字符串值，按行首 disc-* id 去重——
// 同一 id 二次出现时替换为一行占位符（保留 id 锚点，agent 仍可引用）。
//
// 约束（Claude review 8/17 对齐）：
//  1. 幂等性：占位符以 '[' 开头，不匹配 disc-* 块起始模式 → 二次处理不再
//     改写 → body 稳定 → deepseek prefix cache 不被去重反复打断。
//  2. 最小编辑（漏洞 A）：只替换字符串值本身，其余字节（顶层 key 顺序、
//     HTML 转义、数字格式）原样保留 → 去重请求的 prefix cache 在替换点之前
//     全部命中，不会从第一个 token 全 miss。
//  3. 识别边界（漏洞 B）：值必须同时含 disc- 行首块与 AIPM 讨论工具专属
//     header（"讨论记录:" / "搜索讨论历史"）才处理，sqlite3/grep 等输出
//     即使恰好含 disc- 行首格式也不误伤。
//  4. 数据边界：只在转发层改写，绝不写回 discussion_log（保护 M0 对账）。

var discIDRe = regexp.MustCompile(`^disc-\d{8}-\d{6}-[0-9a-f]{6} `)

const dedupPlaceholder = "已在上文出现，内容省略（如需内容可单条重读）"

// placeholderRe recognizes our own placeholder lines so a second pass keeps
// them as standalone lines instead of folding them into a neighbouring block
// (byte-level idempotency — constraint #1).
var placeholderRe = regexp.MustCompile(`^\[disc-\d{8}-\d{6}-[0-9a-f]{6} 已在上文出现`)

// DedupeDiscussionContent rewrites tool-result discussion blocks so each
// disc-* id appears at most once per request. Returns the rewritten body,
// the number of duplicated blocks collapsed, and the saved rune count.
// Idempotent: running it again on its own output is a no-op.
func DedupeDiscussionContent(body []byte, agent string) ([]byte, int, int) {
	seen := make(map[string]bool)
	type edit struct {
		start, end int
		repl       []byte
	}
	var edits []edit
	totalBlocks, totalSaved := 0, 0

	for _, sv := range scanJSONStringValues(body) {
		if !isDiscussionResult(sv.path, sv.val) {
			continue
		}
		deduped, blocks, saved := dedupeTextWithSeen(sv.val, seen)
		if blocks == 0 {
			continue
		}
		enc, _ := json.Marshal(deduped)
		edits = append(edits, edit{sv.start, sv.end, enc})
		totalBlocks += blocks
		totalSaved += saved
	}
	if len(edits) == 0 {
		return body, 0, 0
	}

	// Apply edits back-to-front; replacing a later span never shifts an
	// earlier one, so the recorded offsets stay valid. Only string-value
	// spans change; every other byte (key order, numbers, escaping) is
	// preserved verbatim (constraint #2 — minimal edit).
	out := body
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		var b bytes.Buffer
		b.Write(out[:e.start])
		b.Write(e.repl)
		b.Write(out[e.end:])
		out = b.Bytes()
	}
	return out, totalBlocks, totalSaved
}

// isDiscussionResult narrows candidates to AIPM discussion tool results:
// the value must carry the tool's专属 header plus a disc- marker, and sit at
// a tool-result-ish JSON path (output / content / text).
func isDiscussionResult(path []string, val string) bool {
	if len(path) == 0 {
		return false
	}
	if !strings.Contains(val, "disc-") {
		return false
	}
	if !strings.Contains(val, "讨论记录:") && !strings.Contains(val, "搜索讨论历史") {
		return false
	}
	switch path[len(path)-1] {
	case "output", "content", "text":
		return true
	}
	return false
}

// strVal is one scanned JSON string value plus its object-key path and raw
// byte span (start = opening quote index, end = closing quote index + 1).
// Array indices are not tracked — callers only need the trailing key.
type strVal struct {
	path       []string
	start, end int
	val        string
}

// scanJSONStringValues walks raw JSON bytes and returns every string VALUE
// (not object key) with its key path and byte span. A lightweight byte state
// machine tracks string/escape state, container nesting, and key→value
// transitions. JSON escapes (\n, \uXXXX, ...) are decoded, so val is the
// real string content; offsets let callers rewrite a span in place without
// re-encoding any other part of the document.
func scanJSONStringValues(raw []byte) []strVal {
	var out []strVal
	var containers []byte
	var pendingKeys []string
	expectingValue := false
	inString, escaped := false, false
	var sb strings.Builder
	start := 0

	push := func(c byte) {
		containers = append(containers, c)
		pendingKeys = append(pendingKeys, "")
	}
	pop := func() {
		containers = containers[:len(containers)-1]
		pendingKeys = pendingKeys[:len(pendingKeys)-1]
	}
	top := func() byte {
		if len(containers) > 0 {
			return containers[len(containers)-1]
		}
		return 0
	}
	path := func() []string {
		var p []string
		for _, k := range pendingKeys {
			if k != "" {
				p = append(p, k)
			}
		}
		return p
	}
	clearTopKey := func() {
		if top() == '{' {
			pendingKeys[len(pendingKeys)-1] = ""
		}
	}
	isValueStart := func(c byte) bool {
		return c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' || c == 't' || c == 'f' || c == 'n'
	}
	isDelim := func(c byte) bool {
		return c == ',' || c == ']' || c == '}' || c == ':' || c == ' ' || c == '\t' || c == '\n' || c == '\r'
	}

	i := 0
	for i < len(raw) {
		c := raw[i]
		if inString {
			if escaped {
				if c == 'u' && i+5 <= len(raw) {
					if r, ok := decodeHex4(raw[i+1 : i+5]); ok {
						rn := r
						skip := 4
						if r >= 0xD800 && r <= 0xDBFF && i+11 <= len(raw) && raw[i+5] == '\\' && raw[i+6] == 'u' {
							if r2, ok2 := decodeHex4(raw[i+7 : i+11]); ok2 && r2 >= 0xDC00 && r2 <= 0xDFFF {
								rn = 0x10000 + (r-0xD800)<<10 + (r2 - 0xDC00)
								skip = 10
							}
						}
						sb.WriteRune(rn)
						i += 1 + skip
						escaped = false
						continue
					}
				}
				switch c {
				case 'n':
					sb.WriteByte('\n')
				case 't':
					sb.WriteByte('\t')
				case 'r':
					sb.WriteByte('\r')
				case 'b':
					sb.WriteByte('\b')
				case 'f':
					sb.WriteByte('\f')
				default:
					sb.WriteByte(c) // \", \\, \/, or invalid — keep byte
				}
				escaped = false
				i++
				continue
			}
			switch c {
			case '\\':
				escaped = true
				i++
				continue
			case '"':
				val := sb.String()
				sb.Reset()
				if top() == '{' && !expectingValue {
					pendingKeys[len(pendingKeys)-1] = val
				} else {
					out = append(out, strVal{path: path(), start: start, end: i + 1, val: val})
					clearTopKey()
				}
				expectingValue = false
				inString = false
				i++
				continue
			}
			sb.WriteByte(c)
			i++
			continue
		}
		switch {
		case c == '"':
			inString = true
			start = i
		case c == '{':
			push('{')
			expectingValue = false
		case c == '[':
			push('[')
			expectingValue = true
		case c == '}':
			pop()
			// A compound value just completed: if it was a key's value,
			// that key is no longer pending.
			clearTopKey()
			expectingValue = false
		case c == ']':
			pop()
			clearTopKey()
			expectingValue = top() == '['
		case c == ':':
			expectingValue = true
		case isValueStart(c):
			for i < len(raw) && !isDelim(raw[i]) {
				i++
			}
			clearTopKey()
			expectingValue = false
			continue
		}
		i++
	}
	return out
}

// decodeHex4 decodes exactly four hex bytes into a rune.
func decodeHex4(b []byte) (rune, bool) {
	if len(b) != 4 {
		return 0, false
	}
	var v int
	for _, c := range b {
		h := -1
		switch {
		case c >= '0' && c <= '9':
			h = int(c - '0')
		case c >= 'a' && c <= 'f':
			h = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			h = int(c-'A') + 10
		}
		if h < 0 {
			return 0, false
		}
		v = v*16 + h
	}
	return rune(v), true
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

// dedupSummary formats the [DEDUP] observability log line.
func dedupSummary(agent string, blocks, saved int) string {
	return fmt.Sprintf("agent=%s blocks=%d saved_chars=%d", agent, blocks, saved)
}
