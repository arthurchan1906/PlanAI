package api

import (
	"testing"

	pmdb "aipmc/db"
)

// TestBuildAgentCmd_DirUsesProjectPath verifies the launched agent's working
// directory comes from the web server's project path, not the process cwd.
func TestBuildAgentCmd_DirUsesProjectPath(t *testing.T) {
	rt := pmdb.AgentRuntime{
		Model:           "deepseek-v4-pro",
		ReasoningEffort: "medium",
		ExtraEnv:        map[string]string{"FOO": "bar"},
	}
	proxyURL := "http://127.0.0.1:19530"
	projectDir := "/tmp/proj-under-test"

	for _, agent := range []string{"claude", "claude-code", "codex", "openai-codex", "gemini", "gemini-cli", "opencode"} {
		cmd, _, err := buildAgentCmd(agent, proxyURL, rt, projectDir)
		if err != nil {
			t.Fatalf("%s: buildAgentCmd error: %v", agent, err)
		}
		if cmd.Dir != projectDir {
			t.Errorf("%s: cmd.Dir = %q, want %q", agent, cmd.Dir, projectDir)
		}
	}
}

// TestBuildAgentCmd_EmptyProjectDir falls back is caller's job — the helper
// must not panic and still set Dir when a value is provided.
func TestBuildAgentCmd_NoPanic(t *testing.T) {
	rt := pmdb.AgentRuntime{Model: "m", ReasoningEffort: "low"}
	for _, agent := range []string{"claude", "codex", "gemini", "opencode"} {
		if _, _, err := buildAgentCmd(agent, "http://127.0.0.1:19530", rt, ""); err != nil {
			t.Fatalf("%s: unexpected error: %v", agent, err)
		}
	}
}
