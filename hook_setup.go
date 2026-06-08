package main

// setupHooksCmd is the entry point for hook configuration.
// Delegates to platform-specific setup functions.
func setupHooksCmd(targetPlatform string) error {
	commandPath := resolveCommandPath()

	switch targetPlatform {
	case "Gemini CLI", "gemini":
		return setupGeminiHooks(commandPath)
	default:
		// Claude Code and all other platforms
		return setupClaudeHooks(commandPath)
	}
}
