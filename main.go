package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
		gcfg := pmdb.LoadGlobalConfig()
		// Build reverse proxy to proxy port for backward-compatible /__proxy/* forwarding
		var proxyHandler http.Handler
		if gcfg.ProxyPort > 0 {
			proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", gcfg.ProxyPort))
			proxyHandler = httputil.NewSingleHostReverseProxy(proxyURL)
		}
		srv := web.NewServer(staticFS, newAPIHandler(), host, port, proxyHandler)
		srv.Listen()
		return
	case "serve":
		os.Exit(serveCommand())
	case "chat":
		chatcli.Run(application)
		return
	case "proxy":
		gcfg := pmdb.LoadGlobalConfig()
		if err := proxy.Run(proxy.Options{
			Port:         gcfg.ProxyPort,
			UpstreamURL:  gcfg.UpstreamURL,
			UpstreamKey:  os.Getenv("UPSTREAM_KEY"),
			Model:        gcfg.ProxyModel,
			LogDir:       gcfg.ProxyLogDir,
			AnthropicURL: gcfg.AnthropicURL,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "proxy error: %v\n", err)
			os.Exit(1)
		}
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

func serveCommand() int {
	// Parse flags
	noBrowser := false
	noProxy := false
	for _, a := range os.Args {
		switch a {
		case "--no-browser":
			noBrowser = true
		case "--no-proxy":
			noProxy = true
		}
	}

	gcfg := pmdb.LoadGlobalConfig()
	exe, _ := os.Executable()

	// Step 1: Ensure proxy is running
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", gcfg.ProxyPort)
	proxyRunning := false
	if conn, err := net.DialTimeout("tcp", proxyAddr, 500*time.Millisecond); err == nil {
		conn.Close()
		// Verify it's actually our proxy
		resp, err := http.Get(fmt.Sprintf("http://%s/__proxy/status", proxyAddr))
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			proxyRunning = true
		}
	}

	if !proxyRunning && !noProxy {
		fmt.Printf("→ Proxy 未运行，启动中...\n")
		cmd := exec.Command(exe, "proxy")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "无法启动 proxy: %v\n", err)
			return 1
		}
		// Wait for proxy to be ready
		for i := 0; i < 30; i++ {
			time.Sleep(200 * time.Millisecond)
			resp, err := http.Get(fmt.Sprintf("http://%s/__proxy/status", proxyAddr))
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				proxyRunning = true
				break
			}
		}
		if !proxyRunning {
			fmt.Fprintf(os.Stderr, "Proxy 6s 内未就绪\n")
			return 1
		}
		fmt.Printf("✓ Proxy 就绪 :%d\n", gcfg.ProxyPort)
	} else if !noProxy {
		fmt.Printf("✓ Proxy 已在运行 :%d\n", gcfg.ProxyPort)
	} else {
		fmt.Printf("⚠ Proxy 未运行 (--no-proxy)，Agent 请求将无法转发\n")
	}

	// Step 2: Allocate web port
	cfg := pmdb.LoadConfig()
	webPort := cfg.WebPort
	if webPort == 0 {
		webPort = 8720
	}
	for i := 0; i < 100; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", webPort))
		if err == nil {
			ln.Close()
			break
		}
		webPort++
	}

	// Step 3: Register project
	projectPath, _ := os.Getwd()
	pmdb.SaveProject(pmdb.ProjectEntry{
		Path:      projectPath,
		Name:      filepath.Base(projectPath),
		WebPort:   webPort,
		ProxyPort: gcfg.ProxyPort,
	})

	// Step 4: Create proxy handler for embedding
	staticFS, err := fs.Sub(uiFS, "frontend/dist")
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载 UI 失败: %v\n", err)
		return 1
	}
	proxyHandler := proxy.NewHandler(proxy.Options{
		Port:         gcfg.ProxyPort,
		UpstreamURL:  gcfg.UpstreamURL,
		UpstreamKey:  os.Getenv("UPSTREAM_KEY"),
		Model:        gcfg.ProxyModel,
		LogDir:       gcfg.ProxyLogDir,
		AnthropicURL: gcfg.AnthropicURL,
	})
	srv := web.NewServer(staticFS, newAPIHandler(), "127.0.0.1", webPort, proxyHandler)

	// Step 5: Auto-open browser (unless --no-browser)
	if !noBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			url := fmt.Sprintf("http://127.0.0.1:%d", webPort)
			openBrowser(url)
		}()
	}

	// Step 6: Start web server (blocking)
	fmt.Printf("✓ Web 启动 :%d → http://127.0.0.1:%d\n", webPort, webPort)
	fmt.Printf("✓ 项目 %s 已注册\n", filepath.Base(projectPath))
	if err := srv.Listen(); err != nil {
		fmt.Fprintf(os.Stderr, "web error: %v\n", err)
		return 1
	}
	return 0
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
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
