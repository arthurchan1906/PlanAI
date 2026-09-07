package main

import "testing"

// agentPlatformKey returns the setup platform key for an agent name; "" if not covered.
func TestAgentPlatformKey(t *testing.T) {
	cases := map[string]string{
		"claude":       "claude",
		"claude-code":  "claude",
		"cc":           "claude",
		"codex":        "codex",
		"openai-codex": "codex",
		"gemini":       "", // 不在本轮范围
		"unknown":      "",
	}
	for in, want := range cases {
		if got := agentPlatformKey(in); got != want {
			t.Errorf("agentPlatformKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// jsonMCPServersConfigured reports whether a JSON platform config contains an
// aipm entry whose command exactly matches commandPath (same predicate setupMCP uses to skip).
func TestJSONMCPServersConfigured(t *testing.T) {
	const cmd = "/opt/bin/aipmc"
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"empty file", "", false},
		{"no mcpServers", `{"foo":"bar"}`, false},
		{"mcpServers no aipm", `{"mcpServers":{"other":{"command":"x"}}}`, false},
		{"aipm command matches", `{"mcpServers":{"aipm":{"command":"` + cmd + `","args":["mcp"]}}}`, true},
		{"aipm command differs", `{"mcpServers":{"aipm":{"command":"/old/path/aipmc","args":["mcp"]}}}`, false},
		{"aipm not an object", `{"mcpServers":{"aipm":"yes"}}`, false},
		{"invalid json", `{not json`, false},
	}
	for _, c := range cases {
		if got := jsonMCPServersConfigured([]byte(c.data), cmd); got != c.want {
			t.Errorf("%s: jsonMCPServersConfigured = %v, want %v", c.name, got, c.want)
		}
	}
}

// codexMCPConfigured reports whether the codex user-level TOML config contains
// a [mcp_servers.aipm] section whose command exactly matches commandPath.
func TestCodexMCPConfigured(t *testing.T) {
	const cmd = "/opt/bin/aipmc"
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"no section", "", false},
		{"no mcp section at all", "model = \"gpt-5.1\"", false},
		{"section command matches", "[mcp_servers.aipm]\ncommand = \"/opt/bin/aipmc\"\nargs = [\"mcp\"]\n", true},
		{"section command differs", "[mcp_servers.aipm]\ncommand = \"/old/path/aipmc\"\n", false},
		{"section ends before other section", "[model]\nname = \"x\"\n[mcp_servers.aipm]\ncommand = \"/opt/bin/aipmc\"\n[mcp_servers.other]\ncommand = \"y\"\n", true},
		{"section command in following section only", "[mcp_servers.aipm]\ncommand = \"/old/path/aipmc\"\n[mcp_servers.aipm2]\ncommand = \"/opt/bin/aipmc\"\n", false},
	}
	for _, c := range cases {
		if got := codexMCPConfigured(c.content, cmd); got != c.want {
			t.Errorf("%s: codexMCPConfigured = %v, want %v", c.name, got, c.want)
		}
	}
}
