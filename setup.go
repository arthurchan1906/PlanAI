package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ============================================================
// MCP Setup — auto-configure MCP for multiple AI coding platforms
// ============================================================

// platformConfig maps platform names to their project-level config paths.
type platformConfig struct {
	Name       string   // display name
	Key        string   // short key for CLI (e.g. "claude", "cursor")
	ConfigDir  string   // relative to project root
	ConfigFile string   // filename within ConfigDir
	Aliases    []string // alternative names users might type
}

var platforms = []platformConfig{
	// JSON-format configs
	{Name: "Claude Code", Key: "claude", ConfigDir: "", ConfigFile: ".mcp.json", Aliases: []string{"claude-code", "cc"}},
	{Name: "Cursor", Key: "cursor", ConfigDir: ".cursor", ConfigFile: "mcp.json"},
	{Name: "Windsurf", Key: "windsurf", ConfigDir: ".windsurf", ConfigFile: "mcp.json"},
	{Name: "Cline (VS Code)", Key: "cline", ConfigDir: ".vscode", ConfigFile: "cline_mcp_servers.json", Aliases: []string{"vscode-cline"}},
	{Name: "Roo Code", Key: "roo", ConfigDir: ".vscode", ConfigFile: "mcp.json", Aliases: []string{"roo-code", "roocode"}},
	{Name: "Gemini CLI", Key: "gemini", ConfigDir: ".gemini", ConfigFile: "settings.json", Aliases: []string{"gc"}},
	// TOML-format configs
	{Name: "Codex (OpenAI)", Key: "codex", ConfigDir: "", ConfigFile: "", Aliases: []string{"openai", "openai-codex"}},
}

// platformByKey maps lowercase short keys/aliases to platform configs.
var platformByKey = buildPlatformIndex()

func buildPlatformIndex() map[string]*platformConfig {
	idx := map[string]*platformConfig{}
	for i := range platforms {
		p := &platforms[i]
		idx[strings.ToLower(p.Key)] = p
		for _, a := range p.Aliases {
			idx[strings.ToLower(a)] = p
		}
	}
	return idx
}

// resolvePlatform looks up a platform by name, key, or alias (case-insensitive).
// Returns the display name or an error with suggestions.
func resolvePlatform(input string) (string, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" || input == "all" {
		return input, nil
	}

	if p, ok := platformByKey[input]; ok {
		return p.Name, nil
	}
	return "", fmt.Errorf("unknown platform %q", input)
}

// listPlatforms prints available platforms to stdout.
func listPlatforms() {
	fmt.Println("Available platforms for MCP setup:")
	fmt.Println()
	for _, p := range platforms {
		aliases := []string{p.Key}
		aliases = append(aliases, p.Aliases...)
		fmt.Printf("  %-20s  (aliases: %s)\n", p.Name, strings.Join(aliases, ", "))
	}
	fmt.Println()
	fmt.Println("Usage: aipmc setup <platform>")
	fmt.Println("       aipmc setup all    — configure all platforms")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  aipmc setup claude")
	fmt.Println("  aipmc setup cursor")
	fmt.Println("  aipmc setup codex")
}

// setupMCP configures MCP for all supported platforms, or a specific one.
// targetPlatform can be a display name, short key, alias, "" (empty = all), or "all".
func setupMCP(targetPlatform string) error {
	runtimeDir, err := findRuntimeDir()
	if err != nil {
		return fmt.Errorf("cannot find project root — run aipmc init first: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)

	binaryPath := resolveBinaryPath()

	configured := 0
	skipped := 0

	for _, p := range platforms {
		if targetPlatform != "" && targetPlatform != "all" && p.Name != targetPlatform {
			continue
		}

		// Codex uses TOML format at user level
		if p.Name == "Codex (OpenAI)" {
			if err := setupCodexMCP(binaryPath); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  Codex: %v\n", err)
			} else {
				fmt.Printf("  ✅ Codex (OpenAI) → ~/.codex/config.toml\n")
				configured++
			}
			continue
		}

		configPath := filepath.Join(projectRoot, p.ConfigDir, p.ConfigFile)

		// Read existing config as a generic map to preserve ALL existing keys
		// (e.g. Claude Code settings.local.json may have permissions, hooks, etc.)
		cfg := map[string]any{}
		fileExisted := false
		if data, err := os.ReadFile(configPath); err == nil {
			fileExisted = true
			if len(data) > 0 {
				if err := json.Unmarshal(data, &cfg); err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠️  %s: cannot parse existing config, will overwrite: %v\n", p.Name, err)
					cfg = map[string]any{}
				}
			}
		}

		// Extract or create mcpServers map, preserving type
		var servers map[string]any
		if existing, ok := cfg["mcpServers"]; ok {
			if m, ok := existing.(map[string]any); ok {
				servers = m
			}
		}
		if servers == nil {
			servers = map[string]any{}
		}

		// Check if aipm is already configured with the same binary path
		if existing, ok := servers["aipm"]; ok {
			if m, ok := existing.(map[string]any); ok {
				if cmd, ok := m["command"].(string); ok && cmd == binaryPath {
					if fileExisted {
						skipped++
						continue
					}
				}
			}
		}

		// Build the aipm server entry
		servers["aipm"] = map[string]any{
			"command": binaryPath,
			"args":    []any{"mcp"},
		}
		cfg["mcpServers"] = servers

		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: cannot create config dir: %v\n", p.Name, err)
			continue
		}

		data, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: cannot write config: %v\n", p.Name, err)
			continue
		}

		action := "configured"
		if fileExisted {
			action = "updated"
		}
		fmt.Printf("  ✅ %s → %s (%s)\n", p.Name, configPath, action)
		configured++
	}

	if configured == 0 && skipped == 0 {
		return fmt.Errorf("no platforms configured")
	}

	if skipped > 0 {
		fmt.Printf("  ℹ️  %d platform(s) already configured (skipped)\n", skipped)
	}

	fmt.Printf("\nMCP Server command: %s mcp\n", binaryPath)
	if targetPlatform == "" || targetPlatform == "all" {
		fmt.Printf("已配置 %d 个平台。重启 AI Coding 工具后生效。\n", configured+skipped)
	}
	return nil
}

// setupCodexMCP writes the MCP server entry to Codex's user-level TOML config.
// Codex config: ~/.codex/config.toml
// Format:
//
//	[mcp_servers.aipm]
//	command = "path/to/aipmc"
//	args = ["mcp"]
func setupCodexMCP(binaryPath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home dir: %w", err)
	}
	codexDir := filepath.Join(homeDir, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")

	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("cannot create .codex dir: %w", err)
	}

	// Read existing config
	existing := ""
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	}

	// The TOML entry to add
	entry := fmt.Sprintf("[mcp_servers.aipm]\ncommand = \"%s\"\nargs = [\"mcp\"]\n\n", binaryPath)

	// Check if already configured
	if containsStrIn(existing, "[mcp_servers.aipm]") {
		return nil // already configured
	}

	// Append to existing config
	newConfig := existing + entry
	return os.WriteFile(configPath, []byte(newConfig), 0644)
}

func containsStrIn(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func resolveBinaryPath() string {
	// Use os.Executable() to find the real binary — handles PATH lookups,
	// symlinks, and relative invocations correctly.
	binaryPath, err := os.Executable()
	if err != nil {
		// Fallback to os.Args[0] resolved against CWD
		binaryPath = os.Args[0]
		if !filepath.IsAbs(binaryPath) {
			binaryPath, _ = filepath.Abs(binaryPath)
		}
	}
	if runtime.GOOS == "windows" {
		binaryPath = filepath.ToSlash(binaryPath)
	}
	return binaryPath
}

// checkMCPSetup verifies which platforms have MCP configured.
func checkMCPSetup() map[string]any {
	runtimeDir, err := findRuntimeDir()
	if err != nil {
		return map[string]any{"configured": false, "reason": "project not initialized"}
	}
	projectRoot := filepath.Dir(runtimeDir)

	result := map[string]any{
		"configured": false,
		"platforms":  []map[string]any{},
	}

	configuredCount := 0
	for _, p := range platforms {
		// Skip Codex — TOML format not checked here
		if p.ConfigDir == "" && p.ConfigFile == "" {
			continue
		}
		configPath := filepath.Join(projectRoot, p.ConfigDir, p.ConfigFile)
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		if servers, ok := cfg["mcpServers"].(map[string]any); ok {
			if _, ok := servers["aipm"]; ok {
				configuredCount++
				platformsResult, _ := result["platforms"].([]map[string]any)
				result["platforms"] = append(platformsResult, map[string]any{
					"name": p.Name,
					"key":  p.Key,
					"path": configPath,
				})
			}
		}
	}

	if configuredCount > 0 {
		result["configured"] = true
	}

	return result
}
