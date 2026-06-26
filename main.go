package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	apipkg "aipmc/api"
	"aipmc/app"
	"aipmc/analyze"
	"aipmc/chatcli"
	"aipmc/cli"
	pmdb "aipmc/db"
	"aipmc/hook"
	"aipmc/proxy"
	"aipmc/search"
	"aipmc/store"
	"aipmc/web"
)

//go:embed frontend/dist
var uiFS embed.FS

var application = app.New()

func main() {
	application.ReloadAI()

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
		hookPlatforms := []string{}
		if target == "all" || resolved == "all" {
			hookPlatforms = []string{"Claude Code", "Gemini CLI", "Codex (OpenAI)", "OpenCode", "Cursor"}
		} else if resolved == "Claude Code" || target == "claude" ||
			resolved == "Gemini CLI" || target == "gemini" ||
			resolved == "Codex (OpenAI)" || target == "codex" ||
			resolved == "OpenCode" || target == "opencode" ||
			resolved == "Cursor" || target == "cursor" {
			hookPlatforms = []string{resolved}
		}
		for _, platform := range hookPlatforms {
			if err := hook.SetupHooksCmd(resolveCommandPath(), platform); err != nil {
				fmt.Fprintf(os.Stderr, "hook setup failed (%s): %v\n", platform, err)
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
		count, err := application.EmbedDiscussions(n)
		if err != nil { fmt.Fprintf(os.Stderr, "embed error: %v\n", err); os.Exit(1) }
		fmt.Printf("embedded %d discussions\n", count)
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
	case "hook-opencode":
		hook.ProcessOpencodeHook()
		return
	case "hook-cursor":
		hook.ProcessCursorHook()
		return
	case "mcp":
		if err := application.RunMCP(); err != nil {
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
		srv := web.NewServer(staticFS, newAPIHandler(), host, port)
		srv.Listen()
		return
	case "chat":
		chatcli.Run(application)
		return
	case "proxy":
		gcfg := pmdb.LoadGlobalConfig()
		proxy.Run(proxy.Options{
			Port:        gcfg.ProxyPort,
			UpstreamURL: gcfg.UpstreamURL,
			UpstreamKey: os.Getenv("UPSTREAM_KEY"),
			Model:       gcfg.ProxyModel,
			LogDir:      gcfg.ProxyLogDir,
		})
		return
	case "agent":
		if len(os.Args) < 3 {
			fmt.Println("Usage: aipmc agent <claude|gemini|codex>")
			fmt.Println()
			fmt.Println("Launch an AI coding agent pre-configured to use aipmc proxy.")
			fmt.Println("Proxy must be running: aipmc proxy")
			os.Exit(1)
		}
		runAgent(os.Args[2])
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
		cli.PrintJSON(search.ProjectContext(query, limit))
	case "status":
		cli.PrintJSON(application.StatusSnapshot())
	case "start":
		cli.PrintJSON(application.AgentStartPacket())
	case "next":
		cli.PrintJSON(application.NextActionPacket())
	case "context":
		cli.PrintJSON(application.ContextPack())
	case "analyze":
		cli.PrintJSON(analyze.RunFullAnalysis())
	case "review":
		reviewSub := subcmd
		reviewRaw := rawArgs
		if strings.HasPrefix(reviewSub, "--") {
			reviewRaw = append([]string{reviewSub}, rawArgs...)
			reviewSub = ""
		}
		dispatchReview(reviewSub, cli.ParseArgs(reviewRaw))
	case "briefing":
		fmt.Println(analyze.BuildBriefing(application.AI))
	case "inbox":
		cli.PrintJSON(application.InboxSummary())
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
	case "reconcile":
		dispatchReconcile(subcmd, args)
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

func newAPIHandler() http.Handler {
	return apipkg.New(apipkg.Deps{App: application}).Handler()
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

func runAgent(name string) {
	gcfg := pmdb.LoadGlobalConfig()
	port := gcfg.ProxyPort
	model := gcfg.ProxyModel
	if model == "" {
		model = "gpt-4o"
	}
	proxyURL := fmt.Sprintf("http://localhost:%d", port)

	var cmd *exec.Cmd
	switch strings.ToLower(name) {
	case "claude", "claude-code", "cc":
		cmd = exec.Command("claude")
		cmd.Env = append(os.Environ(),
			"ANTHROPIC_BASE_URL="+proxyURL,
			"ANTHROPIC_AUTH_TOKEN=local",
			"ANTHROPIC_MODEL="+model,
		)
	case "gemini", "gemini-cli", "gc":
		cmd = exec.Command("gemini")
		cmd.Env = append(os.Environ(),
			"GOOGLE_GEMINI_BASE_URL="+proxyURL,
			"GEMINI_API_KEY=local",
		)
	case "codex", "openai-codex":
		cmd = exec.Command("codex", "-p", "proxy")
	default:
		fmt.Fprintf(os.Stderr, "unknown agent: %s\n", name)
		fmt.Fprintln(os.Stderr, "available: claude, gemini, codex")
		os.Exit(1)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
