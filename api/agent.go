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
		effort := rt.ReasoningEffort
		if effort == "" {
			effort = "medium"
		}
		if err := codexWriteProxyProfile(proxyURL, rt.Model, effort); err != nil {
			web.SendError(w, 500, fmt.Sprintf("Codex 配置失败: %v", err))
			return
		}
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
		web.SendError(w, 400, fmt.Sprintf("未知 agent: %s (支持 claude/codex/gemini/opencode)", agentName))
		return
	}

	essential := essentialSystemEnv()
	// Apply resolved extra env vars (merged global + profile + project overrides)
	for k, v := range rt.ExtraEnv {
		envOverrides = append(envOverrides, k+"="+v)
	}
	cmd.Env = append(essential, envOverrides...)
	cmd.Dir, _ = os.Getwd()

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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	codexDir := filepath.Join(homeDir, ".codex")
	os.MkdirAll(codexDir, 0755)

	if model == "" {
		model = "gpt-5.1"
	}

	profile := fmt.Sprintf(`# Codex proxy profile — auto-generated by AIPM Agent Launcher
# Usage: codex -p proxy exec "prompt"
model = "%s"
model_provider = "custom"
model_reasoning_effort = "%s"

[model_providers.custom]
name = "AIPM Proxy"
base_url = "%s/v1"
env_key_instructions = "not needed for local proxy"
`, model, reasoningEffort, proxyURL)

	profilePath := filepath.Join(codexDir, "proxy.config.toml")
	return os.WriteFile(profilePath, []byte(profile), 0644)
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
