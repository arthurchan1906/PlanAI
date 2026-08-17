package discussion

import (
	"fmt"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// ReadOpts mirrors aipm_read_discussions MCP parameters.
type ReadOpts struct {
	Source      string
	SessionID   string
	LastN       int
	Since       string
	Cursor      string
	Full        bool
	ProjectPath string
}

// Read fetches discussion_log rows with v1 read_discussions semantics.
func Read(opts ReadOpts) ([]map[string]any, error) {
	return store.ReadDiscussions(store.ReadDiscussionsOpts{
		Source:      opts.Source,
		SessionID:   opts.SessionID,
		LastN:       opts.LastN,
		Since:       opts.Since,
		Cursor:      opts.Cursor,
		Full:        opts.Full,
		ProjectPath: opts.ProjectPath,
	})
}

// CursorFromResults returns the ID of the last row as the next cursor,
// or empty string if the result set is empty.
func CursorFromResults(rows []map[string]any) string {
	if len(rows) == 0 {
		return ""
	}
	return u.Str(rows[len(rows)-1]["id"])
}

// PreviewRunes is the default preview length for discussion MCP text output.
const PreviewRunes = 200

// FormatResults renders discussion rows for MCP or CLI output.
func FormatResults(rows []map[string]any, full bool) string {
	var b strings.Builder
	if len(rows) == 0 {
		b.WriteString("(无讨论记录)\n")
		return b.String()
	}
	for _, r := range rows {
		content := u.Str(r["content"])
		if !full {
			content = PreviewContent(content, PreviewRunes)
		}
		b.WriteString(fmt.Sprintf("%s %s [%s][%s][sid=%s]\n%s\n\n",
			u.Str(r["id"]), u.Str(r["created_at"]), u.Str(r["role"]), u.Str(r["source"]),
			shortSessionID(u.Str(r["session_id"])), content))
	}
	return b.String()
}

// shortSessionID renders a compact session handle for text output; the full
// id stays available in the structured results.
func shortSessionID(sid string) string {
	if sid == "" || sid == "unknown" {
		return "?"
	}
	if len(sid) > 13 {
		return sid[:13]
	}
	return sid
}

// FormatSessionMessages renders messages grouped under one session (search full_session mode).
func FormatSessionMessages(sessionID string, messages []map[string]any, full bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n### Session: %s\n", sessionID))
	for _, r := range messages {
		content := u.Str(r["content"])
		if !full {
			content = PreviewContent(content, PreviewRunes)
		}
		b.WriteString(fmt.Sprintf("[%s][%s] %s  %s\n",
			u.Str(r["role"]), u.Str(r["source"]), u.Str(r["created_at"]), content))
	}
	return b.String()
}

// PreviewContent truncates s to maxRunes runes (safe for UTF-8 / CJK).
func PreviewContent(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// SnippetContent renders a compact hit-context snippet around the first
// occurrence of query (or, failing that, one of its CJK 2-grams) in s.
// It is the search-results counterpart of PreviewContent: long messages are
// shown as the hit context instead of a blind head-truncation, so the user
// sees why the row matched. Falls back to PreviewContent when nothing hits.
func SnippetContent(s, query string, radius int) string {
	runes := []rune(s)
	if len(runes) <= 2*radius+1 || query == "" {
		return PreviewContent(s, 2*radius+1)
	}
	q := []rune(query)
	pos := indexRunes(runes, q)
	if pos < 0 {
		// CJK 2-gram fallback: a row may have matched via bigrams even when
		// the full query string is not contiguous in the content.
		for _, g := range cjkBigrams(query) {
			if pos = indexRunes(runes, []rune(g)); pos >= 0 {
				break
			}
		}
	}
	if pos < 0 {
		return PreviewContent(s, 2*radius+1)
	}
	start := pos - radius
	if start < 0 {
		start = 0
	}
	end := pos + len(q) + radius
	if end > len(runes) {
		end = len(runes)
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	b.WriteString(string(runes[start:end]))
	if end < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}

func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
