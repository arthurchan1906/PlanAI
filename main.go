package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bufio"
	"aipmc/agent"
	"aipmc/ai"
	"aipmc/analyze"
	"aipmc/cli"
	pmdb "aipmc/db"
	"aipmc/hook"
	"aipmc/mcp"
	"aipmc/store"
	"aipmc/web"
)

//go:embed frontend/dist
var uiFS embed.FS

// aiClient is the global AI client, initialized in main().
// nil when AI is not configured — all AI-dependent code paths
// gracefully degrade.
var aiClient *ai.Client

func initAI() {
	cfg := pmdb.LoadConfig()
	endpoint := cfg.AIEndpoint
	if endpoint == "" {
		endpoint = os.Getenv("AI_ENDPOINT")
	}
	model := cfg.AIModel
	if model == "" {
		model = os.Getenv("AI_MODEL")
	}
	chatModel := cfg.AIChatModel
	if chatModel == "" {
		chatModel = os.Getenv("AI_CHAT_MODEL")
	}
	if endpoint != "" {
		apiKey := os.Getenv("AI_API_KEY")
		embEndpoint := cfg.AIEmbeddingEndpoint
		if embEndpoint == "" { embEndpoint = os.Getenv("AI_EMBEDDING_ENDPOINT") }
		aiClient = ai.NewClient(endpoint, embEndpoint, model, chatModel, apiKey)
	}
}

func main() {
	initAI()

	if len(os.Args) < 2 {
		fmt.Println("AIPM CLI — AI Project Manager")
		fmt.Println("Usage: aipmc <command> [args...]")
		fmt.Println("Run 'aipmc help' for full command list.")
		os.Exit(0)
	}

	cmd := os.Args[1]

	switch cmd {
	case "init":
		path, err := pmdb.Bootstrap()
		if err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
		writeSkillFile()
		fmt.Printf("Initialized .pmai at %s\n", filepath.Dir(filepath.Dir(path)))
		// Auto-configure MCP for all platforms
		if err := setupMCP("all"); err != nil {
			fmt.Fprintf(os.Stderr, "MCP setup skipped: %v (run 'aipmc setup' manually)\n", err)
		}
		return
	case "help":
		cli.PrintHelp()
		return
	case "setup":
		if len(os.Args) < 3 {
			// No platform specified — show available platforms
			fmt.Println("Please specify a platform to configure.")
			fmt.Println()
			listPlatforms()
			os.Exit(0)
		}
		target := os.Args[2]
		// Resolve short name / alias to full platform name
		resolved, err := resolvePlatform(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unknown platform: %s\n\n", target)
			listPlatforms()
			os.Exit(1)
		}
		if err := setupMCP(resolved); err != nil {
			fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
			os.Exit(1)
		}
		// Also setup hooks for platforms that support them
		if resolved == "Claude Code" || target == "claude" || resolved == "Gemini CLI" || target == "gemini" || resolved == "Codex (OpenAI)" || target == "codex" {
			// Auto-detect binary path and configure Claude Code / Gemini hooks
			if err := hook.SetupHooksCmd(resolveCommandPath(), resolved); err != nil {
				fmt.Fprintf(os.Stderr, "hook setup failed: %v\n", err)
			}
		}
		return
	case "log":
		role := ""
		source := ""
		content := ""
		sid := ""
		fromStdin := false
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--role": if i+1 < len(args) { role = args[i+1]; i++ }
			case "--source": if i+1 < len(args) { source = args[i+1]; i++ }
			case "--content": if i+1 < len(args) { content = args[i+1]; i++ }
			case "--session": if i+1 < len(args) { sid = args[i+1]; i++ }
			case "--stdin": fromStdin = true
			}
		}
		if fromStdin {
			data, _ := io.ReadAll(os.Stdin)
			content = string(data)
			// Strip trailing JSON artifacts (leftover from hook sed parsing)
			for _, suffix := range []string{
				`","background_tasks`,
				`","stop_hook_active`,
				`","session_crons`,
				`","hook_event_name`,
			} {
				if idx := strings.Index(content, suffix); idx > 0 {
					content = content[:idx] + `"}`
					break
				}
			}
			content = strings.TrimSuffix(content, `"}`)
			content = strings.TrimSuffix(content, `}`)
		}
		if role == "" || content == "" {
			fmt.Fprintln(os.Stderr, "Usage: aipmc log --role <user|assistant> --source <name> (--content <text> | --stdin) [--session <id>]")
			os.Exit(1)
		}
		r, err := store.LogDiscussion(sid, role, source, content, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "log error: %v\n", err)
			os.Exit(1)
		}
		preview := content
		if len([]rune(preview)) > 80 {
			preview = string([]rune(preview)[:80])
		}
		fmt.Printf("logged %s [%s][%s] %s\n", r["id"].(string), role, source, preview)
		return
	case "embed":
		n := 0 // 0 = all
		if len(os.Args) > 2 { fmt.Sscanf(os.Args[2], "%d", &n) }
		count, err := embedDiscussions(n)
		if err != nil { fmt.Fprintf(os.Stderr, "embed error: %v\n", err); os.Exit(1) }
		fmt.Printf("embedded %d discussions\n", count)
		return
	case "wait":
		waitForTurnCmd(os.Args[2:])
		return
	case "hook-process":
		hook.ProcessClaudeHook()
		return
	case "hook-gemini":
		hook.ProcessGeminiHook()
		return
	case "hook-codex":
		hook.ProcessCodexHook()
		return
	case "mcp":
		server := mcp.NewServer(aiClient,
			searchProjectContext,
			func(q string, l int) interface{} {
				hits := searchFTS5(q, l)
				if hits == nil { return nil }
				out := make([]map[string]interface{}, len(hits))
				for i, h := range hits {
					out[i] = map[string]interface{}{"type": h.Type, "id": h.ID, "title": h.Title, "status": h.Status, "score": h.Score, "command": h.Command}
				}
				return out
			},
			func(q string) interface{} {
				hits := searchLinear(q)
				out := make([]map[string]interface{}, len(hits))
				for i, h := range hits {
					out[i] = map[string]interface{}{"type": h.Type, "id": h.ID, "title": h.Title, "status": h.Status, "score": h.Score, "command": h.Command}
				}
				return out
			},
			func(q string, l int, hits interface{}) interface{} {
				if raw, ok := hits.([]map[string]interface{}); ok {
					reranked := aiSearchRerank(q, l, searchHitFromMaps(raw))
					out := make([]map[string]interface{}, len(reranked))
					for i, h := range reranked {
						out[i] = map[string]interface{}{"type": h.Type, "id": h.ID, "title": h.Title, "status": h.Status, "score": h.Score, "command": h.Command}
					}
					return out
				}
				return nil
			},
			searchDiscussions,
		)
		if err := server.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
			os.Exit(1)
		}
		return
	case "web":
		port := 0
		host := ""
		for i := 2; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--port", "-p":
				if i+1 < len(os.Args) { fmt.Sscanf(os.Args[i+1], "%d", &port); i++ }
			case "--host", "-h":
				if i+1 < len(os.Args) { host = os.Args[i+1]; i++ }
			}
		}
		if host == "" { host = "127.0.0.1" }
		if port == 0 { port = 8720 }
		staticFS, err := fs.Sub(uiFS, "frontend/dist")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load embedded UI: %v\n", err)
			os.Exit(1)
		}
		srv := web.NewServer(staticFS, handleAPIHandler(), host, port)
		srv.Listen()
		return
	case "chat":
		runChat()
		return
	}

	var rawArgs []string
	subcmd := ""
	if len(os.Args) > 2 {
		subcmd = os.Args[2]
		rawArgs = os.Args[3:]
	}
	args := cli.ParseArgs(rawArgs)

	switch cmd {
	case "search":
		query := os.Args[2]
		limit := args.Int("limit", 8)
		cli.PrintJSON(searchProjectContext(query, limit))
	case "status":
		cli.PrintJSON(getStatusSnapshot())
	case "start":
		cli.PrintJSON(buildAgentStartPacket())
	case "next":
		cli.PrintJSON(buildNextActionPacket())
	case "context":
		cli.PrintJSON(buildContextPack())
	case "analyze":
		cli.PrintJSON(analyze.RunFullAnalysis())
	case "briefing":
		fmt.Println(analyze.BuildBriefing(aiClient))
	case "inbox":
		cli.PrintJSON(getInboxSummary())
	case "doctor":
		dbPath, _ := pmdb.FindPath()
		cli.PrintJSON(runDoctor(dbPath))
	case "info":
		dbPath, _ := pmdb.FindPath()
		cli.RunInfo(dbPath)
	case "task":
		dispatchTask(subcmd, args)
	case "commit":
		dispatchCommit(subcmd, args)
	case "plan":
		dispatchPlan(subcmd, args)
	case "bug":
		dispatchBug(subcmd, args)
	case "decision":
		dispatchDecision(subcmd, args)
	case "idea":
		dispatchIdea(subcmd, args)
	case "roadmap":
		dispatchRoadmap(subcmd, args)
	case "principle":
		dispatchPrinciple(subcmd, args)
	case "link":
		dispatchLink(subcmd, args)
	case "vision":
		dispatchVision(subcmd, args)
	case "daily":
		dispatchDaily(subcmd, args)
	case "session":
		dispatchSession(subcmd, args)
	case "docs":
		dispatchDocs(subcmd, args)
	case "canon":
		dispatchCanon(subcmd, args)
	case "code":
		dispatchCode(subcmd, args)
	case "event":
		dispatchEvent(subcmd, args)
	case "feedback":
		dispatchFeedback(subcmd, args)
	case "thread":
		dispatchThread(subcmd, args)
	case "brief":
		dispatchBrief(subcmd, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func writeSkillFile() {
	dir, err := pmdb.RuntimeDir()
	if err != nil {
		dir, _ = os.Getwd()
	}
	skillDir := filepath.Join(dir, "..", ".claude", "skills")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "pmai.md"), []byte(skillMD), 0644)
}

// handleAPIHandler wraps handleAPI as an http.Handler.
func handleAPIHandler() http.Handler {
	return http.HandlerFunc(handleAPI)
}

func runDoctor(dbPath string) map[string]any {
	problems := []string{}
	if dbPath == "" {
		problems = append(problems, "No .pmai directory found. Run aipmc init first.")
	} else {
		db, err := pmdb.Open()
		if err != nil {
			problems = append(problems, fmt.Sprintf("Cannot open database: %v", err))
		} else {
			db.Close()
		}
	}
	return map[string]any{"ok": len(problems) == 0, "problems": problems, "db_path": dbPath, "binary": os.Args[0]}
}

// ── chat command ──────────────────────────────────────────────────────

func runChat() {
	if aiClient == nil || !aiClient.Enabled() {
		fmt.Fprintln(os.Stderr, "AI 未配置。请设置以下环境变量:")
		fmt.Fprintln(os.Stderr, "  AI_ENDPOINT   — LLM API 地址")
		fmt.Fprintln(os.Stderr, "  AI_MODEL      — 模型名称（或 AI_CHAT_MODEL）")
		fmt.Fprintln(os.Stderr, "  AI_API_KEY    — API 密钥（如需要）")
		os.Exit(1)
	}

	// Resolve project root (parent of .pmai/)
	runtimeDir, err := pmdb.RuntimeDir()
	workDir := "."
	if err == nil && runtimeDir != "" {
		workDir = filepath.Dir(runtimeDir)
	}

	// Build agent
	a := agent.New(aiClient, workDir)

	// Resolve session
	sessionDir := agent.SessionDir(workDir)

	// Check for --new flag or --session flag
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

	// Try to load existing session
	if !newSession {
		if sessionID != "" {
			// Load by ID
			sessPath := filepath.Join(sessionDir, sessionID+".json")
			if s, err := agent.LoadSession(sessPath); err == nil {
				sess = s
			}
		} else {
			// Load the most recent session
			sess = loadLatestSession(sessionDir)
		}
	}

	if sess == nil {
		sess = agent.NewSession()
	}

	// Save path
	sessPath := filepath.Join(sessionDir, sess.ID+".json")

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║  AIPM Coding Agent                       ║")
	fmt.Printf("║  Session: %s               ║\n", sess.ID)
	fmt.Println("║  /exit 退出  /new 新会话  /history 历史  ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()

	// If resuming, show recent context
	if len(sess.Events) > 0 {
		recent := sess.Events
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
				resultPreview := truncateLine(e.ToolResult, 80)
				fmt.Printf("  ← %s\n", resultPreview)
			}
		}
		fmt.Println()
	}

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

		// Handle slash commands
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
			fmt.Println()
			for _, e := range sess.Events {
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
			continue
		}

		// Run agent
		response, err := a.Run(sess, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			continue
		}

		fmt.Println()
		fmt.Println(response)
		fmt.Println()

		// Save after each turn
		sess.Save(sessPath)
	}

	// Save on Ctrl+C / EOF
	sess.Save(sessPath)
	fmt.Printf("\n会话已保存: %s\n", sessPath)
}

// loadLatestSession returns the most recently modified session from sessionDir.
func loadLatestSession(dir string) *agent.Session {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var latest string
	var latestTime int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			latest = e.Name()
		}
	}
	if latest == "" {
		return nil
	}
	s, err := agent.LoadSession(filepath.Join(dir, latest))
	if err != nil {
		return nil
	}
	return s
}

// truncateLine truncates s to maxLen runes.
func truncateLine(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// searchHitFromMaps converts []map[string]interface{} to []searchHit.
// Used as a bridge between mcp package's generic types and root's concrete searchHit type.
func searchHitFromMaps(maps []map[string]interface{}) []searchHit {
	out := make([]searchHit, len(maps))
	for i, m := range maps {
		out[i] = searchHit{
			Type:    strVal(m["type"]),
			ID:      strVal(m["id"]),
			Title:   strVal(m["title"]),
			Status:  strVal(m["status"]),
			Score:   intVal(m["score"]),
			Command: strVal(m["command"]),
		}
	}
	return out
}

func strVal(v interface{}) string {
	if s, ok := v.(string); ok { return s }
	return ""
}

func intVal(v interface{}) int {
	switch n := v.(type) {
	case int: return n
	case float64: return int(n)
	default: return 0
	}
}
