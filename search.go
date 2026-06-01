package main

import (
	"fmt"
	"sort"
	"strings"
)

// searchProjectContext searches across all entity types.
func searchProjectContext(query string, limit int) map[string]any {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return map[string]any{"query": query, "count": 0, "results": []any{}}
	}

	var results []searchHit

	// Search tasks
	for _, t := range mustListTasks() {
		haystack := strings.ToLower(t.Title + " " + t.Status + " " + t.Phase + " " + t.LastNote)
		score := matchScore(haystack, terms)
		if score > 0 {
			results = append(results, searchHit{Type: "task", ID: t.ID, Title: t.Title, Status: t.Status, Score: score,
				Command: fmt.Sprintf("aipmc task show --id %s", t.ID)})
		}
	}

	// Search plans
	if plans, err := listPlans("", ""); err == nil {
		for _, p := range plans {
			haystack := strings.ToLower(str(p["title"]) + " " + str(p["goal"]) + " " + str(p["status"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "plan", ID: str(p["id"]), Title: str(p["title"]), Status: str(p["status"]), Score: score,
					Command: fmt.Sprintf("aipmc plan show --id %s", str(p["id"]))})
			}
		}
	}

	// Search commits
	if commits, err := listCommits("", "", "", "", 0); err == nil {
		for _, c := range commits {
			haystack := strings.ToLower(str(c["title"]) + " " + str(c["summary"]) + " " + str(c["commit_hash"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "commit", ID: str(c["id"]), Title: str(c["title"]), Status: str(c["status"]), Score: score,
					Command: fmt.Sprintf("aipmc commit show --id %s", str(c["id"]))})
			}
		}
	}

	// Search bugs
	if bugs, err := listBugs("", "", "", 0); err == nil {
		for _, b := range bugs {
			haystack := strings.ToLower(str(b["title"]) + " " + str(b["description"]) + " " + str(b["error"]) + " " + str(b["root_cause"]) + " " + str(b["fix"]) + " " + str(b["tags"]) + " " + str(b["files"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "bug", ID: str(b["id"]), Title: str(b["title"]), Status: str(b["status"]), Score: score,
					Command: fmt.Sprintf("aipmc bug show --id %s", str(b["id"]))})
			}
		}
	}

	// Search decisions
	if decs, err := listDecisions(); err == nil {
		for _, d := range decs {
			haystack := strings.ToLower(str(d["title"]) + " " + str(d["status"]) + " " + str(d["background"]) + " " + str(d["decision"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "decision", ID: str(d["id"]), Title: str(d["title"]), Status: str(d["status"]), Score: score,
					Command: fmt.Sprintf("aipmc decision show --id %s", str(d["id"]))})
			}
		}
	}

	// Search ideas
	if ideas, err := listIdeas(""); err == nil {
		for _, i := range ideas {
			haystack := strings.ToLower(str(i["title"]) + " " + str(i["summary"]) + " " + str(i["current_summary"]) + " " + str(i["status"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "idea", ID: str(i["id"]), Title: str(i["title"]), Status: str(i["status"]), Score: score,
					Command: fmt.Sprintf("aipmc idea show --id %s", str(i["id"]))})
			}
		}
	}

	// Search roadmaps
	if rds, err := listRoadmaps(""); err == nil {
		for _, r := range rds {
			haystack := strings.ToLower(str(r["title"]) + " " + str(r["status"]) + " " + str(r["priority"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "roadmap", ID: str(r["id"]), Title: str(r["title"]), Status: str(r["status"]), Score: score,
					Command: fmt.Sprintf("aipmc roadmap show --id %s", str(r["id"]))})
			}
		}
	}

	// Search principles
	if prs, err := listPrinciples("", ""); err == nil {
		for _, p := range prs {
			haystack := strings.ToLower(str(p["title"]) + " " + str(p["summary"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "principle", ID: str(p["id"]), Title: str(p["title"]), Status: str(p["status"]), Score: score})
			}
		}
	}

	// Sort by score desc, then type, then title
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Type != results[j].Type {
			return results[i].Type < results[j].Type
		}
		return results[i].Title < results[j].Title
	})

	if limit <= 0 {
		limit = 8
	}
	if len(results) > limit {
		results = results[:limit]
	}

	return map[string]any{
		"query":   query,
		"count":   len(results),
		"results": results,
	}
}

type searchHit struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Score   int    `json:"score"`
	Command string `json:"command,omitempty"`
}

func searchTerms(query string) []string {
	var terms []string
	for _, t := range strings.Fields(strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(query, "_", " "), "-", " "))) {
		t = strings.TrimSpace(t)
		if t != "" {
			terms = append(terms, t)
		}
	}
	return terms
}

func matchScore(haystack string, terms []string) int {
	score := 0
	for _, t := range terms {
		if strings.Contains(haystack, t) {
			score++
		}
	}
	return score
}

func mustListTasks() []Task {
	tasks, _ := listTasks("", "")
	return tasks
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ---- Agent Runtime ----

func buildAgentStartPacket() map[string]any {
	tasks, _ := listTasks("in_progress", "")
	plans, _ := listPlans("", "active")

	// Fetch unconsumed PM events to inject into Agent's context
	events, _ := getUnconsumedEvents()
	alerts := []map[string]any{}
	for _, e := range events {
		alerts = append(alerts, map[string]any{
			"type":    e["type"],
			"summary": e["summary"],
			"entity":  e["entity_type"],
		})
	}

	return map[string]any{
		"role":    "ai_start",
		"message": "Use this before coding. Reuse existing PMAI tasks/plans/decisions/docs before creating new ones.",
		"rules": []string{
			"Use the current task/doc context before coding.",
			"Before adding a task, plan, decision, or doc: aipmc search \"<topic>\".",
			"If related work already exists: prefer show, update, or task note instead of creating a new record.",
		},
		"current_focus": map[string]any{
			"in_progress_tasks": tasks[:min(3, len(tasks))],
			"active_plans":      plans[:min(3, len(plans))],
		},
		"briefing": BuildBriefing(), // Markdown briefing for Agent consumption
		"pm_alerts": alerts,          // Unconsumed PM intent changes
		"recommended_flow": []map[string]any{
			{"when": "Before coding or creating anything new", "command": "aipmc start"},
			{"when": "If the current work topic is not obvious", "command": "aipmc search \"<topic>\""},
			{"when": "If matching work already exists", "command": "aipmc next"},
			{"when": "Only after reuse checks fail", "command": "aipmc task add ...  or  aipmc plan add ..."},
		},
	}
}

func buildNextActionPacket() map[string]any {
	tasks, _ := listTasks("in_progress", "")
	if len(tasks) > 0 {
		t := tasks[0]
		return map[string]any{
			"next_action": map[string]any{
				"type":    "task",
				"id":      t.ID,
				"title":   t.Title,
				"status":  t.Status,
				"command": fmt.Sprintf("aipmc task show --id %s", t.ID),
			},
		}
	}
	return map[string]any{
		"next_action": map[string]any{
			"command": "aipmc plan list",
			"reason":  "No in-progress tasks. Check plans for next work.",
		},
	}
}

func buildContextPack() map[string]any {
	tasks, _ := listTasks("in_progress", "")
	plans, _ := listPlans("", "active")
	docs, _ := listDocRecords("", "")

	sotDocs := []any{}
	for _, d := range docs {
		if sot, _ := d["source_of_truth"].(bool); sot {
			sotDocs = append(sotDocs, d)
		}
	}

	// Include analysis results for Agent awareness
	report := runFullAnalysis()
	events, _ := getUnconsumedEvents()
	alerts := []map[string]any{}
	for _, e := range events {
		alerts = append(alerts, map[string]any{
			"type":    e["type"],
			"summary": e["summary"],
			"entity":  e["entity_type"],
		})
	}

	return map[string]any{
		"project": map[string]any{
			"source_of_truth_docs": sotDocs[:min(3, len(sotDocs))],
		},
		"mainline": map[string]any{
			"in_progress_tasks": tasks[:min(3, len(tasks))],
			"active_plans":      plans[:min(3, len(plans))],
		},
		"analysis": map[string]any{
			"summary":    report.Summary,
			"orphans":    report.Orphans,
			"duplicates": report.Duplicates,
			"at_risk":    report.Progress,
		},
		"pm_alerts": alerts,
	}
}

func getStatusSnapshot() map[string]any {
	tasks, _ := listTasks("", "")
	bugs, _ := listBugs("open", "", "", 0)
	inProgress := 0
	for _, t := range tasks {
		if t.Status == "in_progress" {
			inProgress++
		}
	}
	return map[string]any{
		"total_tasks":       len(tasks),
		"in_progress_tasks": inProgress,
		"open_bugs":         len(bugs),
	}
}

func getInboxSummary() map[string]any {
	ideas, _ := listIdeas("new")
	return map[string]any{
		"new_ideas": len(ideas),
		"ideas":     ideas[:min(10, len(ideas))],
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
