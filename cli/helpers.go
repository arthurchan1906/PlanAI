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

PRIMARY (infrastructure):
  aipmc init                     Initialize project + auto-configure MCP
  aipmc setup <platform>          Configure MCP for a specific platform (run without args to see options)
  aipmc web                      Start web UI server (PM dashboard)
  aipmc mcp                      Start MCP server (Agent interface)

MCP TOOLS (Agent primary — always visible in tool list):
  aipm_get_briefing              Project briefing + PM alerts + analysis
  aipm_search_context            Search all entities with related context
  aipm_record_commit             Record commit + scope drift detection
  aipm_create_task               Create task + duplicate detection
  aipm_analyze                   Full project health analysis
  aipm_mark_consumed             Confirm Agent has read PM events

CLI (legacy / debug):
  aipmc search|start|next|status|context|inbox|analyze|briefing
  aipmc task|commit|plan|bug|decision|idea|roadmap|principle [CRUD]
  aipmc topic {create,catchup,prompt,close,list,show,routes}  Multi-agent collaboration
  aipmc doctor|info|event|link|daily|docs|canon

HIERARCHY: commit → task → plan → roadmap (no orphans, no back-fill)`)
}
