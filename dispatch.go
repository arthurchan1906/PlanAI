package main

import (
	"fmt"
	"os"
	"strings"

	"aipmc/cli"
)

func dispatchTask(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		tasks, _ := listTasks(args.Str("status", ""), "")
		cli.PrintJSON(map[string]any{"tasks": tasks})
	case "show":
		t, _ := getTask(args.Get("id"))
		cli.PrintJSON(t)
	case "add":
		t, err := createTask(args.Get("title"), args.Str("priority", "P1"), args.Str("status", "todo"), args.Str("phase", "general"), args.Get("plan_id"), nil)
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"task": t})
	case "update":
		t, err := updateTask(args.Get("id"), args.Str("status", ""), args.Str("note", ""), args.Bool("allow_without_commit"), args.Bool("append_note"))
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"task": t})
	case "note":
		r, _ := appendTaskNote(args.Get("id"), args.Get("content"))
		cli.PrintJSON(r)
	case "notes":
		n, _ := listTaskNotes(args.Get("id"), args.Int("limit", 20))
		cli.PrintJSON(map[string]any{"notes": n})
	case "plan":
		t, _ := planTask(args.Get("id"), strings.Split(args.Get("steps"), " "))
		cli.PrintJSON(map[string]any{"task": t})
	case "checkpoint":
		t, _ := updateTaskCheckpoint(args.Get("id"), args.Int("index", 0), !args.Bool("not_done"))
		cli.PrintJSON(map[string]any{"task": t})
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}
func dispatchCommit(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		commits, err := listCommits(args.Str("status", ""), args.Str("task_id", ""), args.Str("decision_id", ""), args.Str("since", ""), args.Int("limit", 0))
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
				cc = append(cc, compact{ID: str(c["id"]), Title: str(c["title"]), Status: str(c["status"]), TaskID: str(c["task_id"]), FileCount: fc})
			}
			cli.PrintJSON(map[string]any{"commits": cc, "count": len(cc)})
			return
		}
		cli.PrintJSON(map[string]any{"commits": commits, "count": len(commits)})
	case "show":
		c, _ := getCommit(args.Get("id"))
		cli.PrintJSON(c)
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
			c, err := createCommit(args.Get("title"), args.Str("summary", ""), args.Str("evidence_summary", ""), args.Str("review_notes", ""), args.Str("branch", ""), args.Str("commit_hash", ""), tid, args.Str("decision_id", ""), args.Str("status", "draft"), args.Str("test_status", "not_run"), args.Str("review_status", "pending"), nil)
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
		c, _ := updateCommit(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"commit": c})
	default:
		fmt.Fprintf(os.Stderr, "unknown commit subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchPlan(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		p, err := listPlans(args.Str("roadmap_id", ""), args.Str("status", ""))
			if err != nil {
				cli.Fail(err)
			}
		cli.PrintJSON(map[string]any{"plans": p})
	case "show":
		p, _ := getPlan(args.Get("id"))
		cli.PrintJSON(p)
	case "add":
		p, err := createPlan(args.Get("title"), args.Str("goal", ""), args.Get("roadmap_id"), args.Str("vision_id", ""), args.Str("priority", "P1"), args.Str("status", "draft"), nil, nil, nil, nil)
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
		p, _ := updatePlan(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"plan": p})
	default:
		fmt.Fprintf(os.Stderr, "unknown plan subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchBug(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		b, _ := listBugs(args.Str("status", ""), args.Str("severity", ""), args.Str("commit_id", ""), args.Int("limit", 0))
		cli.PrintJSON(map[string]any{"bugs": b})
	case "show":
		b, _ := getBug(args.Get("id"))
		cli.PrintJSON(b)
	case "add":
		b, err := createBug(args.Get("title"), args.Str("description", ""), args.Str("severity", "minor"), args.Str("status", "open"), args.Str("commit_id", ""), args.Str("error", ""), args.Str("files", ""), args.Str("root_cause", ""), args.Str("fix", ""), args.Str("tags", ""))
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"bug": b})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "description", "severity", "status", "commit_id", "error", "files", "root_cause", "fix", "tags"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
		}
		if args.Bool("clear_commit_id") {
			payload["clear_commit_id"] = true
		}
		b, _ := updateBug(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"bug": b})
	default:
		fmt.Fprintf(os.Stderr, "unknown bug subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchDecision(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		d, _ := listDecisions()
		cli.PrintJSON(map[string]any{"decisions": d})
	case "show":
		d, _ := getDecision(args.Get("id"))
		cli.PrintJSON(d)
	case "add":
		d, err := createDecision(args.Get("title"), args.Get("background"), args.Get("decision"), args.Str("status", "proposed"))
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"decision": d})
	case "review":
		d, _ := updateDecisionStatus(args.Get("id"), args.Get("status"))
		cli.PrintJSON(map[string]any{"decision": d})
	default:
		fmt.Fprintf(os.Stderr, "unknown decision subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchIdea(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		i, err := listIdeas(args.Str("status", "")); if err != nil { cli.Fail(err) }
		cli.PrintJSON(map[string]any{"ideas": i})
	case "show":
		i, _ := getIdea(args.Get("id"))
		cli.PrintJSON(i)
	case "capture":
		i, err := createIdea(args.Get("title"), args.Get("summary"), args.Str("impact", ""), args.Str("source", "manual"), args.Bool("canon_conflict"), args.Str("current_summary", ""), args.Str("main_question", ""), args.Str("recommended_next_action", "continue_discussion"))
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"idea": i})
	case "review":
		i, _ := reviewIdea(args.Get("id"), args.Get("status"), args.Str("note", ""))
		cli.PrintJSON(map[string]any{"idea": i})
	case "update":
		payload := map[string]any{}
		for _, k := range []string{"title", "summary", "impact", "source", "status", "current_summary", "main_question", "recommended_next_action"} {
			if v := args.Str(k, ""); v != "" {
				payload[k] = v
			}
		}
		i, _ := updateIdea(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"idea": i})
	case "comment":
		c, _ := createIdeaComment(args.Get("id"), args.Get("content"), args.Str("kind", "comment"), args.Str("author_type", "ai"), args.Str("author_name", "aipmc"))
		cli.PrintJSON(c)
	case "convert":
		if args.Get("to") == "task" {
			r, err := convertIdeaToTask(args.Get("id"), args.Str("plan_id", ""))
			if err != nil {
				cli.Fail(err)
			}
			cli.PrintJSON(r)
		} else {
			r, _ := convertIdeaToDecision(args.Get("id"))
			cli.PrintJSON(r)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown idea subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchRoadmap(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		r, _ := listRoadmaps(args.Str("vision_id", ""))
		cli.PrintJSON(map[string]any{"roadmaps": r})
	case "show":
		r, _ := getRoadmap(args.Get("id"))
		cli.PrintJSON(r)
	case "add":
		r, err := createRoadmap(args.Get("title"), args.Str("target_date", ""), args.Str("vision_id", ""), args.Str("status", "planned"), args.Str("priority", "P1"))
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
		r, _ := updateRoadmap(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"roadmap": r})
	default:
		fmt.Fprintf(os.Stderr, "unknown roadmap subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchPrinciple(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		p, _ := listPrinciples(args.Str("status", ""), args.Str("kind", ""))
		cli.PrintJSON(map[string]any{"principles": p})
	case "show":
		p, _ := getPrinciple(args.Get("id"))
		cli.PrintJSON(p)
	case "add":
		p, err := createPrinciple(args.Get("title"), args.Str("summary", ""), args.Str("kind", "governance"), args.Str("status", "active"))
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
		p, _ := updatePrinciple(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"principle": p})
	default:
		fmt.Fprintf(os.Stderr, "unknown principle subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchLink(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		l, _ := listLinks(args.Str("source_id", ""), args.Str("target_id", ""), args.Str("relation", ""))
		cli.PrintJSON(map[string]any{"links": l})
	case "add":
		l, _ := createLink(args.Get("source_type"), args.Get("source_id"), args.Get("relation"), args.Get("target_type"), args.Get("target_id"), args.Str("note", ""))
		cli.PrintJSON(l)
	case "delete":
		deleteLink(args.Get("id"))
		cli.PrintJSON(map[string]any{"ok": true})
	default:
		fmt.Fprintf(os.Stderr, "unknown link subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchVision(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		v, _ := listVisions()
		cli.PrintJSON(map[string]any{"visions": v})
	case "show":
		v, _ := getVision(args.Get("id"))
		cli.PrintJSON(v)
	case "add":
		v, err := createVision(args.Get("title"), args.Str("summary", ""), args.Str("status", "active"), args.Str("horizon", "long_term"))
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
		v, _ := updateVision(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"vision": v})
	default:
		fmt.Fprintf(os.Stderr, "unknown vision subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchDaily(subcmd string, args *cli.Args) {
	switch subcmd {
	case "show":
		d, _ := getDailyNote(args.Str("date", ""))
		cli.PrintJSON(d)
	case "close":
		d, _ := appendDailyNote(args.Str("date", ""), map[string][]string{})
		cli.PrintJSON(d)
	case "replace":
		d, _ := replaceDailyNote(args.Str("date", ""), map[string][]string{})
		cli.PrintJSON(d)
	default:
		fmt.Fprintf(os.Stderr, "unknown daily subcommand: %s\n", subcmd)
		os.Exit(1)
	}
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
		d, _ := listDocRecords(args.Str("status", ""), args.Str("layer", ""))
		cli.PrintJSON(map[string]any{"docs": d})
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
		doc, _ := updateDocRecord(args.Get("path"), payload)
		cli.PrintJSON(doc)
	default:
		fmt.Fprintf(os.Stderr, "unknown docs subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchCanon(subcmd string, args *cli.Args) {
	switch subcmd {
	case "show":
		c, _ := getCanon()
		cli.PrintJSON(c)
	case "update":
		c, _ := updateCanon(args.Get("decision_id"), args.Str("product_goal", ""), args.Str("engineering_focus", ""), args.Str("architecture", ""), nil, nil)
		cli.PrintJSON(c)
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
		events, _ := listEvents(args.Str("filter", ""))
		cli.PrintJSON(map[string]any{"events": events})
	case "create":
		evt, _ := createEvent(args.Get("type"), args.Get("entity_type"), args.Get("entity_id"), args.Get("summary"))
		cli.PrintJSON(evt)
	default:
		fmt.Fprintf(os.Stderr, "unknown event subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchThread(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		threads, _ := listThreads(args.Str("status", ""))
		cli.PrintJSON(map[string]any{"threads": threads})
	case "show":
		t, _ := getThread(args.Get("id"))
		cli.PrintJSON(t)
	case "add":
		t, err := createThread(args.Get("title"), args.Str("summary", ""), args.Str("source", "manual"))
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
		t, _ := updateThread(args.Get("id"), payload)
		cli.PrintJSON(map[string]any{"thread": t})
	case "item":
		// os.Args[3] is the item subcommand (add/remove), after "thread item"
		itemSub := ""
		if len(os.Args) > 3 {
			itemSub = os.Args[3]
		}
		switch itemSub {
		case "add":
			t, _ := addToThread(args.Get("thread_id"), args.Get("entity_type"), args.Get("entity_id"), args.Str("note", ""))
			cli.PrintJSON(map[string]any{"thread": t})
		case "remove":
			removeFromThread(args.Get("thread_id"), args.Get("entity_type"), args.Get("entity_id"))
			cli.PrintJSON(map[string]any{"ok": true})
		default:
			fmt.Fprintf(os.Stderr, "unknown thread item subcommand: %s\n", itemSub)
			os.Exit(1)
		}
	case "delete":
		deleteThread(args.Get("id"))
		cli.PrintJSON(map[string]any{"ok": true})
	case "suggest":
		suggestions := analyzeThreadSuggestions()
		cli.PrintJSON(map[string]any{"suggestions": suggestions})
	default:
		fmt.Fprintf(os.Stderr, "unknown thread subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

func dispatchFeedback(subcmd string, args *cli.Args) {
	switch subcmd {
	case "list":
		fbs, _ := listFeedbacks(args.Str("label", ""))
		cli.PrintJSON(fbs)
	case "add":
		fb, err := addFeedback(args.Str("label", "suggestion"), args.Get("content"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "feedback server unreachable, saved locally: %v\n", err)
			// Fallback: store as idea in local DB
			idea, _ := createIdea("[Feedback] "+args.Get("content")[:min(80, len(args.Get("content")))], args.Get("content"), "", "feedback", false, "", "", "continue_discussion")
			cli.PrintJSON(map[string]any{"status": "stored_locally", "idea": idea})
			return
		}
		cli.PrintJSON(fb)
	default:
		fmt.Fprintf(os.Stderr, "unknown feedback subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}
