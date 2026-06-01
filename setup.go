package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ============================================================
// MCP Setup — auto-configure MCP for multiple AI coding platforms
// ============================================================

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

// platformConfig maps platform names to their project-level config paths.
type platformConfig struct {
	Name       string
	ConfigDir  string // relative to project root
	ConfigFile string // filename within ConfigDir
}

var platforms = []platformConfig{
	// JSON-format configs
	{Name: "Claude Code", ConfigDir: ".claude", ConfigFile: "settings.local.json"},
	{Name: "Cursor", ConfigDir: ".cursor", ConfigFile: "mcp.json"},
	{Name: "Windsurf", ConfigDir: ".windsurf", ConfigFile: "mcp.json"},
	{Name: "Cline (VS Code)", ConfigDir: ".vscode", ConfigFile: "cline_mcp_servers.json"},
	{Name: "Roo Code", ConfigDir: ".vscode", ConfigFile: "mcp.json"},
	// TOML-format configs
	{Name: "Codex (OpenAI)", ConfigDir: "", ConfigFile: ""}, // handled specially
}

// setupMCP configures MCP for all supported platforms, or a specific one.
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

		// Read existing or create new (JSON format)
		cfg := mcpConfig{MCPServers: map[string]mcpServerEntry{}}
		if data, err := os.ReadFile(configPath); err == nil {
			json.Unmarshal(data, &cfg)
			if cfg.MCPServers == nil {
				cfg.MCPServers = map[string]mcpServerEntry{}
			}
		}

		// Check existing
		if existing, ok := cfg.MCPServers["aipm"]; ok {
			if existing.Command == binaryPath {
				skipped++
				continue
			}
		}

		cfg.MCPServers["aipm"] = mcpServerEntry{
			Command: binaryPath,
			Args:    []string{"mcp"},
		}

		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: cannot create config dir: %v\n", p.Name, err)
			continue
		}

		data, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: cannot write config: %v\n", p.Name, err)
			continue
		}

		fmt.Printf("  ✅ %s → %s\n", p.Name, configPath)
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
	binaryPath := os.Args[0]
	if !filepath.IsAbs(binaryPath) {
		binaryPath, _ = filepath.Abs(binaryPath)
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
		configPath := filepath.Join(projectRoot, p.ConfigDir, p.ConfigFile)
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		var cfg mcpConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		if entry, ok := cfg.MCPServers["aipm"]; ok {
			configuredCount++
			platformsResult, _ := result["platforms"].([]map[string]any)
			result["platforms"] = append(platformsResult, map[string]any{
				"name":    p.Name,
				"command": entry.Command,
				"path":    configPath,
			})
		}
	}

	if configuredCount > 0 {
		result["configured"] = true
	}

	return result
}
