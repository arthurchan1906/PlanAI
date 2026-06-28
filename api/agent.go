package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	pmdb "aipmc/db"
	"aipmc/web"
)

// handleAgentLaunch starts an AI coding agent in a native terminal window.
// POST /pmai/agent/launch  {"agent": "claude"|"codex"|"gemini"}
func (s *Server) handleAgentLaunch(w http.ResponseWriter, body map[string]any) {
	agentName, _ := body["agent"].(string)
	if agentName == "" {
		web.SendError(w, 400, "缺少 'agent' 字段 (claude/codex/gemini)")
		return
	}
	agentName = strings.ToLower(agentName)

	gcfg := pmdb.LoadGlobalConfig()
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", gcfg.ProxyPort)

	var cmd *exec.Cmd
	switch agentName {
	case "claude", "claude-code":
		cmd = exec.Command("claude")
		env := os.Environ()
		if gcfg.AnthropicURL != "" {
			// Anthropic passthrough — Claude Code talks native protocol to proxy
			env = append(env,
				"ANTHROPIC_BASE_URL="+proxyURL,
				"ANTHROPIC_API_KEY="+os.Getenv("UPSTREAM_KEY"),
			)
		} else {
			// Fallback: OpenAI translation path
			env = append(env,
				"OPENAI_API_BASE="+proxyURL+"/v1",
				"OPENAI_API_KEY="+os.Getenv("UPSTREAM_KEY"),
			)
		}
		cmd.Env = env
	case "codex", "openai-codex":
		cmd = exec.Command("codex", "exec")
		cmd.Env = append(os.Environ(),
			"OPENAI_API_BASE="+proxyURL+"/v1",
			"OPENAI_API_KEY="+os.Getenv("UPSTREAM_KEY"),
		)
	case "gemini", "gemini-cli":
		cmd = exec.Command("gemini")
		cmd.Env = append(os.Environ(),
			"GEMINI_API_KEY="+os.Getenv("UPSTREAM_KEY"),
			"GEMINI_API_BASE="+proxyURL,
		)
	default:
		web.SendError(w, 400, fmt.Sprintf("未知 agent: %s (支持 claude/codex/gemini)", agentName))
		return
	}

	if err := launchInTerminal(cmd); err != nil {
		web.SendError(w, 500, fmt.Sprintf("启动失败: %v", err))
		return
	}

	web.SendJSON(w, map[string]any{"ok": true, "agent": agentName})
}

func launchInTerminal(cmd *exec.Cmd) error {
	switch runtime.GOOS {
	case "windows":
		// Build a temporary .bat script to ensure environment variables are set correctly
		var sb strings.Builder
		sb.WriteString("@echo off\r\n")
		for _, e := range cmd.Env {
			if strings.Contains(e, "API_BASE=") || strings.Contains(e, "API_KEY=") ||
				strings.Contains(e, "ANTHROPIC_") || strings.Contains(e, "GEMINI_") ||
				strings.Contains(e, "OPENAI_") || strings.Contains(e, "UPSTREAM_") {
				sb.WriteString("set " + e + "\r\n")
			}
		}
		sb.WriteString(strings.Join(cmd.Args, " ") + "\r\n")
		batContent := sb.String()

		tmpFile, err := os.CreateTemp("", "aipmc-launch-*.bat")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()
		tmpFile.WriteString(batContent)
		tmpFile.Close()

		return exec.Command("cmd", "/c", "start", fmt.Sprintf("AIPM Agent - %s", cmd.Args[0]), "cmd", "/c", tmpPath).Start()
	case "darwin":
		script := fmt.Sprintf(`tell app "Terminal" to do script "cd %s && %s"`,
			func() string { d, _ := os.Getwd(); return d }(),
			strings.Join(cmd.Args, " "))
		return exec.Command("osascript", "-e", script).Start()
	default: // linux and others
		terms := []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal"}
		for _, term := range terms {
			if _, err := exec.LookPath(term); err == nil {
				return exec.Command(term, "-e", strings.Join(cmd.Args, " ")).Start()
			}
		}
		// Fallback: just start detached
		return cmd.Start()
	}
}
