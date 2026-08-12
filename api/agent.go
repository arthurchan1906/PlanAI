package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/web"
)

type agentSession struct {
	Agent     string `json:"agent"`
	StartedAt int64  `json:"started_at"`
}

func (s *Server) ensureSessionGC() {
	s.sessionsInit.Do(func() {
		go func() {
			for {
				time.Sleep(60 * time.Second)
				s.sessionsMu.Lock()
				cutoff := time.Now().Add(-24 * time.Hour).Unix()
				filtered := s.sessions[:0]
				for _, sess := range s.sessions {
					if sess.StartedAt >= cutoff {
						filtered = append(filtered, sess)
					}
				}
				s.sessions = filtered
				s.sessionsMu.Unlock()
			}
		}()
	})
}

// handleAgentLaunch starts an AI coding agent in a native terminal window.
// POST /pmai/agent/launch  {"agent": "claude"|"codex"|"gemini"}
func (s *Server) handleAgentLaunch(w http.ResponseWriter, body map[string]any) {
	agentName, _ := body["agent"].(string)
	if agentName == "" {
		web.SendError(w, 400, "缺少 'agent' 字段 (claude/codex/gemini/opencode)")
		return
	}
	agentName = strings.ToLower(agentName)

	gcfg := pmdb.LoadGlobalConfig()
	cfg := pmdb.LoadConfig()
	proxyPort := gcfg.ProxyPort
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	// Resolve effective agent config: global profile + project-level overrides
	rt, err := pmdb.ResolveAgentConfig(agentName, gcfg, cfg)
	if err != nil {
		web.SendError(w, 400, fmt.Sprintf("agent 配置错误: %v", err))
		return
	}

	// codex: write its proxy profile as a launch side effect (kept here, not in
	// buildAgentCmd, so the pure command builder stays side-effect-free).
	if agentName == "codex" || agentName == "openai-codex" {
		effort := rt.ReasoningEffort
		if effort == "" {
			effort = "medium"
		}
		if err := codexWriteProxyProfile(proxyURL, rt.Model, effort); err != nil {
			web.SendError(w, 500, fmt.Sprintf("Codex 配置失败: %v", err))
			return
		}
	}

	// Launch inside the web-served project, not the serve process cwd — when
	// multiple `aipmc serve` instances run, os.Getwd() alone lands agents in
	// the wrong directory.
	workDir := s.deps.ProjectPath
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	cmd, envOverrides, err := buildAgentCmd(agentName, proxyURL, rt, workDir)
	if err != nil {
		web.SendError(w, 400, err.Error())
		return
	}

	essential := essentialSystemEnv()
	// Apply resolved extra env vars (merged global + profile + project overrides)
	for k, v := range rt.ExtraEnv {
		envOverrides = append(envOverrides, k+"="+v)
	}
	cmd.Env = append(essential, envOverrides...)

	if err := launchInTerminal(cmd, envOverrides); err != nil {
		web.SendError(w, 500, fmt.Sprintf("启动失败: %v", err))
		return
	}

	s.ensureSessionGC()
	s.sessionsMu.Lock()
	s.sessions = append(s.sessions, agentSession{
		Agent:     agentName,
		StartedAt: time.Now().Unix(),
	})
	s.sessionsMu.Unlock()

	web.SendJSON(w, map[string]any{"ok": true, "agent": agentName})
}

// buildAgentCmd constructs the exec.Cmd (and env overrides) that launches an AI
// coding agent. projectDir is the working directory the agent runs in. It has
// no side effects (no profile/file writes) so it is unit-testable.
func buildAgentCmd(agentName, proxyURL string, rt pmdb.AgentRuntime, projectDir string) (*exec.Cmd, []string, error) {
	var cmd *exec.Cmd
	var envOverrides []string
	switch agentName {
	case "claude", "claude-code":
		cmd = exec.Command("claude")
		envOverrides = []string{
			"ANTHROPIC_BASE_URL=" + proxyURL,
			"ANTHROPIC_AUTH_TOKEN=local",
		}
		if rt.Model != "" {
			envOverrides = append(envOverrides, "ANTHROPIC_MODEL="+pmdb.LoadModelRegistry().ResolveModelForProtocol(rt.Model, "anthropic"))
		}
		if rt.SubAgentModel != "" {
			envOverrides = append(envOverrides, "CLAUDE_CODE_SUBAGENT_MODEL="+rt.SubAgentModel)
		}
		if rt.OpusModel != "" {
			envOverrides = append(envOverrides, "ANTHROPIC_DEFAULT_OPUS_MODEL="+rt.OpusModel)
		}
		if rt.SonnetModel != "" {
			envOverrides = append(envOverrides, "ANTHROPIC_DEFAULT_SONNET_MODEL="+rt.SonnetModel)
		}
		if rt.HaikuModel != "" {
			envOverrides = append(envOverrides, "ANTHROPIC_DEFAULT_HAIKU_MODEL="+rt.HaikuModel)
		}
		if rt.SmallFastModel != "" {
			envOverrides = append(envOverrides, "ANTHROPIC_SMALL_FAST_MODEL="+rt.SmallFastModel)
		}
		if rt.EffortLevel != "" {
			envOverrides = append(envOverrides, "CLAUDE_CODE_EFFORT_LEVEL="+rt.EffortLevel)
		}
	case "codex", "openai-codex":
		cmd = exec.Command("codex", "-p", "proxy")
		envOverrides = []string{
			"OPENAI_API_KEY=local",
		}
	case "gemini", "gemini-cli":
		cmd = exec.Command("gemini")
		envOverrides = []string{
			"GEMINI_API_KEY=local",
			"GOOGLE_API_KEY=local",
			"GEMINI_API_BASE=" + proxyURL,
			"GOOGLE_API_BASE=" + proxyURL,
			"GOOGLE_GEMINI_BASE_URL=" + proxyURL,
		}
	case "opencode", "oc":
		if rt.Model != "" {
			cmd = exec.Command("opencode", "--model", "aipm/"+rt.Model)
		} else {
			cmd = exec.Command("opencode")
		}
		envOverrides = nil
	default:
		return nil, nil, fmt.Errorf("未知 agent: %s (支持 claude/codex/gemini/opencode)", agentName)
	}
	cmd.Dir = projectDir
	return cmd, envOverrides, nil
}

// handleAgentCmd returns the shell command to launch an agent so users can
// copy and run it manually (useful on Windows where auto-launch may not work).
// GET /pmai/web/agent/cmd?agent=claude
func (s *Server) handleAgentCmd(w http.ResponseWriter, agentName string) {
	agentName = strings.ToLower(agentName)
	if agentName == "" {
		web.SendError(w, 400, "缺少 'agent' 参数")
		return
	}

	gcfg := pmdb.LoadGlobalConfig()
	cfg := pmdb.LoadConfig()
	proxyPort := gcfg.ProxyPort
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	rt, err := pmdb.ResolveAgentConfig(agentName, gcfg, cfg)
	if err != nil {
		web.SendError(w, 400, fmt.Sprintf("agent 配置错误: %v", err))
		return
	}

	var envs []string
	var cmdLine string
	switch agentName {
	case "claude", "claude-code":
		envs = append(envs, "ANTHROPIC_BASE_URL="+proxyURL, "ANTHROPIC_AUTH_TOKEN=local")
		if rt.Model != "" {
			envs = append(envs, "ANTHROPIC_MODEL="+pmdb.LoadModelRegistry().ResolveModelForProtocol(rt.Model, "anthropic"))
		}
		cmdLine = "claude"
	case "codex", "openai-codex":
		cmdLine = "codex -p proxy"
		envs = append(envs, "OPENAI_API_KEY=local")
	case "gemini", "gemini-cli":
		envs = append(envs,
			"GEMINI_API_KEY=local", "GOOGLE_API_KEY=local",
			"GEMINI_API_BASE="+proxyURL, "GOOGLE_API_BASE="+proxyURL, "GOOGLE_GEMINI_BASE_URL="+proxyURL,
		)
		cmdLine = "gemini"
	case "opencode", "oc":
		if rt.Model != "" {
			cmdLine = "opencode --model aipm/" + rt.Model
		} else {
			cmdLine = "opencode"
		}
	default:
		web.SendError(w, 400, "未知 agent: "+agentName)
		return
	}

	// Build Unix and Windows command strings
	unixCmd := strings.Join(envs, " ") + " " + cmdLine
	winCmd := strings.Join(envs, " && set ") + " && " + cmdLine
	if len(envs) > 0 {
		winCmd = "set " + winCmd
	}

	web.SendJSON(w, map[string]any{
		"agent":  agentName,
		"unix":   unixCmd,
		"win":    winCmd,
		"cmdline": cmdLine,
		"envs":   envs,
	})
}

func (s *Server) handleAgentSessions(w http.ResponseWriter) {
	s.ensureSessionGC()
	s.sessionsMu.Lock()
	list := make([]agentSession, len(s.sessions))
	copy(list, s.sessions)
	s.sessionsMu.Unlock()
	if list == nil {
		list = []agentSession{}
	}
	web.SendJSON(w, map[string]any{"sessions": list})
}

func codexWriteProxyProfile(proxyURL, model, reasoningEffort string) error {
	return WriteCodexProxyProfile(proxyURL, model, reasoningEffort)
}

// WriteCodexProxyProfile updates ~/.codex/proxy.config.toml (the `codex -p proxy`
// profile) in place, preserving codex-managed state instead of overwriting the
// whole file on every launch. It refreshes only the model/endpoint fields and
// guarantees the [mcp_servers.aipm] root table exists.
//
// Why: codex treats this profile file as a writable config layer and persists
// hook trust records ([hooks.state]) and MCP tool overrides
// ([mcp_servers.aipm.tools.*]) into it. An overwrite loses hook trust — codex
// then prompts "Hooks need review" on every start. Worse, if a tools sub-table
// survives without the root table, codex rejects the profile with
// "invalid transport in mcp_servers.aipm" when writing trust, so the trust can
// never persist.
func WriteCodexProxyProfile(proxyURL, model, reasoningEffort string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return err
	}
	if model == "" {
		model = "gpt-5.1"
	}
	if reasoningEffort == "" {
		reasoningEffort = "medium"
	}

	profilePath := filepath.Join(codexDir, "proxy.config.toml")
	existing, _ := os.ReadFile(profilePath)
	merged := mergeCodexProxyProfile(string(existing), model, reasoningEffort, proxyURL)
	if merged == string(existing) {
		return nil
	}
	return os.WriteFile(profilePath, []byte(merged), 0644)
}

const codexProfileHeader = `# Codex proxy profile — auto-generated by AIPM Agent Launcher
# Usage: codex -p proxy exec "prompt"
`

// mergeCodexProxyProfile merges aipm's model fields into an existing codex
// proxy profile. Unknown sections (hooks.state, mcp_servers, tui.*, ...) are
// kept byte-for-byte.
func mergeCodexProxyProfile(existing, model, reasoningEffort, proxyURL string) string {
	if strings.TrimSpace(existing) == "" {
		return fmt.Sprintf(`%smodel = "%s"
model_provider = "custom"
model_reasoning_effort = "%s"

[model_providers.custom]
name = "AIPM Proxy"
base_url = "%s/v1"
env_key_instructions = "not needed for local proxy"

[mcp_servers.aipm]
command = "aipmc"
args = ["mcp"]
`, codexProfileHeader, model, reasoningEffort, proxyURL)
	}

	lines := strings.Split(existing, "\n")
	out := make([]string, 0, len(lines))
	inCustom := false
	sawModel, sawProvider, sawEffort, sawCustom, sawMcpRoot := false, false, false, false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[model_providers.custom]" {
			inCustom = true
			sawCustom = true
			out = append(out, line)
			continue
		}
		if trimmed == "[mcp_servers.aipm]" {
			sawMcpRoot = true
		}
		if inCustom {
			if strings.HasPrefix(trimmed, "base_url") {
				out = append(out, fmt.Sprintf("base_url = %q", proxyURL+"/v1"))
				continue
			}
			if trimmed == "" || strings.HasPrefix(trimmed, "[") {
				inCustom = false
			}
		} else if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			switch {
			case strings.HasPrefix(trimmed, "model_provider "):
				out = append(out, `model_provider = "custom"`)
				sawProvider = true
				continue
			case strings.HasPrefix(trimmed, "model_reasoning_effort "):
				out = append(out, fmt.Sprintf("model_reasoning_effort = %q", reasoningEffort))
				sawEffort = true
				continue
			case strings.HasPrefix(trimmed, "model "):
				out = append(out, fmt.Sprintf("model = %q", model))
				sawModel = true
				continue
			}
		}
		out = append(out, line)
	}

	// Top-level scalars must be inserted before the first table header so they
	// never land inside a table such as [hooks.state].
	var missing []string
	if !sawModel {
		missing = append(missing, fmt.Sprintf("model = %q", model))
	}
	if !sawProvider {
		missing = append(missing, `model_provider = "custom"`)
	}
	if !sawEffort {
		missing = append(missing, fmt.Sprintf("model_reasoning_effort = %q", reasoningEffort))
	}
	if len(missing) > 0 {
		idx := 0
		for i, l := range out {
			if strings.HasPrefix(strings.TrimSpace(l), "[") {
				idx = i
				break
			}
		}
		out = insertLines(out, idx, missing)
	}

	if !sawCustom {
		out = append(out, "", "[model_providers.custom]",
			`name = "AIPM Proxy"`,
			fmt.Sprintf("base_url = %q", proxyURL+"/v1"),
			`env_key_instructions = "not needed for local proxy"`)
	}

	// Guarantee the mcp_servers.aipm root table exists. Codex writes tool
	// overrides as [mcp_servers.aipm.tools.*] sub-tables; without the root
	// table the profile fails codex validation with "invalid transport".
	if !sawMcpRoot {
		block := []string{"", "[mcp_servers.aipm]", `command = "aipmc"`, `args = ["mcp"]`}
		idx := -1
		for i, l := range out {
			if strings.HasPrefix(strings.TrimSpace(l), "[mcp_servers.") {
				idx = i
				break
			}
		}
		if idx < 0 {
			out = append(out, block...)
		} else {
			out = insertLines(out, idx, block)
		}
	}
	return strings.Join(out, "\n")
}

func insertLines(out []string, idx int, block []string) []string {
	if idx >= len(out) {
		return append(out, block...)
	}
	res := make([]string, 0, len(out)+len(block))
	res = append(res, out[:idx]...)
	res = append(res, block...)
	res = append(res, out[idx:]...)
	return res
}

func essentialSystemEnv() []string {
	keep := map[string]bool{
		"PATH": true, "PATHEXT": true,
		"HOME": true, "USERPROFILE": true,
		"APPDATA": true, "LOCALAPPDATA": true,
		"TEMP": true, "TMP": true,
		"SYSTEMROOT": true, "SYSTEMDRIVE": true,
		"HOMEDRIVE": true, "HOMEPATH": true,
		"ComSpec": true, "USERNAME": true, "COMPUTERNAME": true,
		"ProgramFiles": true, "ProgramFiles(x86)": true,
		"ProgramData": true, "ALLUSERSPROFILE": true,
	}
	var out []string
	for _, e := range os.Environ() {
		key := e
		if idx := strings.Index(e, "="); idx >= 0 {
			key = e[:idx]
		}
		if keep[key] {
			out = append(out, e)
		}
	}
	return out
}

func launchInTerminal(cmd *exec.Cmd, envOverrides []string) error {
	switch runtime.GOOS {
	case "windows":
		exePath, err := exec.LookPath(cmd.Args[0])
		if err != nil {
			return fmt.Errorf("未找到 %s，请确认已安装并在 PATH 中", cmd.Args[0])
		}

		workDir := cmd.Dir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}

		var sb strings.Builder
		sb.WriteString("@echo off\r\n")
		sb.WriteString("chcp 65001 >nul 2>&1\r\n")
		fmt.Fprintf(&sb, "cd /d \"%s\"\r\n", workDir)
		sb.WriteString("title AIPM Agent - " + cmd.Args[0] + "\r\n")
		for _, e := range envOverrides {
			sb.WriteString("set " + e + "\r\n")
		}
		fmt.Fprintf(&sb, "echo Starting AIPM Agent - %s\r\n", cmd.Args[0])
		sb.WriteString("echo.\r\n")
		sb.WriteString(`"` + exePath + `"`)
		for i := 1; i < len(cmd.Args); i++ {
			sb.WriteString(" " + cmd.Args[i])
		}
		sb.WriteString("\r\n")
		sb.WriteString("if %ERRORLEVEL% NEQ 0 (\r\n")
		sb.WriteString("  echo.\r\n")
		fmt.Fprintf(&sb, "  echo Error: %s exited with code %%ERRORLEVEL%%\r\n", cmd.Args[0])
		sb.WriteString("  pause\r\n")
		sb.WriteString(")\r\n")
		sb.WriteString("del \"%~f0\"\r\n")

		tmpFile, _ := os.CreateTemp("", "aipmc-launch-*.bat")
		tmpPath := tmpFile.Name()
		tmpFile.WriteString(sb.String())
		tmpFile.Close()

		return exec.Command("cmd", "/c", "start",
			fmt.Sprintf("AIPM Agent - %s", cmd.Args[0]), "cmd", "/c", tmpPath).Start()
	case "darwin":
		cdDir := cmd.Dir
		if cdDir == "" {
			cdDir, _ = os.Getwd()
		}
		var exports string
		for _, e := range envOverrides {
			exports += "export " + e + "; "
		}
		script := fmt.Sprintf(`tell app "Terminal" to do script "clear && printf '\\033[3J' && cd '%s' && %s %s"`,
			cdDir, exports, strings.Join(cmd.Args, " "))
		return exec.Command("osascript", "-e", script).Start()
	default:
		var exports string
		for _, e := range envOverrides {
			exports += "export " + e + "; "
		}
		fullCmd := exports + strings.Join(cmd.Args, " ")
		terms := []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal"}
		for _, term := range terms {
			if _, err := exec.LookPath(term); err == nil {
				return exec.Command(term, "-e", fullCmd).Start()
			}
		}
		return cmd.Start()
	}
}
