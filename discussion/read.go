package discussion

import (
	"fmt"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// ReadOpts mirrors aipm_read_discussions MCP parameters.
type ReadOpts struct {
	Source string
	LastN  int
	Since  string
	Full   bool
}

// Read fetches discussion_log rows with v1 read_discussions semantics.
func Read(opts ReadOpts) ([]map[string]any, error) {
	return store.ReadDiscussions(store.ReadDiscussionsOpts{
		Source: opts.Source,
		LastN:  opts.LastN,
		Since:  opts.Since,
		Full:   opts.Full,
	})
}

const previewRunes = 200

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
			content = previewContent(content, previewRunes)
		}
		b.WriteString(fmt.Sprintf("%s %s [%s][%s]\n%s\n\n",
			u.Str(r["id"]), u.Str(r["created_at"]), u.Str(r["role"]), u.Str(r["source"]), content))
	}
	return b.String()
}

func previewContent(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
