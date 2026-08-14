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
