package chatcli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aipmc/agent"
	"aipmc/app"
)

// Run starts the interactive coding agent REPL.
func Run(application *app.App) {
	if application.AI() == nil || !application.AI().Enabled() {
		fmt.Fprintln(os.Stderr, "AI 未配置。请设置以下环境变量:")
		fmt.Fprintln(os.Stderr, "  AI_ENDPOINT   — LLM API 地址")
		fmt.Fprintln(os.Stderr, "  AI_MODEL      — 模型名称（或 AI_CHAT_MODEL）")
		fmt.Fprintln(os.Stderr, "  AI_API_KEY    — API 密钥（如需要）")
		os.Exit(1)
	}

	workDir := agent.ProjectWorkDir()
	svc := agent.NewChatService(application.AI(), workDir, "aipmc-chat")
	sessionDir := agent.SessionDir(workDir)

	newSession := false
	sessionID := ""
	for _, arg := range os.Args[2:] {
		if arg == "--new" {
			newSession = true
		}
		if strings.HasPrefix(arg, "--session=") {
			sessionID = strings.TrimPrefix(arg, "--session=")
		}
	}

	var sess *agent.Session
	if !newSession {
		if sessionID != "" {
			if s, err := agent.LoadSession(filepath.Join(sessionDir, sessionID+".json")); err == nil {
				sess = s
			}
		} else {
			sess = agent.LoadLatestSession(workDir)
		}
	}
	if sess == nil {
		sess = agent.NewSession()
	}
	sessPath := filepath.Join(sessionDir, sess.ID+".json")

	printBanner(sess.ID)
	printRecent(sess.Events)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("▸ ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		switch input {
		case "/exit":
			sess.Save(sessPath)
			fmt.Printf("会话已保存: %s\n", sessPath)
			return
		case "/new":
			sess.Save(sessPath)
			sess = agent.NewSession()
			sessPath = filepath.Join(sessionDir, sess.ID+".json")
			fmt.Printf("新会话: %s\n\n", sess.ID)
			continue
		case "/history":
			printHistory(sess.Events)
			continue
		}

		result, err := svc.Send(sess.ID, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			continue
		}
		sess, _ = agent.LoadSession(filepath.Join(sessionDir, result.SessionID+".json"))
		fmt.Println()
		fmt.Println(result.Response)
		fmt.Println()
	}
	sess.Save(sessPath)
	fmt.Printf("\n会话已保存: %s\n", sessPath)
}

func printBanner(sessionID string) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║  AIPM Coding Agent                       ║")
	fmt.Printf("║  Session: %s               ║\n", sessionID)
	fmt.Println("║  /exit 退出  /new 新会话  /history 历史  ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()
}

func printRecent(events []agent.Event) {
	if len(events) == 0 {
		return
	}
	recent := events
	if len(recent) > 6 {
		recent = recent[len(recent)-6:]
	}
	for _, e := range recent {
		switch e.Role {
		case "user":
			fmt.Printf("▸ %s\n", truncateLine(e.Content, 120))
		case "assistant":
			if e.Content != "" {
				fmt.Printf("  %s\n", truncateLine(e.Content, 120))
			}
			for _, tc := range e.ToolCalls {
				fmt.Printf("  [tool: %s]\n", tc.Name)
			}
		case "tool":
			fmt.Printf("  ← %s\n", truncateLine(e.ToolResult, 80))
		}
	}
	fmt.Println()
}

func printHistory(events []agent.Event) {
	fmt.Println()
	for _, e := range events {
		switch e.Role {
		case "user":
			fmt.Printf("▸ %s\n", e.Content)
		case "assistant":
			if e.Content != "" {
				fmt.Printf("  %s\n", e.Content)
			}
			for _, tc := range e.ToolCalls {
				fmt.Printf("  [tool: %s(%v)]\n", tc.Name, tc.Args)
			}
		case "tool":
			fmt.Printf("  ← %s\n", truncateLine(e.ToolResult, 200))
		}
	}
	fmt.Println()
}

func truncateLine(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
