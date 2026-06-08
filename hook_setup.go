package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func setupHooksCmd(targetPlatform string) error {
	commandPath := resolveCommandPath()

	if targetPlatform == "Gemini CLI" || targetPlatform == "gemini" {
		hookCommand := fmt.Sprintf("\"%s\" hook-gemini", commandPath)
		fmt.Printf("  ✅ Gemini hook configured: %s\n", hookCommand)

		runtimeDir, _ := findRuntimeDir()
		projectRoot := filepath.Dir(runtimeDir)
		settingsPath := filepath.Join(projectRoot, ".gemini", "settings.json")

		cfg := map[string]any{}
		if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
			json.Unmarshal(data, &cfg)
		}

		hooks, _ := cfg["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
		}

		hookEntry := []any{map[string]any{"command": hookCommand, "type": "command"}}
		hooks["BeforeAgent"] = hookEntry
		hooks["AfterTool"] = hookEntry
		hooks["AfterAgent"] = hookEntry
		cfg["hooks"] = hooks

		os.MkdirAll(filepath.Dir(settingsPath), 0755)
		data, _ := json.MarshalIndent(cfg, "", "  ")
		return os.WriteFile(settingsPath, data, 0644)
	}

	// Claude Code: direct binary call — no bash wrapper scripts.
	// All hook processing is done in Go (hook_process.go), which parses
	// stdin JSON directly. Uses resolveCommandPath() — writes "aipmc" when
	// the binary is on PATH (portable), or the absolute path as fallback.
	runtimeDir, err := findRuntimeDir()
	if err != nil {
		return fmt.Errorf("find runtime dir: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.local.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	// All hooks use the same command: aipmc hook-process
	// processClaudeHook() dispatches on hook_event_name
	hookEntry := []any{
		map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": commandPath,
					"args":    []string{"hook-process"},
				},
			},
		},
	}

	hooks["Stop"] = hookEntry
	hooks["StopFailure"] = hookEntry
	hooks["UserPromptSubmit"] = hookEntry
	hooks["PostToolUse"] = hookEntry
	cfg["hooks"] = hooks

	os.MkdirAll(filepath.Dir(settingsPath), 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	fmt.Printf("  ✅ Hooks configured → %s\n", settingsPath)
	return nil
}
