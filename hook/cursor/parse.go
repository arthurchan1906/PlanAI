package cursor

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

type hookPayload struct {
	Event          string
	SessionID      string
	ConversationID string
	GenerationID   string
	Prompt         string
	Text           string
	ToolName       string
	FilePath       string
	Edits          json.RawMessage
	ToolInput      json.RawMessage
	ToolResp       json.RawMessage
	ToolOutput     json.RawMessage
	ErrorMessage   string
}

func parseHookData(data []byte) (hookPayload, []byte) {
	data = bytes.TrimSpace(stripUTF8BOM(data))

	var typed struct {
		Event          string          `json:"hook_event_name"`
		SessionID      string          `json:"session_id"`
		ConversationID string          `json:"conversation_id"`
		GenerationID   string          `json:"generation_id"`
		Prompt         string          `json:"prompt"`
		Text           string          `json:"text"`
		ToolName       string          `json:"tool_name"`
		FilePath       string          `json:"file_path"`
		Edits          json.RawMessage `json:"edits"`
		ToolInput      json.RawMessage `json:"tool_input"`
		ToolResp       json.RawMessage `json:"tool_response"`
		ToolOutput     json.RawMessage `json:"tool_output"`
		ErrorMessage   string          `json:"error_message"`
	}
	if err := json.Unmarshal(data, &typed); err == nil {
		return hookPayload{
			Event: typed.Event, SessionID: typed.SessionID, ConversationID: typed.ConversationID,
			GenerationID: typed.GenerationID,
			Prompt: typed.Prompt, Text: typed.Text, ToolName: typed.ToolName, FilePath: typed.FilePath,
			Edits: typed.Edits, ToolInput: typed.ToolInput, ToolResp: typed.ToolResp,
			ToolOutput: typed.ToolOutput, ErrorMessage: typed.ErrorMessage,
		}, data
	}

	s := string(data)
	return hookPayload{
		Event:          extractJSONStringFieldLenient(s, "hook_event_name"),
		SessionID:      extractJSONStringFieldLenient(s, "session_id"),
		ConversationID: extractJSONStringFieldLenient(s, "conversation_id"),
		GenerationID:   extractJSONStringFieldLenient(s, "generation_id"),
		Prompt:         firstNonEmpty(extractJSONStringFieldLenient(s, "prompt"), extractCursorPromptLenient(s)),
		Text:           firstNonEmpty(extractJSONStringFieldLenient(s, "text"), extractCursorTextLenient(s)),
		ToolName:       extractJSONStringFieldLenient(s, "tool_name"),
		FilePath:       extractJSONStringFieldLenient(s, "file_path"),
		Edits:          extractJSONValueLenient(s, "edits"),
		ToolInput:      extractJSONValueLenient(s, "tool_input"),
		ToolResp:       firstRawMessage(extractJSONValueLenient(s, "tool_response"), extractJSONValueLenient(s, "tool_output")),
		ToolOutput:     extractJSONValueLenient(s, "tool_output"),
		ErrorMessage:   extractJSONStringFieldLenient(s, "error_message"),
	}, data
}

func firstRawMessage(values ...json.RawMessage) json.RawMessage {
	for _, v := range values {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

func extractJSONStringFieldLenient(s, field string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*"((?:\\.|[^"\\])*)"` )
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return decodeJSONString(m[1])
	}
	return ""
}

func extractCursorPromptLenient(s string) string {
	const start = `"prompt":"`
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	for _, key := range []string{`","attachments"`, `","session_id"`, `","hook_event_name"`, `","generation_id"`, `","composer_mode"`} {
		if j := strings.Index(rest, key); j >= 0 {
			return cleanCursorPrompt(decodeJSONString(rest[:j]))
		}
	}
	return ""
}

func extractCursorTextLenient(s string) string {
	const start = `"text":"`
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	for _, key := range []string{`","duration_ms"`, `","model"`, `","session_id"`, `","hook_event_name"`, `","generation_id"`} {
		if j := strings.Index(rest, key); j >= 0 {
			return decodeJSONString(rest[:j])
		}
	}
	return ""
}

func extractJSONValueLenient(s, field string) json.RawMessage {
	marker := `"` + field + `":`
	i := strings.Index(s, marker)
	if i < 0 {
		return nil
	}
	rest := strings.TrimSpace(s[i+len(marker):])
	if len(rest) == 0 {
		return nil
	}
	switch rest[0] {
	case '{':
		if v := extractBalancedJSON(rest, '{', '}'); v != "" {
			return json.RawMessage(v)
		}
	case '[':
		if v := extractBalancedJSON(rest, '[', ']'); v != "" {
			return json.RawMessage(v)
		}
	case '"':
		if end := findJSONStringEnd(rest); end > 0 {
			return json.RawMessage(rest[:end])
		}
	}
	return nil
}

func extractBalancedJSON(s string, open, close byte) string {
	if len(s) == 0 || s[0] != open {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return ""
}

func findJSONStringEnd(s string) int {
	if len(s) == 0 || s[0] != '"' {
		return 0
	}
	esc := false
	for i := 1; i < len(s); i++ {
		if esc {
			esc = false
			continue
		}
		if s[i] == '\\' {
			esc = true
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
	}
	return 0
}

func decodeJSONString(s string) string {
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err == nil {
		return out
	}
	return s
}

func cleanCursorPrompt(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "?,")
}

func stripUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}
