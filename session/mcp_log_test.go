package session

import "testing"

func TestIsMCPLog(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"📡 aipm_get_briefing ✅", true},
		{"🛠 MCP:aipm_get_briefing", true},
		{"🛠 MCP:aipm_search_discussions \"query\"", true},
		{"🔧 shell command", false},
		{"💭 thinking", false},
	}
	for _, c := range cases {
		if got := IsMCPLog(c.content); got != c.want {
			t.Fatalf("IsMCPLog(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}

func TestParseMCPTool(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"📡 aipm_get_briefing ✅", "aipm_get_briefing"},
		{"🛠 MCP:aipm_get_briefing", "aipm_get_briefing"},
		{"🛠 MCP:aipm_search_discussions \"密友\"", "aipm_search_discussions"},
		{"📡 aipm_record_commit ❌ \"title\"", "aipm_record_commit"},
	}
	for _, c := range cases {
		if got := ParseMCPTool(c.content); got != c.want {
			t.Fatalf("ParseMCPTool(%q) = %q, want %q", c.content, got, c.want)
		}
	}
}

func TestBestSessionMatchPrefersShorterSession(t *testing.T) {
	long := SessionAnchor{
		Source: "claude-code", SessionID: "long",
		FirstSeen: "2026-06-12T14:23:27", LastSeen: "2026-06-16T16:05:46",
	}
	short := SessionAnchor{
		Source: "claude-code", SessionID: "short",
		FirstSeen: "2026-06-16T09:22:46", LastSeen: "2026-06-16T09:48:42",
	}
	ts, _ := parseISO("2026-06-16T09:22:53")
	key, ok := bestSessionMatch([]SessionAnchor{long, short}, "claude-code", ts)
	if !ok || key != short.Key() {
		t.Fatalf("bestSessionMatch = %q ok=%v, want %q", key, ok, short.Key())
	}
}
