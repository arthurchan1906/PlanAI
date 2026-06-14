package project

import (
	"fmt"

	"aipmc/ai"
	"aipmc/analyze"
	"aipmc/store"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StatusSnapshot returns high-level project counts for dashboard/API.
func StatusSnapshot() map[string]any {
	tasks, _ := store.ListTasks("", "")
	bugs, _ := store.ListBugs("open", "", "", 0)
	inProgress := 0
	for _, t := range tasks {
		if t.Status == "in_progress" {
			inProgress++
		}
	}
	return map[string]any{
		"total_tasks":         len(tasks),
		"in_progress_tasks":   inProgress,
		"open_bugs":           len(bugs),
	}
}

// InboxSummary returns new ideas awaiting review.
func InboxSummary() map[string]any {
	ideas, _ := store.ListIdeas("new")
	return map[string]any{
		"new_ideas": len(ideas),
		"ideas":     ideas[:minInt(10, len(ideas))],
	}
}

// NextActionPacket suggests the next CLI action for an agent.
func NextActionPacket() map[string]any {
	tasks, _ := store.ListTasks("in_progress", "")
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

// ContextPack assembles project context for agents and web UI.
func ContextPack() map[string]any {
	tasks, _ := store.ListTasks("in_progress", "")
	plans, _ := store.ListPlans("", "active")
	docs, _ := store.ListDocRecords("", "")

	sotDocs := []any{}
	for _, d := range docs {
		if sot, _ := d["source_of_truth"].(bool); sot {
			sotDocs = append(sotDocs, d)
		}
	}
	report := analyze.RunFullAnalysis()
	events, _ := store.GetUnconsumedEvents()
	alerts := pmAlerts(events)

	return map[string]any{
		"project": map[string]any{
			"source_of_truth_docs": sotDocs[:minInt(3, len(sotDocs))],
		},
		"mainline": map[string]any{
			"in_progress_tasks": tasks[:minInt(3, len(tasks))],
			"active_plans":      plans[:minInt(3, len(plans))],
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

// AgentStartPacket is the payload for `aipmc start`.
func AgentStartPacket(client *ai.Client) map[string]any {
	tasks, _ := store.ListTasks("in_progress", "")
	plans, _ := store.ListPlans("", "active")
	threads, _ := store.ListThreads("active")
	events, _ := store.GetUnconsumedEvents()

	return map[string]any{
		"role":    "ai_start",
		"message": "Use this before coding. Reuse existing PMAI tasks/plans/decisions/docs before creating new ones.",
		"rules": []string{
			"Use the current task/doc context before coding.",
			"Before adding a task, plan, decision, or doc: aipmc search \"<topic>\".",
			"If related work already exists: prefer show, update, or task note instead of creating a new record.",
		},
		"current_focus": map[string]any{
			"in_progress_tasks": tasks[:minInt(3, len(tasks))],
			"active_plans":      plans[:minInt(3, len(plans))],
			"active_threads":    threads[:minInt(3, len(threads))],
		},
		"thread_suggestions": analyze.AnalyzeThreadSuggestions(),
		"briefing":           analyze.BuildBriefing(client),
		"pm_alerts":          pmAlerts(events),
		"recommended_flow": []map[string]any{
			{"when": "Before coding or creating anything new", "command": "aipmc start"},
			{"when": "If the current work topic is not obvious", "command": "aipmc search \"<topic>\""},
			{"when": "If matching work already exists", "command": "aipmc next"},
			{"when": "Only after reuse checks fail", "command": "aipmc task add ...  or  aipmc plan add ..."},
		},
	}
}

func pmAlerts(events []map[string]any) []map[string]any {
	alerts := []map[string]any{}
	for _, e := range events {
		alerts = append(alerts, map[string]any{
			"type":    e["type"],
			"summary": e["summary"],
			"entity":  e["entity_type"],
		})
	}
	return alerts
}
