package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func hookScriptContent(binaryPath string) string {
	unixPath := strings.ReplaceAll(binaryPath, "\\", "/")
	return "#!/bin/bash\n" +
		"# aipm Stop hook — saves assistant response (pure bash, zero deps)\n" +
		"C=$(cat)\n" +
		"SID=$(echo \"$C\" | grep -o '\"session_id\":\"[^\"]*\"' | head -1 | sed 's/\"session_id\":\"//;s/\"//')\n" +
		"MSG=$(echo \"$C\" | sed 's/.*\"last_assistant_message\":\"//;s/\",\"stop_hook_active.*//;s/\"}$//' | head -c 3000)\n" +
		"[ -z \"$MSG\" ] || [ \"$MSG\" = \"$C\" ] && exit 0\n" +
		"echo \"$MSG\" | \"" + unixPath + "\" log --role assistant --source claude-code --stdin --session \"${SID:-unknown}\" > /dev/null 2>&1\n" +
		"exit 0\n"
}

func hookScriptContentUser(binaryPath string) string {
	unixPath := strings.ReplaceAll(binaryPath, "\\", "/")
	return "#!/bin/bash\n" +
		"# aipm UserPromptSubmit hook — saves user prompt (pure bash, zero deps)\n" +
		"C=$(cat)\n" +
		"SID=$(echo \"$C\" | grep -o '\"session_id\":\"[^\"]*\"' | head -1 | sed 's/\"session_id\":\"//;s/\"//')\n" +
		"MSG=$(echo \"$C\" | sed 's/.*\"prompt\":\"//;s/\",\"hook_event_name.*//;s/\"}$//' | head -c 3000)\n" +
		"[ -z \"$MSG\" ] || [ \"$MSG\" = \"$C\" ] && exit 0\n" +
		"echo \"$MSG\" | \"" + unixPath + "\" log --role user --source claude-code --stdin --session \"${SID:-unknown}\" > /dev/null 2>&1\n" +
		"exit 0\n"
}

func hookScriptContentTool(binaryPath string) string {
	unixPath := strings.ReplaceAll(binaryPath, "\\", "/")
	return "#!/bin/bash\n" +
		"# aipm PostToolUse hook — saves tool calls (pure bash, zero deps)\n" +
		"C=$(cat)\n" +
		"SID=$(echo \"$C\" | grep -o '\"session_id\":\"[^\"]*\"' | head -1 | sed 's/\"session_id\":\"//;s/\"//')\n" +
		"NAME=$(echo \"$C\" | grep -o '\"tool_name\":\"[^\"]*\"' | head -1 | sed 's/\"tool_name\":\"//;s/\"//')\n" +
		"[ -z \"$NAME\" ] && exit 0\n" +
		"echo \"[${NAME}]\" | \"" + unixPath + "\" log --role assistant --source claude-code --stdin --session \"${SID:-unknown}\" > /dev/null 2>&1\n" +
		"exit 0\n"
}

func setupHooksCmd(targetPlatform string) error {
	home, _ := os.UserHomeDir()
	if home == "" { home = os.Getenv("USERPROFILE") }
	if home == "" { return fmt.Errorf("cannot determine home directory") }

	binaryPath := resolveBinaryPath()
	hookDir := filepath.Join(home, ".aipm", "hooks")
	os.MkdirAll(hookDir, 0755)

	if targetPlatform == "Gemini CLI" || targetPlatform == "gemini" {
		hookCommand := fmt.Sprintf("\"%s\" hook-gemini", filepath.ToSlash(binaryPath))
		fmt.Printf("  ✅ Gemini hook configured to use internal command\n")

		// Update .gemini/settings.json
		runtimeDir, _ := findRuntimeDir()
		projectRoot := filepath.Dir(runtimeDir)
		settingsPath := filepath.Join(projectRoot, ".gemini", "settings.json")

		cfg := map[string]any{}
		if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
			json.Unmarshal(data, &cfg)
		}

		hooks, _ := cfg["hooks"].(map[string]any)
		if hooks == nil { hooks = map[string]any{} }

		hookEntry := []any{map[string]any{"command": hookCommand, "type": "command"}}
		hooks["BeforeAgent"] = hookEntry
		hooks["AfterTool"] = hookEntry
		hooks["AfterAgent"] = hookEntry
		cfg["hooks"] = hooks

		os.MkdirAll(filepath.Dir(settingsPath), 0755)
		data, _ := json.MarshalIndent(cfg, "", "  ")
		return os.WriteFile(settingsPath, data, 0644)
	}

	// Write Stop hook script

	stopPath := filepath.Join(hookDir, "save-discussion.sh")
	if err := os.WriteFile(stopPath, []byte(hookScriptContent(binaryPath)), 0755); err != nil {
		return fmt.Errorf("write stop hook: %w", err)
	}
	fmt.Printf("  ✅ Stop hook script → %s\n", stopPath)

	userPath := filepath.Join(hookDir, "save-user-prompt.sh")
	if err := os.WriteFile(userPath, []byte(hookScriptContentUser(binaryPath)), 0755); err != nil {
		return fmt.Errorf("write user hook: %w", err)
	}
	fmt.Printf("  ✅ UserPromptSubmit hook script → %s\n", userPath)

	// Write settings
	runtimeDir, _ := findRuntimeDir()
	projectRoot := filepath.Dir(runtimeDir)
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.local.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil { hooks = map[string]any{} }

	toolPath := filepath.Join(hookDir, "save-tool.sh")
	if err := os.WriteFile(toolPath, []byte(hookScriptContentTool(binaryPath)), 0755); err != nil {
		return fmt.Errorf("write tool hook: %w", err)
	}
	fmt.Printf("  ✅ PostToolUse hook script → %s\n", toolPath)

	stopUnix := strings.ReplaceAll(stopPath, "\\", "/")
	userUnix := strings.ReplaceAll(userPath, "\\", "/")
	toolUnix := strings.ReplaceAll(toolPath, "\\", "/")

	stopEntry := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": stopUnix}}}}
	userEntry := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": userUnix}}}}
	toolEntry := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": toolUnix}}}}

	hooks["Stop"] = stopEntry
	hooks["StopFailure"] = stopEntry
	hooks["UserPromptSubmit"] = userEntry
	hooks["PostToolUse"] = toolEntry
	cfg["hooks"] = hooks

	os.MkdirAll(filepath.Dir(settingsPath), 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	fmt.Printf("  ✅ Hooks config → %s\n", settingsPath)
	return nil
}
