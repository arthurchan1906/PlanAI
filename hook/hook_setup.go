package hook

// SetupHooksCmd is the entry point for hook configuration.
// Delegates to platform-specific setup functions.
// commandPath is the resolved path to the aipmc binary.
func SetupHooksCmd(commandPath, targetPlatform string) error {
	switch targetPlatform {
	case "Gemini CLI", "gemini":
		return SetupGeminiHooks(commandPath)
	case "Codex CLI", "codex", "Codex (OpenAI)":
		return SetupCodexHooks(commandPath)
	default:
		// Claude Code and all other platforms
		return SetupClaudeHooks(commandPath)
	}
}
