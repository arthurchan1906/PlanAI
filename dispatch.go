package main

import (
	"fmt"
	"os"
	"strings"

	"aipmc/ai"
	"aipmc/cli"
	"aipmc/mcp"
	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/vision"
	"aipmc/analyze"
	"aipmc/session"
	"aipmc/u"
)

func dispatchTask(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		tasks, err := store.ListTasks(args.Str("status", ""), "")
		cli.PrintJSONOrFail(map[string]any{"tasks": tasks}, err)
	case "show":
		t, err := store.GetTask(args.Get("id"))
		cli.PrintJSONOrFail(t, err)
	case "add":
		t, err := store.CreateTask("", args.Get("title"), args.Str("priority", "P1"), args.Str("status", "todo"), args.Str("phase", "general"), args.Get("plan_id"), nil)
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"task": t})
	case "update":
		t, err := store.UpdateTask("", args.Get("id"), args.Str("status", ""), args.Str("note", ""), args.Bool("allow_without_commit"), args.Bool("append_note"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"task": t})
	case "note":
		r, err := store.AppendTaskNote("", args.Get("id"), args.Get("content"))
		cli.PrintJSONOrFail(r, err)
	case "notes":
		n, err := store.ListTaskNotes(args.Get("id"), args.Int("limit", 20))
		cli.PrintJSONOrFail(map[string]any{"notes": n}, err)
	case "plan":
		t, err := store.PlanTask(args.Get("id"), strings.Split(args.Get("steps"), " "))
		cli.PrintJSONOrFail(map[string]any{"task": t}, err)
	case "checkpoint":
		t, err := store.UpdateTaskCheckpoint(args.Get("id"), args.Int("index", 0), !args.Bool("not_done"))
		cli.PrintJSONOrFail(map[string]any{"task": t}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}
func dispatchCommit(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		commits, err := store.ListCommits(args.Str("status", ""), args.Str("task_id", ""), args.Str("decision_id", ""), args.Str("since", ""), args.Int("limit", 0))
			if err != nil {
				cli.Fail(err)
			}
		if args.Bool("compact") {
			type compact struct {
				ID, Title, Status, TaskID string
				FileCount                 int
			}
			var cc []compact
			for _, c := range commits {
				fc := 0
				if files, ok := c["files"].([]any); ok {
					fc = len(files)
				}
				cc = append(cc, compact{ID: u.Str(c["id"]), Title: u.Str(c["title"]), Status: u.Str(c["status"]), TaskID: u.Str(c["task_id"]), FileCount: fc})
			}
			cli.PrintJSON(map[string]any{"commits": cc, "count": len(cc)})
			return
			}
		cli.PrintJSON(map[string]any{"commits": commits, "count": len(commits)})
	case "show":
		c, err := store.GetCommit(args.Get("id"))
		cli.PrintJSONOrFail(c, err)
	case "add":
		taskIDs := []string{}
		if ids := args.Str("task_ids", ""); ids != "" {
			for _, tid := range strings.Split(ids, ",") {
				if tid = strings.TrimSpace(tid); tid != "" {
					taskIDs = append(taskIDs, tid)
				}
			}
			}
		if len(taskIDs) == 0 {
			if tid := args.Str("task_id", ""); tid != "" {
				taskIDs = append(taskIDs, tid)
			}
			}
		if len(taskIDs) == 0 {
			cli.Fail(fmt.Errorf("commit requires --task-id (or --task-ids for multi-task). Find a task: aipmc task list --status in_progress"))
			}
		var commits []map[string]any
		for _, tid := range taskIDs {
			c, err := store.CreateCommit("", args.Get("title"), args.Str("summary", ""), args.Str("evidence_summary", ""), args.Str("review_notes", ""), args.Str("branch", ""), args.Str("commit_hash", ""), tid, args.Str("decision_id", ""), args.Str("status", "draft"), args.Str("test_status", "not_run"), args.Str("review_status", "pending"), nil)
			if err != nil {
				cli.Fail(err)
			}
			commits = append(commits, c)
			}
		if len(commits) == 1 {
			cli.PrintJSON(map[string]any{"commit": commits[0]})
			} else {
			cli.PrintJSON(map[string]any{"commits": commits})
			}
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "summary", "evidence_summary", "review_notes", "branch", "commit_hash", "decision_id", "status", "test_status", "review_status"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		if args.Bool("clear_decision_id") {
			payload["decision_id"] = nil
			}
		c, err := store.UpdateCommit(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"commit": c}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown commit subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchPlan(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		p, err := store.ListPlans(args.Str("roadmap_id", ""), args.Str("status", ""))
			if err != nil {
				cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"plans": p})
	case "show":
		p, err := store.GetPlan(args.Get("id"))
		cli.PrintJSONOrFail(p, err)
	case "add":
		p, err := store.CreatePlan(args.Get("title"), args.Str("goal", ""), args.Get("roadmap_id"), args.Str("vision_id", ""), args.Str("priority", "P1"), args.Str("status", "draft"), nil, nil, nil, nil)
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"plan": p})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "goal", "status", "priority", "roadmap_id", "task_ids"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		p, err := store.UpdatePlan(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"plan": p}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown plan subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchBug(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		b, err := store.ListBugs(args.Str("status", ""), args.Str("severity", ""), args.Str("commit_id", ""), args.Int("limit", 0), 0)
		cli.PrintJSONOrFail(map[string]any{"bugs": b}, err)
	case "show":
		b, err := store.GetBug(args.Get("id"))
		cli.PrintJSONOrFail(b, err)
	case "add":
		b, err := store.CreateBug("", args.Get("title"), args.Str("description", ""), args.Str("severity", "minor"), args.Str("status", "open"), args.Str("commit_id", ""), args.Str("task_id", ""), args.Str("error", ""), args.Str("files", ""), args.Str("root_cause", ""), args.Str("fix", ""), args.Str("tags", ""))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"bug": b})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "description", "severity", "status", "commit_id", "task_id", "error", "files", "root_cause", "fix", "tags"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		if args.Bool("clear_commit_id") {
			payload["clear_commit_id"] = true
			}
		b, err := store.UpdateBug(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"bug": b}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown bug subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchDecision(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		d, err := store.ListDecisions()
		cli.PrintJSONOrFail(map[string]any{"decisions": d}, err)
	case "show":
		d, err := store.GetDecision(args.Get("id"))
		cli.PrintJSONOrFail(d, err)
	case "add":
		d, err := store.CreateDecision("", args.Get("title"), args.Get("background"), args.Get("decision"), args.Str("status", "proposed"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"decision": d})
	case "review":
		d, err := store.UpdateDecisionStatus(args.Get("id"), args.Get("status"))
		cli.PrintJSONOrFail(map[string]any{"decision": d}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown decision subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchIdea(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		i, err := store.ListIdeas(args.Str("status", "")); if err != nil { cli.Fail(err) }
		cli.PrintJSON(map[string]any{"ideas": i})
	case "show":
		i, err := store.GetIdea(args.Get("id"))
		cli.PrintJSONOrFail(i, err)
	case "capture":
		i, err := store.CreateIdea(args.Get("title"), args.Get("summary"), args.Str("impact", ""), args.Str("source", "manual"), args.Bool("canon_conflict"), args.Str("current_summary", ""), args.Str("main_question", ""), args.Str("recommended_next_action", "continue_discussion"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"idea": i})
	case "review":
		i, err := store.ReviewIdea(args.Get("id"), args.Get("status"), args.Str("note", ""))
		cli.PrintJSONOrFail(map[string]any{"idea": i}, err)
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "summary", "impact", "source", "status", "current_summary", "main_question", "recommended_next_action"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		i, err := store.UpdateIdea(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"idea": i}, err)
	case "comment":
		c, err := store.CreateIdeaComment(args.Get("id"), args.Get("content"), args.Str("kind", "comment"), args.Str("author_type", "ai"), args.Str("author_name", "aipmc"))
		cli.PrintJSONOrFail(c, err)
	case "convert":
		if args.Get("to") == "task" {
			r, err := store.ConvertIdeaToTask(args.Get("id"), args.Str("plan_id", ""))
			if err != nil {
				cli.Fail(err)
			}
			cli.PrintJSON(r)
			} else {
			r, err := store.ConvertIdeaToDecision(args.Get("id"))
			cli.PrintJSONOrFail(r, err)
			}
	default:
		fmt.Fprintf(os.Stderr, "unknown idea subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchRoadmap(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		r, err := store.ListRoadmaps(args.Str("vision_id", ""))
		cli.PrintJSONOrFail(map[string]any{"roadmaps": r}, err)
	case "show":
		r, err := store.GetRoadmap(args.Get("id"))
		cli.PrintJSONOrFail(r, err)
	case "add":
		r, err := store.CreateRoadmap(args.Get("title"), args.Str("target_date", ""), args.Str("vision_id", ""), args.Str("status", "planned"), args.Str("priority", "P1"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"roadmap": r})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "target_date", "status", "priority", "vision_id"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		r, err := store.UpdateRoadmap(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"roadmap": r}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown roadmap subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchPrinciple(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		p, err := store.ListPrinciples(args.Str("status", ""), args.Str("kind", ""))
		cli.PrintJSONOrFail(map[string]any{"principles": p}, err)
	case "show":
		p, err := store.GetPrinciple(args.Get("id"))
		cli.PrintJSONOrFail(p, err)
	case "add":
		p, err := store.CreatePrinciple(args.Get("title"), args.Str("summary", ""), args.Str("kind", "governance"), args.Str("status", "active"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"principle": p})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "summary", "kind", "status"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		p, err := store.UpdatePrinciple(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"principle": p}, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown principle subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchLink(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		l, err := store.ListLinks(args.Str("source_id", ""), args.Str("target_id", ""), args.Str("relation", ""))
		cli.PrintJSONOrFail(map[string]any{"links": l}, err)
	case "add":
		l, err := store.CreateLink("", args.Get("source_type"), args.Get("source_id"), args.Get("relation"), args.Get("target_type"), args.Get("target_id"), args.Str("note", ""))
		cli.PrintJSONOrFail(l, err)
	case "delete":
		store.DeleteLink(args.Get("id"))
		cli.PrintJSON(map[string]any{"ok": true})
	default:
		fmt.Fprintf(os.Stderr, "unknown link subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchVision(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		v, err := store.ListVisions()
		cli.PrintJSONOrFail(map[string]any{"visions": v}, err)
	case "show":
		v, err := store.GetVision(args.Get("id"))
		cli.PrintJSONOrFail(v, err)
	case "add":
		v, err := store.CreateVision(args.Get("title"), args.Str("summary", ""), args.Str("status", "active"), args.Str("horizon", "long_term"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"vision": v})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "summary", "status", "horizon"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		v, err := store.UpdateVision(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"vision": v}, err)
		case "":
		runVisionCLI(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown vision subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchDaily(subcmd string, args *cli.Args) {
	switch subcmd {
	case "show":
		d, err := store.GetDailyNote(args.Str("date", ""))
		cli.PrintJSONOrFail(d, err)
	case "close":
		d, err := store.AppendDailyNote(args.Str("date", ""), map[string][]string{})
		cli.PrintJSONOrFail(d, err)
	case "replace":
		d, err := store.ReplaceDailyNote(args.Str("date", ""), map[string][]string{})
		cli.PrintJSONOrFail(d, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown daily subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchReview(subcmd string, args *cli.Args) {
	switch subcmd {
	case "sessions", "":
		since := session.ParseSince(args.Str("since", "24h"))
		limit := args.Int("limit", 50)
		sample := args.Str("sample", "")
		if sample == "" {
			sample = ".pmai/cache/sessions_sample.json"
		}
		var summarizer ai.Summarizer
		if application.AI() != nil && application.AI().Enabled() {
			summarizer = application.AI()
		}
		result, err := session.Run(session.RunOpts{Since: since, Limit: limit, SamplePath: sample, ProjectPath: "", Summarizer: summarizer})
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(result)
	default:
		fmt.Fprintf(os.Stderr, "unknown review subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}


func dispatchReconcile(subcmd string, args *cli.Args) {
	since := session.ParseSince(args.Str("since", "6h"))
	if args.Bool("full") {
		since = ""
	}
	result, err := session.Reconcile(since, "")
	if err != nil {
		cli.Fail(err)
	}
	cli.PrintJSON(result)
}

func dispatchSession(subcmd string, args *cli.Args) {
	switch subcmd {
	case "close":
		cli.PrintJSON(map[string]any{"ok": true, "message": "Session closed."})
	default:
		fmt.Fprintf(os.Stderr, "unknown session subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchDocs(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		d, err := store.ListDocRecords(args.Str("status", ""), args.Str("layer", ""))
		cli.PrintJSONOrFail(map[string]any{"docs": d}, err)
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"type", "status", "layer"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		if args.Bool("source_of_truth") {
			payload["source_of_truth"] = true
			}
		if args.Bool("clear_source_of_truth") {
			payload["source_of_truth"] = false
			}
		doc, err := store.UpdateDocRecord(args.Get("path"), payload)
		cli.PrintJSONOrFail(doc, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown docs subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchCanon(subcmd string, args *cli.Args) {
	switch subcmd {
	case "show":
		c, err := store.GetCanon()
		cli.PrintJSONOrFail(c, err)
	case "update":
		c, err := store.UpdateCanon(args.Get("decision_id"), args.Str("product_goal", ""), args.Str("engineering_focus", ""), args.Str("architecture", ""), u.SplitAndTrim(args.Str("add_scope", ""), "|"), u.SplitAndTrim(args.Str("add_avoid", ""), "|"))
		cli.PrintJSONOrFail(c, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown canon subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchCode(subcmd string, args *cli.Args) {
	cli.PrintJSON(map[string]any{"message": "use git directly for code operations"})
}
func dispatchBrief(subcmd string, args *cli.Args) {
	cli.PrintJSON(map[string]any{"message": fmt.Sprintf("brief %s: run aipmc search <topic>", subcmd)})
}

func dispatchEvent(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		events, err := store.ListEvents(args.Str("filter", ""))
		cli.PrintJSONOrFail(map[string]any{"events": events}, err)
	case "create":
		evt, err := store.CreateEvent(args.Get("type"), args.Get("entity_type"), args.Get("entity_id"), args.Get("summary"))
		cli.PrintJSONOrFail(evt, err)
	default:
		fmt.Fprintf(os.Stderr, "unknown event subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchThread(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		threads, err := store.ListThreads(args.Str("status", ""))
		cli.PrintJSONOrFail(map[string]any{"threads": threads}, err)
	case "show":
		t, err := store.GetThread(args.Get("id"))
		cli.PrintJSONOrFail(t, err)
	case "add":
		t, err := store.CreateThread(args.Get("title"), args.Str("summary", ""), args.Str("source", "manual"))
		if err != nil {
			cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"thread": t})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "summary", "status"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
			}
		t, err := store.UpdateThread(args.Get("id"), payload)
		cli.PrintJSONOrFail(map[string]any{"thread": t}, err)
	case "item":
		// os.Args[3] is the item subcommand (add/remove), after "thread item"
		itemSub := ""
		if len(os.Args) > 3 {
			itemSub = os.Args[3]
			}
		switch itemSub {
		case "add":
			t, err := store.AddToThread(args.Get("thread_id"), args.Get("entity_type"), args.Get("entity_id"), args.Str("note", ""))
			cli.PrintJSONOrFail(map[string]any{"thread": t}, err)
		case "remove":
			store.RemoveFromThread(args.Get("thread_id"), args.Get("entity_type"), args.Get("entity_id"))
			cli.PrintJSON(map[string]any{"ok": true})
		default:
			fmt.Fprintf(os.Stderr, "unknown thread item subcommand: %s\n", itemSub)
			os.Exit(1)
			}
	case "delete":
		store.DeleteThread(args.Get("id"))
		cli.PrintJSON(map[string]any{"ok": true})
	case "suggest":
		suggestions := analyze.AnalyzeThreadSuggestions()
		cli.PrintJSON(map[string]any{"suggestions": suggestions})
	default:
		fmt.Fprintf(os.Stderr, "unknown thread subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchFeedback(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		fbs, _ := mcp.ListFeedbacks(args.Str("label", ""))
		cli.PrintJSON(fbs)
	case "add":
		fb, err := mcp.AddFeedback(args.Str("label", "suggestion"), args.Get("content"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "feedback server unreachable, saved locally: %v\n", err)
			// Fallback: store as idea in local DB
			idea, err := store.CreateIdea("[Feedback] "+args.Get("content")[:min(80, len(args.Get("content")))], args.Get("content"), "", "feedback", false, "", "", "continue_discussion")
			cli.PrintJSONOrFail(map[string]any{"status": "stored_locally", "idea": idea}, err)
			return
			}
		cli.PrintJSON(fb)
	default:
		fmt.Fprintf(os.Stderr, "unknown feedback subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchModels(subcmd string, args *cli.Args) {
	reg := pmdb.LoadModelRegistry()

	switch subcmd {
	case "", "list":
		if !reg.IsActive() {
			fmt.Println("No virtual models configured. Create ~/.aipmc/models.json or use 'aipmc models provider add'.")
			return
		}
		fmt.Println("Providers:")
		for _, p := range reg.Providers {
			anthro := ""
			if p.AnthropicURL != "" {
				anthro = fmt.Sprintf(" anthropic=%s", p.AnthropicURL)
			}
			resp := ""
			if p.ResponsesURL != "" {
				resp = fmt.Sprintf(" responses=%s", p.ResponsesURL)
			}
			fmt.Printf("  %-15s openai=%s%s%s\n", p.Name, p.OpenAIURL, anthro, resp)
		}
		fmt.Println()
		fmt.Println("Virtual Models:")
		for _, m := range reg.Models {
			providers := reg.ListModelProviders(m.ID)
			routeDetails := ""
			for _, rt := range m.Routes {
				proto := ""
				if rt.ModelAnthropic != "" {
					proto += fmt.Sprintf(" anthropic=%s", rt.ModelAnthropic)
				}
				if rt.ModelResponses != "" {
					proto += fmt.Sprintf(" responses=%s", rt.ModelResponses)
				}
				if rt.ModelOpenAI != "" {
					proto += fmt.Sprintf(" openai=%s", rt.ModelOpenAI)
				}
				if proto != "" {
					routeDetails += fmt.Sprintf(" [%s%s]", rt.Provider, proto)
				} else {
					routeDetails += fmt.Sprintf(" [%s]", rt.Provider)
				}
			}
			if routeDetails == "" {
				routeDetails = " (passthrough)"
			}
			fmt.Printf("  %-25s providers=%-12s%s", m.ID, strings.Join(providers, ", "), routeDetails)
			if len(m.Tags) > 0 {
				fmt.Printf(" [%s]", strings.Join(m.Tags, ", "))
			}
			fmt.Println()
		}

	case "current":
		all := pmdb.LoadAllCurrentModels()
		if len(all) == 0 {
			fmt.Println("All agents: Auto (using agent defaults)")
		} else {
			reg2 := pmdb.LoadModelRegistry()
			for _, a := range []string{"claude", "codex", "opencode", "gemini", "cursor"} {
				if cm, ok := all[a]; ok {
					providers := reg2.ListModelProviders(cm)
					if len(providers) > 0 {
						fmt.Printf("  %-10s %s (%s)\n", a+":", cm, strings.Join(providers, ", "))
					} else {
						fmt.Printf("  %-10s %s\n", a+":", cm)
					}
				} else {
					fmt.Printf("  %-10s Auto\n", a+":")
				}
			}
		}

	case "switch":
		modelID := ""
		agent := ""
		if len(os.Args) > 3 {
			modelID = os.Args[3]
		}
		if len(os.Args) > 4 {
			agent = os.Args[4]
		}
		if modelID == "" {
			fmt.Fprintln(os.Stderr, "Usage: aipmc models switch <model-id|--auto> --agent <agent>")
			os.Exit(1)
		}
		if agent == "--agent" {
			if len(os.Args) > 5 {
				agent = os.Args[5]
			}
		}
		if agent == "" || agent == "--agent" {
			fmt.Fprintln(os.Stderr, "Usage: aipmc models switch <model-id|--auto> --agent <agent>")
			fmt.Fprintln(os.Stderr, "  agent: claude, codex, opencode, gemini, cursor")
			os.Exit(1)
		}
		if modelID == "--auto" {
			if err := pmdb.SaveCurrentModel(agent, ""); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Switched %s to Auto\n", agent)
			return
		}
		if err := pmdb.SaveCurrentModel(agent, modelID); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Switched %s to: %s\n", agent, modelID)

	case "provider":
		provSub := ""
		if len(os.Args) > 3 {
			provSub = os.Args[3]
		}
		switch provSub {
		case "add":
			name := args.Get("name")
			openaiURL := args.Get("openai_url")
			if name == "" || openaiURL == "" {
				fmt.Fprintln(os.Stderr, "Usage: aipmc models provider add --name <name> --openai_url <url> [--anthropic_url <url>] [--responses_url <url>]")
				os.Exit(1)
			}
			reg.AddProvider(pmdb.Provider{
				Name:         name,
				OpenAIURL:    openaiURL,
				AnthropicURL: args.Str("anthropic_url", ""),
				ResponsesURL: args.Str("responses_url", ""),
			})
			if err := pmdb.SaveModelRegistry(reg); err != nil {
				fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Provider %q added.\n", name)
		case "rm", "remove":
			name := args.Get("name")
			if name == "" {
				fmt.Fprintln(os.Stderr, "Usage: aipmc models provider rm --name <name>")
				os.Exit(1)
			}
			if !reg.RemoveProvider(name) {
				fmt.Fprintf(os.Stderr, "provider %q not found\n", name)
				os.Exit(1)
			}
			// Also remove models that only have routes to this provider
			var keep []pmdb.VirtualModel
			for _, m := range reg.Models {
				keepModel := false
				for _, rt := range m.Routes {
					if rt.Provider != name {
						keepModel = true
						break
					}
				}
				if keepModel {
					keep = append(keep, m)
				} else if len(m.Routes) == 0 && m.Provider != "" && m.Provider != name {
					// Legacy model with no routes: keep if its Provider is not the deleted one.
					keep = append(keep, m)
				}
			}
			reg.Models = keep
			if err := pmdb.SaveModelRegistry(reg); err != nil {
				fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Provider %q removed.\n", name)
		default:
			fmt.Fprintf(os.Stderr, "Usage: aipmc models provider <add|rm>\n")
			os.Exit(1)
		}

	case "add":
		id := args.Get("id")
		provider := args.Get("provider")
		if id == "" || provider == "" {
			fmt.Fprintln(os.Stderr, "Usage: aipmc models add --id <name> --provider <name> [--anthropic <model>] [--openai <model>] [--responses <model>] [--tags t1,t2] [--priority N]")
			os.Exit(1)
		}
		if reg.FindProvider(provider) == nil {
			fmt.Fprintf(os.Stderr, "provider %q not found \u2014 add it first: aipmc models provider add --name %s --openai_url <url>\n", provider, provider)
			os.Exit(1)
		}
		tags := strings.Split(args.Str("tags", ""), ",")
		if len(tags) == 1 && tags[0] == "" {
			tags = nil
		}
		reg.AddModel(pmdb.VirtualModel{
			ID:          id,
			DisplayName: args.Str("display_name", ""),
			Routes: []pmdb.ModelRoute{{
				Provider:       provider,
				ModelAnthropic: args.Str("anthropic", ""),
				ModelResponses: args.Str("responses", ""),
				ModelOpenAI:    args.Str("openai", ""),
			}},
			Tags:     tags,
			Priority: args.Int("priority", 0),
		})
		if err := pmdb.SaveModelRegistry(reg); err != nil {
			fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Model %q added.\n", id)

	case "rm", "remove":
		id := args.Get("id")
		if id == "" {
			fmt.Fprintln(os.Stderr, "Usage: aipmc models rm --id <name>")
			os.Exit(1)
		}
		if !reg.RemoveModel(id) {
			fmt.Fprintf(os.Stderr, "model %q not found\n", id)
			os.Exit(1)
		}
		if err := pmdb.SaveModelRegistry(reg); err != nil {
			fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Model %q removed.\n", id)

	default:
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  aipmc models current\n")
		fmt.Fprintf(os.Stderr, "  aipmc models switch <model-id|--auto> --agent <agent>\n")
		fmt.Fprintf(os.Stderr, "  aipmc models provider add --name <name> --openai_url <url> [--anthropic_url <url>] [--responses_url <url>]\n")
		fmt.Fprintf(os.Stderr, "  aipmc models provider rm --name <name>\n")
		fmt.Fprintf(os.Stderr, "  aipmc models add --id <name> --provider <name> [--anthropic <model>] [--openai <model>] [--responses <model>] [--tags t1,t2] [--priority N]\n")
		fmt.Fprintf(os.Stderr, "  aipmc models rm --id <name>\n")
		os.Exit(1)
	}
}

// runVisionCLI handles the "aipmc vision --image <PATH> --prompt <TEXT>" command.
// It sends a screenshot to a vision-capable model for analysis.
func runVisionCLI(args *cli.Args) {
	imagePath := args.Get("image")
	prompt := args.Get("prompt")
	modelID := args.Str("model", "")
	iteration := args.Int("iteration", 1)

	if imagePath == "" || prompt == "" {
		fmt.Fprintln(os.Stderr, "Usage: aipmc vision --image <PATH> --prompt <TEXT> [--iteration N] [--model MODEL_ID]")
		os.Exit(1)
	}

	result := vision.RunVision(imagePath, prompt, iteration, modelID)
	cli.PrintJSON(result)

	if !result.OK {
		os.Exit(1)
	}
}
