package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"aipmc/ai"
	"aipmc/cli"
)

// aiClient is the global AI client, initialized in main().
// nil when AI is not configured — all AI-dependent code paths
// gracefully degrade.
var aiClient *ai.Client

func initAI() {
	cfg := loadConfig()
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
		path, err := bootstrapDB()
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
			if err := setupHooksCmd(resolved); err != nil {
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
		r, err := logDiscussion(sid, role, source, content, "")
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
		processClaudeHook()
		return
	case "hook-gemini":
		processGeminiHook()
		return
	case "hook-codex":
		processCodexHook()
		return
	case "mcp":
		server := newMCPServer(aiClient)
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
		runWebServer(port, host)
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
		cli.PrintJSON(runFullAnalysis())
	case "briefing":
		fmt.Println(BuildBriefing())
	case "inbox":
		cli.PrintJSON(getInboxSummary())
	case "doctor":
		dbPath, _ := findDBPath()
		cli.PrintJSON(runDoctor(dbPath))
	case "info":
		dbPath, _ := findDBPath()
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
	dir, err := findRuntimeDir()
	if err != nil {
		dir, _ = os.Getwd()
	}
	skillDir := filepath.Join(dir, "..", ".claude", "skills")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "pmai.md"), []byte(skillMD), 0644)
}

func runDoctor(dbPath string) map[string]any {
	problems := []string{}
	if dbPath == "" {
		problems = append(problems, "No .pmai directory found. Run aipmc init first.")
	} else {
		db, err := openDB()
		if err != nil {
			problems = append(problems, fmt.Sprintf("Cannot open database: %v", err))
		} else {
			db.Close()
		}
	}
	return map[string]any{"ok": len(problems) == 0, "problems": problems, "db_path": dbPath, "binary": os.Args[0]}
}
