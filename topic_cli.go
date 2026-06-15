package main

import (
	"fmt"
	"os"

	"aipmc/cli"
	"aipmc/collab"
	"aipmc/store"
	"aipmc/u"
)

func dispatchTopic(subcmd string, args *cli.Args) {
	// Keep agentNotice in sync with the collaboration rules in skill.go.
	collab.SetAgentNotice("讨论模式：勿改代码。互读用 aipm_read_discussions(full=true)。禁止 sqlite3。")

	switch subcmd {
	case "create":
		title := args.Get("title")
		if title == "" {
			cli.Fail(fmt.Errorf("topic create requires --title"))
		}
		t, err := store.CreateCollaborationTopic(title, args.Str("plan_id", ""), "pm")
		if err != nil {
			cli.Fail(err)
		}
		if args.Bool("discussion-mode") {
			if err := collab.SetDiscussionMode(u.Str(t["id"]), true); err != nil {
				cli.Fail(err)
			}
			fmt.Println("  🔒 讨论模式已开启 — 非白名单路径 Write 将告警")
		}
		cli.PrintJSON(map[string]any{"topic": t})
	case "catchup":
		id := args.Get("topic")
		if id == "" {
			cli.Fail(fmt.Errorf("topic catchup requires --topic"))
		}
		if err := collab.Catchup(id); err != nil {
			cli.Fail(err)
		}
	case "prompt":
		id := args.Get("topic")
		to := args.Get("to")
		say := args.Get("say")
		if id == "" || to == "" || say == "" {
			cli.Fail(fmt.Errorf("topic prompt requires --topic, --to, and --say"))
		}
		out, err := collab.Prompt(id, to, args.Str("refs", ""), say)
		if err != nil {
			cli.Fail(err)
		}
		fmt.Print(out)
	case "close":
		id := args.Get("topic")
		if id == "" {
			cli.Fail(fmt.Errorf("topic close requires --topic"))
		}
		t, err := collab.CloseTopic(id, args.Bool("force"))
		if err != nil {
			cli.Fail(err)
		}
		collab.SetDiscussionMode(id, false)
		cli.PrintJSON(map[string]any{"topic": t})
	case "list":
		rooms, err := store.ListMeetingRooms(args.Str("status", "active"))
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"topics": rooms})
	case "show":
		id := args.Get("topic")
		if id == "" {
			cli.Fail(fmt.Errorf("topic show requires --topic"))
		}
		t, err := store.GetCollaborationTopic(id)
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"topic": t})
	case "routes":
		id := args.Get("topic")
		if id == "" {
			cli.Fail(fmt.Errorf("topic routes requires --topic"))
		}
		routes, err := store.ListRoutes(id, args.Int("limit", 20))
		if err != nil {
			cli.Fail(err)
		}
		cli.PrintJSON(map[string]any{"routes": routes})
	default:
		fmt.Fprintf(os.Stderr, "unknown topic subcommand: %s\n", subcmd)
		fmt.Fprintf(os.Stderr, "usage: aipmc topic {create|catchup|prompt|close|list|show|routes}\n")
		os.Exit(1)
	}
}
