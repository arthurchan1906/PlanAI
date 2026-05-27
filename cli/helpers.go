package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

func PrintJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func Fail(err error)  { fmt.Fprintf(os.Stderr, "error: %v\n", err); os.Exit(1) }

func RunDoctor(dbPath string, dbOpen func() (interface{ Close() error }, error)) {
	problems := []string{}
	if dbPath == "" {
		problems = append(problems, "No .pmai directory found. Run aipmc init first.")
	} else {
		db, err := dbOpen()
		if err != nil {
			problems = append(problems, fmt.Sprintf("Cannot open database: %v", err))
		} else {
			db.Close()
		}
	}
	PrintJSON(map[string]any{"ok": len(problems) == 0, "problems": problems, "db_path": dbPath, "binary": os.Args[0]})
}

func RunInfo(dbPath string) {
	PrintJSON(map[string]any{"tool": "aipmc", "db_path": dbPath,
		"commands": []string{"init", "search", "start", "next", "status", "doctor", "info", "web",
			"task {list,show,add,update,note,notes,plan,checkpoint}",
			"commit {list,show,add,update}", "plan {list,show,add,update}",
			"bug {list,show,add,update}", "decision {list,show,add,review}",
			"idea {list,show,capture,review,update,comment,convert}",
			"roadmap {list,show,add,update}", "principle {list,show,add,update}",
			"link {list,add,delete}", "daily {show,close,replace}", "docs {list,update}",
		}})
}

func PrintHelp() {
	fmt.Println(`AIPM CLI — AI Project Manager

COMMANDS:
  aipmc init                     Initialize .pmai in current project
  aipmc search <query>           Search across all types
  aipmc start / next / status    Agent runtime
  aipmc doctor / info            Diagnostics
  aipmc web                      Start web UI server

  aipmc task list|show|add|update|note|notes|plan|checkpoint
  aipmc commit list|show|add|update
  aipmc plan list|show|add|update
  aipmc bug list|show|add|update
  aipmc decision list|show|add|review
  aipmc idea list|show|capture|review|update|comment|convert
  aipmc roadmap list|show|add|update
  aipmc principle list|show|add|update
  aipmc link list|add|delete
  aipmc daily show|close|replace
  aipmc docs list|update

HIERARCHY: commit → task → plan → roadmap (no orphans, no back-fill)`)
}
