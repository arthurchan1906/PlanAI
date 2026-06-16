package session

import "strings"

// IsMCPLog reports AIPM MCP tool log lines from hook or MCP server.
// Formats: "📡 aipm_*" (MCP server) and "🛠 MCP:aipm_*" (Cursor hook).
func IsMCPLog(content string) bool {
	if strings.HasPrefix(content, "📡 aipm_") {
		return true
	}
	return strings.HasPrefix(content, "🛠 MCP:aipm_")
}

// ParseMCPTool extracts the aipm_* tool name from a MCP log line.
func ParseMCPTool(content string) string {
	if strings.HasPrefix(content, "🛠 MCP:") {
		rest := strings.TrimPrefix(content, "🛠 MCP:")
		if idx := strings.IndexAny(rest, " \"✅❌"); idx >= 0 {
			rest = rest[:idx]
		}
		return strings.TrimSpace(rest)
	}
	s := strings.TrimPrefix(content, "📡 ")
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	name := parts[0]
	if idx := strings.IndexAny(name, "✅❌"); idx >= 0 {
		name = name[:idx]
	}
	return name
}
