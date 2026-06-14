package cursor

import (
	"encoding/json"
	"os"
	"strings"

	"aipmc/store"
)

type cursorTranscriptLine struct {
	Role    string `json:"role"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func extractTranscriptPath(data []byte) string {
	var v struct {
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.Unmarshal(data, &v); err == nil && v.TranscriptPath != "" {
		return v.TranscriptPath
	}
	return extractJSONStringFieldLenient(string(data), "transcript_path")
}

func extractUserQuery(text string) string {
	const start = "<user_query>\n"
	const end = "\n</user_query>"
	if i := strings.Index(text, start); i >= 0 {
		rest := text[i+len(start):]
		if j := strings.Index(rest, end); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
	}
	return strings.TrimSpace(text)
}

// ReadLatestUserQueryFromTranscript returns the latest user message from transcript JSONL.
func ReadLatestUserQueryFromTranscript(path string) string {
	return readLatestUserQueryFromTranscript(path)
}

func readLatestUserQueryFromTranscript(path string) string {
	lines, err := readTranscriptLinesReverse(path, 200)
	if err != nil {
		return ""
	}
	for _, line := range lines {
		var entry cursorTranscriptLine
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Role != "user" {
			continue
		}
		for _, c := range entry.Message.Content {
			if c.Type != "text" || c.Text == "" {
				continue
			}
			if q := extractUserQuery(c.Text); q != "" {
				return q
			}
		}
	}
	return ""
}

// readAllUserQueriesFromTranscriptReverse returns recent user queries newest-first.
func readAllUserQueriesFromTranscriptReverse(path string, maxMessages int) []string {
	lines, err := readTranscriptLinesReverse(path, maxMessages*3)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range lines {
		var entry cursorTranscriptLine
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Role != "user" {
			continue
		}
		for _, c := range entry.Message.Content {
			if c.Type != "text" || c.Text == "" {
				continue
			}
			if q := extractUserQuery(c.Text); q != "" {
				out = append(out, q)
				if len(out) >= maxMessages {
					return out
				}
			}
		}
	}
	return out
}

// ReadLatestUnloggedUserQueryFromTranscript returns the newest transcript user
// message not already stored in discussion_log for this session.
func ReadLatestUnloggedUserQueryFromTranscript(path, sessionID string) string {
	return readLatestUnloggedUserQueryFromTranscript(path, sessionID)
}

func readLatestUnloggedUserQueryFromTranscript(path, sessionID string) string {
	logged, _ := store.RecentUserPrompts(sessionID, "cursor", 20)
	loggedSet := make(map[string]struct{}, len(logged))
	for _, s := range logged {
		loggedSet[s] = struct{}{}
	}
	for _, q := range readAllUserQueriesFromTranscriptReverse(path, 30) {
		q = cleanCursorPrompt(q)
		if q == "" {
			continue
		}
		if _, seen := loggedSet[q]; !seen {
			return q
		}
	}
	return ""
}

// readLatestAssistantTextFromTranscript returns the most recent assistant text
// block from the transcript (skips tool-only turns).
func readLatestAssistantTextFromTranscript(path string) string {
	lines, err := readTranscriptLinesReverse(path, 400)
	if err != nil {
		return ""
	}
	for _, line := range lines {
		var entry cursorTranscriptLine
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Role != "assistant" {
			continue
		}
		var parts []string
		for _, c := range entry.Message.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func readTranscriptLinesReverse(path string, maxLines int) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	all := strings.Split(string(content), "\n")
	var lines []string
	for i := len(all) - 1; i >= 0 && len(lines) < maxLines; i-- {
		line := strings.TrimSpace(all[i])
		if line == "" {
			continue
		}
		// Skip non-message records (e.g. turn_ended).
		if !strings.Contains(line, `"role"`) {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// PreferTranscriptText uses transcript UTF-8 when hook payload text is garbled.
func PreferTranscriptText(hookText, transcriptPath string, fromTranscript func(string) string) string {
	return preferTranscriptText(hookText, transcriptPath, fromTranscript)
}

func preferTranscriptText(hookText, transcriptPath string, fromTranscript func(string) string) string {
	if transcriptPath == "" {
		return hookText
	}
	if t := fromTranscript(transcriptPath); t != "" {
		return t
	}
	return hookText
}

func isMostlyASCII(s string) bool {
	if s == "" {
		return true
	}
	runes := []rune(s)
	ascii := 0
	for _, r := range runes {
		if r < 128 {
			ascii++
		}
	}
	return ascii*100/len(runes) > 80
}

func preferAssistantText(hookText, transcriptPath string) string {
	hookText = FixHookText(hookText)
	// Prefer transcript UTF-8 text over hook payload (which may be garbled on Windows).
	if transcriptPath != "" {
		if t := readLatestAssistantTextFromTranscript(transcriptPath); t != "" {
			return t
		}
	}
	if hookText != "" {
		return hookText
	}
	return preferTranscriptText(hookText, transcriptPath, readLatestAssistantTextFromTranscript)
}
