package main

import (
	"bufio"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"aipmc/ai"
	"aipmc/analyze"
	apipkg "aipmc/api"
	"aipmc/app"
	"aipmc/chatcli"
	"aipmc/cli"
	pmdb "aipmc/db"
	"aipmc/eval"
	"aipmc/hook"
	"aipmc/proxy"
	"aipmc/search"
	"aipmc/session"
	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
)

//go:embed frontend/dist
var uiFS embed.FS

var application = app.New()

func main() {
	application.ReloadAI()

	if len(os.Args) < 2 {
		fmt.Println("AIPMC CLI — AI Project Manager")
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
		// Auto-install post-commit hook (non-fatal if not in git repo)
		if err := hook.InstallPostCommitHook(); err != nil {
			fmt.Fprintf(os.Stderr, "note: post-commit hook skipped (%v)\n", err)
		}
		return
	case "help":
		cli.PrintHelp()
		return
	case "setup":
		if len(os.Args) < 3 {
			fmt.Println("Please specify a platform to configure.")
			fmt.Println()
			listPlatforms()
			os.Exit(0)
		}
		target := os.Args[2]
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
			case "--role":
				if i+1 < len(args) {
					role = args[i+1]
					i++
				}
			case "--source":
				if i+1 < len(args) {
					source = args[i+1]
					i++
				}
			case "--content":
				if i+1 < len(args) {
					content = args[i+1]
					i++
				}
			case "--session":
				if i+1 < len(args) {
					sid = args[i+1]
					i++
				}
			case "--stdin":
				fromStdin = true
			}
		}
		if fromStdin {
			data, _ := io.ReadAll(os.Stdin)
			content = string(data)
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
		id, _ := r["id"].(string)
		if id == "" {
			id = "-" // spool 兜底 dropped 路径无 id（事件未捕获，4f65c89 实证 panic 修复）
		}
		fmt.Printf("logged %s [%s][%s] %s\n", id, role, source, preview)
		return
	case "embed":
		n := 0
		if len(os.Args) > 2 {
			fmt.Sscanf(os.Args[2], "%d", &n)
		}
		count, err := application.EmbedDiscussions(n)
		if err != nil {
			fmt.Fprintf(os.Stderr, "embed error: %v\n", err)
			os.Exit(1)
		}
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
	case "hook-post-commit":
		hook.ProcessPostCommitHook()
		return
	case "hook":
		if len(os.Args) < 3 {
			fmt.Println("usage: aipmc hook <install|uninstall>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "install":
			if err := hook.InstallPostCommitHook(); err != nil {
				fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
				os.Exit(1)
			}
		case "uninstall":
			if err := hook.UninstallPostCommitHook(); err != nil {
				fmt.Fprintf(os.Stderr, "uninstall failed: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Printf("unknown hook subcommand: %s\n", os.Args[2])
			fmt.Println("usage: aipmc hook <install|uninstall>")
			os.Exit(1)
		}
		return
	case "mcp":
		if err := application.RunMCP(); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
			os.Exit(1)
		}
		return
	case "serve":
		os.Exit(serveCommand())
	case "chat":
		chatcli.Run(application)
		return
	case "proxy":
		profile := "default"
		// Support aipmc proxy --profile <name> or aipmc proxy <profile> (positional fallback)
		for idx := 2; idx < len(os.Args); idx++ {
			if (os.Args[idx] == "--profile" || os.Args[idx] == "-p") && idx+1 < len(os.Args) {
				profile = os.Args[idx+1]
				idx++
			} else if idx == 2 && !strings.HasPrefix(os.Args[idx], "-") {
				// Positional fallback: aipmc proxy <profile>
				profile = os.Args[idx]
			}
		}
		loadCredentialsOnStartup(profile)
		// 启动时补写 discussion spool（P0 捕获缺口兜底，bug-20260826-154305-941881）
		if err := store.FlushDiscussionSpool(); err != nil {
			fmt.Fprintf(os.Stderr, "discussion spool flush warning: %v\n", err)
		}
		gcfg := pmdb.LoadGlobalConfig()
		if err := proxy.Run(proxy.Options{
			Port:         gcfg.ProxyPort,
			BindAddr:     gcfg.ProxyBindAddr,
			UpstreamURL:  gcfg.UpstreamURL,
			Model:        gcfg.ProxyModel,
			LogDir:       gcfg.ProxyLogDir,
			AnthropicURL: gcfg.AnthropicURL,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "proxy error: %v\n", err)
			os.Exit(1)
		}
		return
	case "models":
		subcmd := ""
		var rawArgs []string
		if len(os.Args) > 2 {
			subcmd = os.Args[2]
			rawArgs = os.Args[3:]
		}
		dispatchModels(subcmd, cli.ParseArgs(rawArgs))
		return
	case "key":
		dispatchKey(os.Args)
		return
	case "eval":
		// EVAL_PIPELINE：M1-M5 归因提取器（headless，无 LLM 依赖）。
		// Usage: aipmc eval [--since 30d] [--kind attribution] [--log <path>] [--no-fail]
		//        aipmc eval process --session <id> [--fix-hash <prefix>]（P0a1a，T1-T5）
		//        aipmc eval acceptance --session <id> [--fix-hash <prefix>] [--hollow-session <id>] [--anchor-msg <msg>] [--claim <title>]（P0a1b，T9）
		//        aipmc eval p0a2 --session <id> [--fix-hash <prefix>]（P0a2 方向性报告：主动触发/静态可核对/P3 计数基线）
		sinceDays := 30
		kind := "attribution"
		logPath := ""
		noFail := false
		sessionID := ""
		fixHash := ""
		dbPath := ""
		anchorMsg := ""
		claim := ""
		hollowSession := ""
		confirmSessions := ""
		l2Mode := false
		l2Max := 0
		l2Timeout := 90 // 秒；单次 LLM 确认硬超时（Claude P1b 二轮审核：无超时 LLM 挂起会无限等待）
		l2Sample := 0   // P1c 分层抽样：每层（按候选日期）抽样数，替代前 N 条
		probeStart := ""
		probeEnd := ""
		probeLabel := ""
		probeRuns := 3
		limit := 10
		shadowPath := ""
		backfillPath := ""
		raw := os.Args[2:]
		// 位置参数 kind（aipmc eval process / acceptance）：与 --kind 等价（usage 声明形式）
		if len(raw) > 0 && raw[0] != "--since" && raw[0] != "--kind" && raw[0] != "--log" && !strings.HasPrefix(raw[0], "--") {
			kind = raw[0]
			raw = raw[1:]
		}
		for i := 0; i < len(raw); i++ {
			switch {
			case raw[i] == "--since" && i+1 < len(raw):
				fmt.Sscanf(raw[i+1], "%dd", &sinceDays)
				i++
			case raw[i] == "--kind" && i+1 < len(raw):
				kind = raw[i+1]
				i++
			case raw[i] == "--log" && i+1 < len(raw):
				logPath = raw[i+1]
				i++
			case raw[i] == "--no-fail":
				noFail = true
			case raw[i] == "--session" && i+1 < len(raw):
				sessionID = raw[i+1]
				i++
			case raw[i] == "--fix-hash" && i+1 < len(raw):
				fixHash = raw[i+1]
				i++
			case raw[i] == "--db" && i+1 < len(raw):
				dbPath = raw[i+1]
				i++
			case raw[i] == "--anchor-msg" && i+1 < len(raw):
				anchorMsg = raw[i+1]
				i++
			case raw[i] == "--claim" && i+1 < len(raw):
				claim = raw[i+1]
				i++
			case raw[i] == "--hollow-session" && i+1 < len(raw):
				hollowSession = raw[i+1]
				i++
			case raw[i] == "--confirm-sessions" && i+1 < len(raw):
				confirmSessions = raw[i+1]
				i++
			case raw[i] == "--l2":
				l2Mode = true
			case raw[i] == "--l2-max" && i+1 < len(raw):
				fmt.Sscanf(raw[i+1], "%d", &l2Max)
				i++
			case raw[i] == "--l2-timeout" && i+1 < len(raw):
				fmt.Sscanf(raw[i+1], "%d", &l2Timeout)
				i++
			case raw[i] == "--l2-sample" && i+1 < len(raw):
				fmt.Sscanf(raw[i+1], "%d", &l2Sample)
				i++
			case raw[i] == "--start" && i+1 < len(raw):
				probeStart = raw[i+1]
				i++
			case raw[i] == "--end" && i+1 < len(raw):
				probeEnd = raw[i+1]
				i++
			case raw[i] == "--label" && i+1 < len(raw):
				probeLabel = raw[i+1]
				i++
			case raw[i] == "--runs" && i+1 < len(raw):
				fmt.Sscanf(raw[i+1], "%d", &probeRuns)
				i++
			case raw[i] == "--limit" && i+1 < len(raw):
				fmt.Sscanf(raw[i+1], "%d", &limit)
				i++
			case raw[i] == "--shadow" && i+1 < len(raw):
				shadowPath = raw[i+1]
				i++
			case raw[i] == "--backfill" && i+1 < len(raw):
				backfillPath = raw[i+1]
				i++
			}
		}
		if logPath == "" {
			logPath = filepath.Join(os.Getenv("HOME"), ".aipmc", "logs", "aipmc.log")
		}
		db, err := pmdb.Open()
		if dbPath != "" {
			if db != nil {
				db.Close()
			}
			db, err = sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "eval: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
		since := time.Now().AddDate(0, 0, -sinceDays)
		switch kind {
		case "attribution":
			// 8/26 C2：--db 指定其他库时，M1a 过滤基准用该库所在项目而非 cwd。
			if dbPath != "" {
				if root := eval.ProjectRootFromDBPath(dbPath); root != "" {
					eval.SetProjectRoot(root)
				}
			}
			rep, err := eval.BuildAttribution(db, logPath, since)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval attribution: %v\n", err)
				os.Exit(1)
			}
			// S4 核验项 4：JSON + 人类可读双输出（人类可读对齐 metrics.go printRow 风格）
			fmt.Print(eval.FormatHuman(rep))
			fmt.Println()
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
			// M1a 观测断裂告警：write_err>0 或任一 agent 对账<1.0 且差量>0 → 非零退出码
			// （8/18 攻击性审核补充：仅渲染 ❌ 时无人读输出则告警无意义）。
			// --no-fail：已知/可解释的窗口残余（如历史测试污染滑动清除期）时显式容忍，
			// 输出仍渲染 ❌ 但不影响退出码（8/18 Claude 审核建议，避免每日误报阻断）。
			alert := rep.WriteErr > 0
			for _, a := range rep.ByAgent {
				if a.M1.LogInject > 0 && a.M1.Reconcile < 1.0 {
					alert = true
				}
			}
			if alert && !noFail {
				os.Exit(1)
			}
		case "process":
			// P0a1a T1-T5：时段边界 + d628b7a 关联 + 反馈识别 + 检索三分类 + 死循环候选
			// P1b --l2：L2 五任务确认编排（候选 → LLM 确认 → 回填；LLM 不可用降级标注）
			if sessionID == "" {
				fmt.Fprintf(os.Stderr, "eval process: 需要 --session <id>\n")
				os.Exit(1)
			}
			rep, err := eval.BuildProcessReport(db, sessionID, fixHash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval process: %v\n", err)
				os.Exit(1)
			}
			if l2Mode {
				projectPath, _ := os.Getwd()
				if dbPath != "" {
					if root := eval.ProjectRootFromDBPath(dbPath); root != "" {
						projectPath = root
					}
				}
				turns, err := eval.BuildTurns(db, sessionID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "eval process L2 回合化: %v\n", err)
					os.Exit(1)
				}
				// LLM 确认器：项目自身 AI 配置（.pmai/config.json，复用 SummarizerFor 的
				// 模型注册表 + 凭据解析）；未配置 → 降级标注「L2 未运行」。
				var confirmer eval.L2Confirmer
				if s := application.SummarizerFor(projectPath); s != nil {
					if c, ok := s.(interface {
						SummarizeJSON(text, instruction string) (string, error)
					}); ok {
						confirmer = &eval.L2Client{Summarizer: c, Timeout: time.Duration(l2Timeout) * time.Second}
					}
				}
				rep.L2, err = eval.RunL2Confirmations(confirmer, rep, turns, eval.L2RunOptions{MaxPerTask: l2Max, SamplePerLayer: l2Sample})
				if err != nil {
					fmt.Fprintf(os.Stderr, "eval process L2: %v\n", err)
					os.Exit(1)
				}
			}
			fmt.Print(eval.FormatProcessHuman(rep))
			fmt.Println()
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
		case "acceptance":
			// P0a1b T9：验收报告聚合（T1-T5 + T6-T8 → 验收①-③ 数据结果表）
			if sessionID == "" {
				fmt.Fprintf(os.Stderr, "eval acceptance: 需要 --session <id>\n")
				os.Exit(1)
			}
			var anchor *eval.AnchorTarget
			if anchorMsg != "" && claim != "" {
				anchor = &eval.AnchorTarget{SessionID: sessionID, UserMsg: anchorMsg, Claim: claim}
			}
			rep, err := eval.BuildAcceptanceReport(db, sessionID, fixHash, hollowSession, anchor)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval acceptance: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(eval.FormatAcceptanceHuman(rep))
			fmt.Println()
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
			// 验收 fail 行 → exit 1（与 attribution alert 语义对称）；--no-fail 逃生口
			fail := false
			for _, r := range rep.Acceptance {
				if strings.HasPrefix(r.Status, "fail") {
					fail = true
					break
				}
			}
			if fail && !noFail {
				os.Exit(1)
			}
		case "p0a2":
			// P0a2 方向性报告：主动触发（工具采用）/ 静态可核对 / P3 计数基线（重复验证点/自建记录利用）
			if sessionID == "" {
				fmt.Fprintf(os.Stderr, "eval p0a2: 需要 --session <id>\n")
				os.Exit(1)
			}
			rep, err := eval.BuildP0a2Report(db, sessionID, fixHash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval p0a2: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(eval.FormatP0a2Human(rep))
			fmt.Println()
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
		case "p0b":
			// P0b：对象级加深方向性（019ff89b）+ 候选→人工确认闭环（10 候选时段）
			// 用法：aipmc eval p0b --db <ed-db> --session <019ff89b session>
			//   --confirm-sessions <c0ad2534>,<01a013f3>
			if sessionID == "" {
				fmt.Fprintf(os.Stderr, "eval p0b: 需要 --session <deepen-session>\n")
				os.Exit(1)
			}
			var cs []string
			if confirmSessions != "" {
				for _, s := range strings.Split(confirmSessions, ",") {
					if s = strings.TrimSpace(s); s != "" {
						cs = append(cs, s)
					}
				}
			}
			rep, err := eval.BuildP0bReport(db, sessionID, cs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval p0b: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(eval.FormatP0bHuman(rep))
			fmt.Println()
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
		case "feedback":
			// B 线 P0（8/27 v13.1）：事后反馈检测器——实体引用未查询（F5 类强漏查）
			// + 数据源引用规范性，纯正则零语义，输出走 C2 线回填。
			// Usage: aipmc eval feedback [--since 30d] [--limit 10] [--db <path>] [--shadow <jsonl>] [--backfill <jsonl>]
			//   --shadow:  追加写入 C2 契约 shadow JSONL（session_id+timestamp 去重，P3 T2 shadow 接线）
			//   --backfill: 消费 shadow JSONL → 高价值强漏查（decision/plan/bug）回填 session_summaries（C2）
			gaps, err := eval.DetectFeedbackGaps(db, since.Format("2006-01-02T15:04:05"), limit)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval feedback: %v\n", err)
				os.Exit(1)
			}
			out, _ := json.MarshalIndent(gaps, "", "  ")
			fmt.Println(string(out))
			refs, misses, ds := 0, 0, 0
			for _, g := range gaps {
				refs += len(g.EntityRefs)
				misses += len(g.MissingQueries)
				ds += len(g.DataSourceRefs)
			}
			fmt.Printf("feedback: sessions=%d entity_refs=%d strong_missing_queries=%d data_source_refs=%d\n",
				len(gaps), refs, misses, ds)
			if shadowPath != "" {
				written, skipped, serr := eval.WriteFeedbackShadow(shadowPath, gaps)
				if serr != nil {
					fmt.Fprintf(os.Stderr, "eval feedback shadow: %v\n", serr)
					os.Exit(1)
				}
				fmt.Printf("feedback shadow: %s written=%d skipped_dup=%d\n", shadowPath, written, skipped)
			}
			if backfillPath != "" {
				sessions, refs, berr := eval.BackfillFeedback(backfillPath)
				if berr != nil {
					fmt.Fprintf(os.Stderr, "eval feedback backfill: %v\n", berr)
					os.Exit(1)
				}
				fmt.Printf("feedback backfill: sessions=%d refs=%d (高价值 decision/plan/bug 强漏查 → session_summaries)\n", sessions, refs)
			}
		case "l2-probe":
			// P1c 对抗样本 + 稳定性探测（§2.3 约束③）：已知健康时段 → deadloop_confirm
			// 跑 N 次，验证不误判 + LLM 判定漂移率。
			// Usage: aipmc eval l2-probe --session <id> --start <ts> --end <ts> [--label 说明] [--runs 3] [--db <path>] [--l2-timeout 90]
			if sessionID == "" || probeStart == "" || probeEnd == "" {
				fmt.Fprintf(os.Stderr, "eval l2-probe: 需要 --session <id> --start <ts> --end <ts>\n")
				os.Exit(1)
			}
			turns, err := eval.BuildTurns(db, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval l2-probe 回合化: %v\n", err)
				os.Exit(1)
			}
			start, err1 := time.Parse("2006-01-02T15:04:05", probeStart)
			end, err2 := time.Parse("2006-01-02T15:04:05", probeEnd)
			if err1 != nil || err2 != nil {
				fmt.Fprintf(os.Stderr, "eval l2-probe: 时间格式 YYYY-MM-DDTHH:MM:SS（err1=%v err2=%v）\n", err1, err2)
				os.Exit(1)
			}
			projectPath, _ := os.Getwd()
			if dbPath != "" {
				if root := eval.ProjectRootFromDBPath(dbPath); root != "" {
					projectPath = root
				}
			}
			var confirmer eval.L2Confirmer
			if s := application.SummarizerFor(projectPath); s != nil {
				if c, ok := s.(interface {
					SummarizeJSON(text, instruction string) (string, error)
				}); ok {
					confirmer = &eval.L2Client{Summarizer: c, Timeout: time.Duration(l2Timeout) * time.Second}
				}
			}
			probe, err := eval.ProbeHealthyWindow(confirmer, turns, start, end, probeLabel, probeRuns)
			if err != nil {
				fmt.Fprintf(os.Stderr, "eval l2-probe: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(eval.FormatProbeHuman(probe))
			fmt.Println()
			out, _ := json.MarshalIndent(probe, "", "  ")
			fmt.Println(string(out))
		default:
			fmt.Fprintf(os.Stderr, "eval: unknown kind %q (supported: attribution/process/acceptance/p0a2/p0b/l2-probe/feedback)\n", kind)
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
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aipmc search <query>")
			os.Exit(1)
		}
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
	case "metrics":
		dispatchMetrics(cli.ParseArgs(os.Args[2:]))
	case "review":
		reviewSub := subcmd
		reviewRaw := rawArgs
		if strings.HasPrefix(reviewSub, "--") {
			reviewRaw = append([]string{reviewSub}, rawArgs...)
			reviewSub = ""
		}
		dispatchReview(reviewSub, cli.ParseArgs(reviewRaw))
	case "briefing":
		b, _ := analyze.BuildBriefing(application.AI(), "")
		fmt.Println(b)
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
		// Treat "--image" etc. as CLI flags, not entity subcommands
		if strings.HasPrefix(subcmd, "--") {
			rawArgs = os.Args[2:]
			args = cli.ParseArgs(rawArgs)
			subcmd = ""
		}
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
	projectFlag := ""
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--project", "-p":
			if i+1 < len(os.Args) {
				projectFlag = os.Args[i+1]
				i++
			}
		case "--profile":
			if i+1 < len(os.Args) {
				os.Setenv("AIPMC_CREDENTIALS_PROFILE", os.Args[i+1])
				i++
			}
		}
	}

	// Auto-unlock credentials if env vars present (for scripting / automation)
	if profile := os.Getenv("AIPMC_CREDENTIALS_PROFILE"); profile != "" {
		if pass := os.Getenv("AIPMC_MASTER_PASSWORD"); pass != "" {
			loadCredentialsOnStartup(profile)
		}
	}

	gcfg := pmdb.LoadGlobalConfig()
	cwd, _ := os.Getwd()

	// Step 1: Determine project path and switch to it immediately
	projectPath := resolveProjectPath(projectFlag, cwd)
	if projectPath == "" {
		return 1
	}
	os.Chdir(projectPath)
	projectName := filepath.Base(projectPath)

	// Logging attribution: tag this process's log lines with the project and
	// write a BOOT banner so each log segment maps to a binary version+project.
	u.SetLogProject(projectName)
	u.LogShared("BOOT", "version=%s pid=%d go=%s", u.BuildVersion, os.Getpid(), runtime.Version())

	// Auto-initialize if project has no .pmai/ directory
	if _, err := os.Stat(filepath.Join(projectPath, ".pmai")); os.IsNotExist(err) {
		fmt.Printf("→ 项目尚未初始化，正在执行 aipmc init...\n")
		if _, err := pmdb.Bootstrap(); err != nil {
			fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
			return 1
		}
		fmt.Printf("✓ 项目已初始化\n")
	}

	// Step 2: Determine web port — projects.json > .pmai/config.json > default
	projects := pmdb.LoadProjects()
	webPort := 0
	if entry, ok := projects[projectPath]; ok && entry.WebPort > 0 {
		webPort = entry.WebPort
	}
	if webPort == 0 {
		cfg := pmdb.LoadConfig()
		webPort = cfg.WebPort
	}
	if webPort == 0 {
		webPort = 8720
	}

	// Step 3: Check for existing serve instance
	if isPortInUse(webPort) {
		if project, ok := checkExistingInstance(webPort); ok {
			if project == projectName {
				fmt.Printf("✓ Web 已运行 → http://127.0.0.1:%d (项目: %s)\n", webPort, project)
				return 0
			}
			fmt.Fprintf(os.Stderr, "端口 :%d 已被项目 %s 占用，请先停止该项目\n", webPort, project)
			return 1
		}
		if procName := portOwnerProcess(webPort); procName != "" {
			fmt.Fprintf(os.Stderr, "端口 :%d 被进程 %s 占用\n", webPort, procName)
		} else {
			fmt.Fprintf(os.Stderr, "端口 :%d 被占用，如果是刚关闭的 aipmc 进程，等待几秒后重试\n", webPort)
		}
		return 1
	}

	// Step 3b: Refuse duplicate instance of the same project on a different port
	// (P15/E2: 多实例并发写日志会稀释评估数据；历史上曾出现同项目双 web 实例)
	for path, entry := range pmdb.LoadProjects() {
		if path != projectPath || entry.WebPort == webPort || entry.WebPort <= 0 {
			continue
		}
		if isPortInUse(entry.WebPort) {
			if project, ok := checkExistingInstance(entry.WebPort); ok && project == projectName {
				fmt.Fprintf(os.Stderr, "项目 %s 已在 :%d 运行（http://127.0.0.1:%d），请先停止旧实例再启动，避免多实例并发写日志\n", projectName, entry.WebPort, entry.WebPort)
				return 1
			}
		}
	}

	// Step 4: Check proxy status
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", gcfg.ProxyPort)
	proxyRunning := false
	if conn, err := net.DialTimeout("tcp", proxyAddr, 500*time.Millisecond); err == nil {
		conn.Close()
		resp, err := http.Get(fmt.Sprintf("http://%s/__proxy/status", proxyAddr))
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			proxyRunning = true
		}
	}

	if proxyRunning {
		owner := portOwnerProcess(gcfg.ProxyPort)
		if owner != "" {
			fmt.Printf("✓ Proxy 已在运行 :%d (%s)\n", gcfg.ProxyPort, owner)
		} else {
			fmt.Printf("✓ Proxy 已在运行 :%d\n", gcfg.ProxyPort)
		}
	} else {
		fmt.Printf("⚠ Proxy 未运行 — 运行 `aipmc proxy` 或在 Web UI 中启动\n")
	}

	// Step 5: Register/update project
	pmdb.SaveProject(pmdb.ProjectEntry{
		Path:         projectPath,
		Name:         projectName,
		WebPort:      webPort,
		ProxyPort:    gcfg.ProxyPort,
		LastOpenedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// Step 6: Create web server
	staticFS, err := fs.Sub(uiFS, "frontend/dist")
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载 UI 失败: %v\n", err)
		return 1
	}
	proxyHandler := proxy.NewHandler(proxy.Options{
		Port:         gcfg.ProxyPort,
		BindAddr:     gcfg.ProxyBindAddr,
		UpstreamURL:  gcfg.UpstreamURL,
		Model:        gcfg.ProxyModel,
		LogDir:       gcfg.ProxyLogDir,
		AnthropicURL: gcfg.AnthropicURL,
	})
	srv := web.NewServer(staticFS, newAPIHandler(projectPath), "127.0.0.1", webPort, proxyHandler, projectName, projectPath)

	// Step 7: Start web server (blocking)
	fmt.Printf("✓ Web 启动 :%d → http://127.0.0.1:%d\n", webPort, webPort)
	fmt.Printf("✓ 项目 %s 已注册\n", projectName)
	// Start background session review pipeline (B1→L2→L3 auto-run) across all registered projects.
	// Each project's L2 summaries use that project's own AI model (its .pmai/config.json),
	// not the serve instance's home model — so a local model configured for one project
	// no longer gets used to summarize every project.
	if application.AI() != nil {
		var otherProjects []string
		for p := range pmdb.LoadProjects() {
			if p != projectPath {
				otherProjects = append(otherProjects, p)
			}
		}
		session.RunAuto(func(p string) ai.Summarizer {
			return application.SummarizerFor(p)
		}, 30*time.Minute, otherProjects)
	}

	if err := srv.Listen(); err != nil {
		fmt.Fprintf(os.Stderr, "web error: %v\n", err)
		return 1
	}
	return 0
}

func resolveProjectPath(projectFlag, cwd string) string {
	if projectFlag != "" {
		if _, err := os.Stat(projectFlag); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "项目路径不存在: %s\n", projectFlag)
			return ""
		}
		return projectFlag
	}

	projects := pmdb.LoadCleanProjects()

	for _, p := range projects {
		if p.Path == cwd {
			return cwd
		}
	}

	if len(projects) == 0 {
		fmt.Printf("→ 注册当前目录: %s\n", cwd)
		return cwd
	}

	fmt.Printf("\n当前目录未注册。已注册 %d 个项目:\n\n", len(projects))
	for i, p := range projects {
		rel := formatTimeAgo(p.LastOpenedAt)
		fmt.Printf("  [%d]  %-10s  %s\n", i+1, rel, p.Path)
	}
	fmt.Printf("\n输入序号 [1-%d]，或 Enter 注册当前目录: ", len(projects))

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)

	if line == "" {
		fmt.Printf("→ 注册当前目录: %s\n", cwd)
		return cwd
	}
	if line == "q" || line == "Q" {
		return ""
	}

	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(projects) {
		fmt.Fprintf(os.Stderr, "无效输入\n")
		return ""
	}
	return projects[idx-1].Path
}

func isPortInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func checkExistingInstance(port int) (project string, ok bool) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}
	var body struct {
		Project string `json:"project"`
		Status  string `json:"status"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) == nil {
		return body.Project, body.Project != ""
	}
	return "", false
}

func portOwnerProcess(port int) string {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
		if err != nil {
			return ""
		}
		lines := strings.Split(string(out), "\n")
		portStr := fmt.Sprintf(":%d", port)
		for _, line := range lines {
			if strings.Contains(line, portStr) && strings.Contains(line, "LISTENING") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					return fmt.Sprintf("PID %s", fields[len(fields)-1])
				}
			}
		}
	default:
		out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port)).Output()
		if err == nil && len(out) > 0 {
			return fmt.Sprintf("PID %s", strings.TrimSpace(string(out)))
		}
	}
	return ""
}

func formatTimeAgo(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		return fmt.Sprintf("%d分钟前", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(diff.Hours()))
	default:
		return fmt.Sprintf("%d天前", int(diff.Hours()/24))
	}
}

func newAPIHandler(projectPath string) http.Handler {
	return apipkg.New(apipkg.Deps{App: application, ProjectPath: projectPath}).Handler()
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
	cfg := pmdb.LoadConfig()
	port := gcfg.ProxyPort
	proxyURL := fmt.Sprintf("http://localhost:%d", port)

	rt, err := pmdb.ResolveAgentConfig(name, gcfg, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent config: %v\n", err)
		os.Exit(1)
	}

	var cmd *exec.Cmd
	switch strings.ToLower(name) {
	case "claude", "claude-code", "cc":
		cmd = exec.Command("claude")
		env := append(os.Environ(),
			"ANTHROPIC_BASE_URL="+proxyURL,
			"ANTHROPIC_AUTH_TOKEN=local",
		)
		if rt.Model != "" {
			env = append(env, "ANTHROPIC_MODEL="+pmdb.LoadModelRegistry().ResolveModelForProtocol(rt.Model, "anthropic"))
		}
		if rt.SubAgentModel != "" {
			env = append(env, "CLAUDE_CODE_SUBAGENT_MODEL="+rt.SubAgentModel)
		}
		if rt.OpusModel != "" {
			env = append(env, "ANTHROPIC_DEFAULT_OPUS_MODEL="+rt.OpusModel)
		}
		if rt.SonnetModel != "" {
			env = append(env, "ANTHROPIC_DEFAULT_SONNET_MODEL="+rt.SonnetModel)
		}
		if rt.HaikuModel != "" {
			env = append(env, "ANTHROPIC_DEFAULT_HAIKU_MODEL="+rt.HaikuModel)
		}
		if rt.SmallFastModel != "" {
			env = append(env, "ANTHROPIC_SMALL_FAST_MODEL="+rt.SmallFastModel)
		}
		if rt.EffortLevel != "" {
			env = append(env, "CLAUDE_CODE_EFFORT_LEVEL="+rt.EffortLevel)
		}
		cmd.Env = env
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

func loadCredentialsOnStartup(profile string) {
	if !pmdb.CredentialsExist() {
		return
	}
	// Check env var first — set by web UI when spawning proxy subprocess
	if pass := os.Getenv("AIPMC_MASTER_PASSWORD"); pass != "" {
		store, err := pmdb.LoadCredentialsProfile([]byte(pass), profile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "credentials: %v\n", err)
			os.Exit(1)
		}
		if store != nil {
			pmdb.SetCredentialStore(store)
			fmt.Println("✓ credentials unlocked")
		}
		return
	}
	fmt.Print("Master password for credentials: ")
	pw, err := pmdb.PromptPassword()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read password: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	store, err := pmdb.LoadCredentialsProfile(pw, profile)
	for i := range pw {
		pw[i] = 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "credentials: %v\n", err)
		os.Exit(1)
	}
	if store != nil {
		pmdb.SetCredentialStore(store)
		fmt.Println("✓ credentials unlocked")
	}
}

func getPassword(prompt string) []byte {
	fmt.Print(prompt)
	pw, err := pmdb.PromptPassword()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read password: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	return pw
}

func dispatchKey(argv []string) {
	if len(argv) < 3 {
		fmt.Println("Usage: aipmc key <init|set|rm|list|show|passwd|status> [--profile <name>]")
		os.Exit(0)
	}

	subcmd := argv[2]
	profile := "default"
	args := argv[3:]

	// Parse --profile / -p flag
	for i := 0; i < len(args); i++ {
		if (args[i] == "--profile" || args[i] == "-p") && i+1 < len(args) {
			profile = args[i+1]
			args = append(args[:i], args[i+2:]...)
			break
		}
	}

	switch subcmd {
	case "init":
		if pmdb.CredentialsExistForProfile(profile) {
			fmt.Fprintf(os.Stderr, "credentials for profile %q already exist. Use aipmc key passwd --profile %s to change password.\n", profile, profile)
			os.Exit(1)
		}
		fmt.Print("Set master password: ")
		p1, err := pmdb.PromptPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "read password: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		fmt.Print("Confirm master password: ")
		p2, err := pmdb.PromptPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "read password: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		if string(p1) != string(p2) {
			fmt.Fprintln(os.Stderr, "passwords do not match")
			os.Exit(1)
		}
		if err := pmdb.CreateProfile(profile, string(p1)); err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("? credentials initialized for profile %q\n", profile)

	case "set":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: aipmc key set <name> <value> [--profile <name>]")
			os.Exit(1)
		}
		name, value := args[0], args[1]
		pass := getPassword("Master password: ")
		store, err := pmdb.LoadCredentialsProfile(pass, profile)
		if err != nil || store == nil {
			fmt.Fprintln(os.Stderr, "wrong password or no credentials file")
			os.Exit(1)
		}
		store.Set(name, value)
		if err := pmdb.SaveCredentialsToProfile(store, pass, profile); err != nil {
			fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("? key for %q saved (profile %s)\n", name, profile)

	case "rm":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: aipmc key rm <name> [--profile <name>]")
			os.Exit(1)
		}
		name := args[0]
		pass := getPassword("Master password: ")
		store, err := pmdb.LoadCredentialsProfile(pass, profile)
		if err != nil || store == nil {
			fmt.Fprintln(os.Stderr, "wrong password or no credentials file")
			os.Exit(1)
		}
		if !store.Remove(name) {
			fmt.Fprintf(os.Stderr, "key %q not found\n", name)
			os.Exit(1)
		}
		if err := pmdb.SaveCredentialsToProfile(store, pass, profile); err != nil {
			fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("? key %q removed (profile %s)\n", name, profile)

	case "list":
		pass := getPassword("Master password: ")
		store, err := pmdb.LoadCredentialsProfile(pass, profile)
		if err != nil || store == nil {
			fmt.Fprintln(os.Stderr, "wrong password or no credentials file")
			os.Exit(1)
		}
		fmt.Printf("Profile: %s\n", profile)
		for _, n := range store.List() {
			k := store.Get(n)
			m := k
			if len(k) > 10 {
				m = k[:6] + "..." + k[len(k)-4:]
			}
			fmt.Printf("  %-15s %s\n", n, m)
		}

	case "show":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: aipmc key show <name> [--profile <name>]")
			os.Exit(1)
		}
		name := args[0]
		pass := getPassword("Master password: ")
		store, err := pmdb.LoadCredentialsProfile(pass, profile)
		if err != nil || store == nil {
			fmt.Fprintln(os.Stderr, "wrong password or no credentials file")
			os.Exit(1)
		}
		if k := store.Get(name); k != "" {
			fmt.Println(k)
		} else {
			fmt.Fprintln(os.Stderr, "not found")
			os.Exit(1)
		}

	case "passwd":
		fmt.Print("Old password: ")
		oldP, err := pmdb.PromptPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "read password: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		fmt.Print("New password: ")
		newP, err := pmdb.PromptPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "read password: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		fmt.Print("Confirm new password: ")
		confirmP, err := pmdb.PromptPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "read password: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		if string(newP) != string(confirmP) {
			fmt.Fprintln(os.Stderr, "passwords do not match")
			os.Exit(1)
		}
		store, err := pmdb.LoadCredentialsProfile(oldP, profile)
		if err != nil || store == nil {
			fmt.Fprintln(os.Stderr, "wrong password or no credentials file")
			os.Exit(1)
		}
		if err := pmdb.SaveCredentialsToProfile(store, newP, profile); err != nil {
			fmt.Fprintf(os.Stderr, "change failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("? password changed (profile %s)\n", profile)

	case "status":
		if pmdb.CredentialsExistForProfile(profile) {
			if s := pmdb.GetCredentialStore(); s != nil && s.Profile == profile {
				fmt.Printf("? profile %s unlocked ? %d provider(s)\n", profile, len(s.Keys))
				for _, n := range s.List() {
					fmt.Printf("  %s\n", n)
				}
			} else {
				fmt.Printf("profile %s exists (locked ? run aipmc serve or aipmc proxy --profile %s)\n", profile, profile)
			}
		} else {
			allProfiles := pmdb.ListProfiles()
			if len(allProfiles) == 0 {
				fmt.Println("no credentials files ? run aipmc key init [--profile name]")
			} else {
				fmt.Printf("no credentials file for profile %q\n", profile)
				fmt.Println("Known profiles:")
				for _, p := range allProfiles {
					fmt.Printf("  %s\n", p)
				}
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown key subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}
