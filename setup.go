package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apipkg "aipmc/api"
	pmdb "aipmc/db"
	"aipmc/paths"
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
	// Hook + MCP (custom format)
	{Name: "OpenCode", Key: "opencode", ConfigDir: "", ConfigFile: "", Aliases: []string{"oc"}},
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
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return fmt.Errorf("cannot find project root — run aipmc init first: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)

	commandPath := resolveCommandPath()

	configured := 0
	skipped := 0

	for _, p := range platforms {
		if targetPlatform != "" && targetPlatform != "all" && p.Name != targetPlatform {
			continue
		}
		// Codex uses TOML format at user level
		if p.Name == "Codex (OpenAI)" {
			if err := setupCodexMCP(commandPath); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  Codex: %v\n", err)
			} else {
				fmt.Printf("  ✅ Codex (OpenAI) → ~/.codex/config.toml\n")
				configured++
			}
			continue
		}

		// OpenCode uses its own JSON format
		if p.Name == "OpenCode" {
			skippedOC, err := setupOpencodeMCP(projectRoot, commandPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  OpenCode: %v\n", err)
			} else if skippedOC {
				skipped++
			} else {
				fmt.Printf("  ✅ OpenCode → opencode.json\n")
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

		// Check if aipm is already configured with the same command path.
		// Accept both the portable "aipmc" and any absolute path pointing
		// to the same binary — avoids overwriting on every setup run.
		skip := false
		if existing, ok := servers["aipm"]; ok {
			if m, ok := existing.(map[string]any); ok {
				if cmd, ok := m["command"].(string); ok {
					if cmd == commandPath {
						skip = true
					}
				}
			}
		}
		if skip && fileExisted {
			skipped++
			continue
		}

		// Build the aipm server entry.
		// commandPath is either "aipmc" (when on PATH — portable) or the
		// full executable path (when run from an arbitrary location).
		servers["aipm"] = map[string]any{
			"command": commandPath,
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

	fmt.Printf("\nMCP Server command: %s mcp\n", commandPath)
	if commandPath != "aipmc" {
		fmt.Println("  ⚠️  aipmc is NOT on PATH — config uses a fixed path that won't survive relocation.")
		fmt.Println("     To make configs portable: add aipmc to PATH and re-run 'aipmc setup'.")
	}
	if targetPlatform == "" || targetPlatform == "all" {
		fmt.Printf("已配置 %d 个平台。重启 AI Coding 工具后生效。\n", configured+skipped)
	}
	return nil
}

// replaceTomlSection replaces a TOML section block (e.g. [mcp_servers.aipm])
// with new content. Returns the updated string and whether the section was found.
func replaceTomlSection(toml, sectionHeader, newContent string) (string, bool) {
	lines := strings.Split(toml, "\n")
	var out []string
	inSection := false
	replaced := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == sectionHeader {
			inSection = true
			if !replaced {
				out = append(out, strings.TrimRight(newContent, "\n"))
				replaced = true
			}
			continue
		}

		if inSection {
			// End of section: blank line or next section header
			if trimmed == "" || strings.HasPrefix(trimmed, "[") {
				inSection = false
				out = append(out, line)
			}
			continue
		}

		out = append(out, line)
	}

	if !replaced {
		return toml, false
	}
	return strings.Join(out, "\n"), true
}

// setupCodexMCP writes the MCP server entry to Codex's user-level TOML config,
// and generates a proxy profile (~/.codex/proxy.config.toml) for connecting through aipmc proxy.
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

	// 1. Write/update MCP server entry in main config.toml
	existing := ""
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	}

	entry := fmt.Sprintf("[mcp_servers.aipm]\ncommand = \"%s\"\nargs = [\"mcp\"]\n\n", binaryPath)
	updated, found := replaceTomlSection(existing, "[mcp_servers.aipm]", entry)
	if found {
		// Section exists — only write if content actually changed (avoids duplicate entries).
		if updated != existing {
			if err := os.WriteFile(configPath, []byte(updated), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  Codex MCP config write failed: %v\n", err)
			} else {
				fmt.Printf("  ✅ Codex MCP updated → %s mcp\n", binaryPath)
			}
		} else {
			fmt.Printf("  ℹ️  Codex MCP already configured (skipped)\n")
		}
	} else {
		// Section not found — append
		newConfig := existing + entry
		if err := os.WriteFile(configPath, []byte(newConfig), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  Codex MCP config write failed: %v\n", err)
		} else {
			fmt.Printf("  ✅ Codex MCP added → %s mcp\n", binaryPath)
		}
	}

	// 2. Generate proxy profile — failure is a warning, not fatal (MCP entry is already set up).
	// Merges into any existing profile so codex-managed state (hook trust,
	// mcp_servers tool overrides) is preserved.
	profilePath := filepath.Join(codexDir, "proxy.config.toml")
	gcfg := pmdb.LoadGlobalConfig()
	proxyPort := gcfg.ProxyPort
	modelName := gcfg.ProxyModel
	if modelName == "" {
		modelName = "gpt-5.1"
	}

	if err := apipkg.WriteCodexProxyProfile(fmt.Sprintf("http://127.0.0.1:%d", proxyPort), modelName, "medium"); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠️  Codex proxy profile write failed: %v\n", err)
	} else {
		fmt.Printf("  ✅ Codex proxy profile → %s\n", profilePath)
		fmt.Printf("     Usage: codex -p proxy\n")
	}
	return nil
}

// setupOpencodeMCP writes the MCP server entry to opencode.json.
// Returns skipped=true when already configured with identical settings.
func setupOpencodeMCP(projectRoot, binaryPath string) (skipped bool, err error) {
	configPath := filepath.Join(projectRoot, "opencode.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			cfg = map[string]any{}
		}
	}

	// Track whether we need to write — covers both $schema and MCP changes.
	needWrite := false

	// Ensure $schema is present (OpenCode requires it)
	if _, ok := cfg["$schema"]; !ok {
		cfg["$schema"] = "https://opencode.ai/config.json"
		needWrite = true
	}

	// ── MCP ──
	mcpRaw, _ := cfg["mcp"]
	mcp, _ := mcpRaw.(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	mcpChanged := false
	if existing, ok := mcp["aipm"]; ok {
		if em, ok := existing.(map[string]any); ok {
			if cmd, ok := em["command"]; ok {
				if cmdArr, ok := cmd.([]any); ok && len(cmdArr) > 0 {
					if cmdStr, ok := cmdArr[0].(string); ok && cmdStr != binaryPath {
						mcpChanged = true
					}
				}
			}
		}
	} else {
		mcpChanged = true
	}
	if mcpChanged {
		mcp["aipm"] = map[string]any{
			"type":    "local",
			"command": []any{binaryPath, "mcp"},
			"enabled": true,
		}
		cfg["mcp"] = mcp
		needWrite = true
	}

	if !needWrite {
		return true, nil // skipped — already configured with identical settings
	}

	data, _ := json.MarshalIndent(cfg, "", "  ")
	return false, os.WriteFile(configPath, data, 0644)
}

func resolveCommandPath() string {
	return paths.ConfigCommand()
}

// checkMCPSetup verifies which platforms have MCP configured.
func checkMCPSetup() map[string]any {
	runtimeDir, err := pmdb.RuntimeDir()
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
		// Codex uses user-level config (~/.codex/config.toml) — skip
		if p.Name == "Codex (OpenAI)" {
			continue
		}

		// OpenCode uses project-level opencode.json with "mcp" key (not "mcpServers")
		if p.Name == "OpenCode" {
			configPath := filepath.Join(projectRoot, "opencode.json")
			data, err := os.ReadFile(configPath)
			if err != nil {
				continue
			}
			var cfg map[string]any
			if err := json.Unmarshal(data, &cfg); err != nil {
				continue
			}
			if mcp, ok := cfg["mcp"].(map[string]any); ok {
				if _, ok := mcp["aipm"]; ok {
					configuredCount++
					platformsResult, _ := result["platforms"].([]map[string]any)
					result["platforms"] = append(platformsResult, map[string]any{
						"name": p.Name,
						"key":  p.Key,
						"path": configPath,
					})
				}
			}
			continue
		}

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
