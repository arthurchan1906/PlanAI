package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// processGeminiHook reads the Gemini CLI BeforeAgent/AfterTool/AfterAgent hook stdin
// JSON and saves to discussion_log.
// Called via: aipmc hook-gemini
func processGeminiHook() {
	// Debug log
	f, _ := os.OpenFile(filepath.Join(os.TempDir(), "aipm-hook-debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer func() { if f != nil { f.Close() } }()

	if f != nil {
		fmt.Fprintf(f, "[%s] Hook triggered\n", nowISO())
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		if f != nil { fmt.Fprintf(f, "  ERROR reading stdin: %v\n", err) }
		return
	}
	if f != nil { fmt.Fprintf(f, "  Read %d bytes\n", len(data)) }

	var c struct {
		Event          string `json:"hook_event_name"`
		Prompt         string `json:"prompt"`
		PromptResponse string `json:"prompt_response"`
		ToolName       string `json:"tool_name"`
		ToolInput      any    `json:"tool_input"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		if f != nil { fmt.Fprintf(f, "  ERROR unmarshalling: %v\n", err) }
		return
	}
	if f != nil { fmt.Fprintf(f, "  Event: %s\n", c.Event) }

	switch c.Event {
	case "BeforeAgent":
		if c.Prompt != "" {
			_, err := logDiscussion("", "user", "gemini-cli", c.Prompt, "")
			if err != nil && f != nil { fmt.Fprintf(f, "  ERROR logging: %v\n", err) }
		}
	case "AfterTool":
		if c.ToolName != "" {
			inputJSON, _ := json.Marshal(c.ToolInput)
			_, err := logDiscussion("", "assistant", "gemini-cli-tool", fmt.Sprintf("[Tool Call: %s] %s", c.ToolName, string(inputJSON)), "")
			if err != nil && f != nil { fmt.Fprintf(f, "  ERROR logging tool: %v\n", err) }
		}
	case "AfterAgent":
		if c.PromptResponse != "" {
			_, err := logDiscussion("", "assistant", "gemini-cli", c.PromptResponse, "")
			if err != nil && f != nil { fmt.Fprintf(f, "  ERROR logging response: %v\n", err) }
		}
	}
}

// setupGeminiHooks writes Gemini CLI hook configuration to .gemini/settings.json.
func setupGeminiHooks(commandPath string) error {
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
