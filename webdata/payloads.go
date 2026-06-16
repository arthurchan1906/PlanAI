package webdata

import (
	"aipmc/analyze"
	"aipmc/store"
)

// PlanningPayload is returned by GET /pmai/web/planning.
func PlanningPayload() map[string]any {
	b := NewBundle()
	b.loadTasks()
	b.loadTaskNotes()
	b.loadCommits()
	b.loadRoadmaps()
	b.loadPlans()
	b.loadVisions()
	b.loadDocs()
	b.loadIdeas()
	return map[string]any{
		"roadmaps":   b.enhancedRoadmaps(),
		"plans":      b.enhancedPlans(),
		"visions":    b.visions,
		"tasks":      b.enhancedTasks(),
		"task_notes": b.taskNotes,
		"commits":    b.enhancedCommits(),
		"docs":       b.enhancedDocs(),
		"ideas":      b.ideas,
	}
}

// CommitsPayload is returned by GET /pmai/web/commits.
func CommitsPayload() map[string]any {
	b := NewBundle()
	b.loadTasks()
	b.loadDecisions()
	return map[string]any{
		"commits":   b.enhancedCommits(),
		"tasks":     b.enhancedTasks(),
		"decisions": b.enhancedDecisions(),
	}
}

// BugsPayload is returned by GET /pmai/web/bugs.
func BugsPayload() map[string]any {
	b := NewBundle()
	b.loadCommits()
	return map[string]any{
		"bugs":    b.enhancedBugs(),
		"commits": b.enhancedCommits(),
	}
}

// DecisionsPayload is returned by GET /pmai/web/decisions.
func DecisionsPayload() map[string]any {
	b := NewBundle()
	b.loadPrinciples()
	b.loadCanon()
	b.loadVisions()
	return map[string]any{
		"decisions":  b.enhancedDecisions(),
		"principles": b.principles,
		"canon":      b.canon,
		"visions":    b.visions,
	}
}

// IdeasPayload is returned by GET /pmai/web/ideas.
func IdeasPayload() map[string]any {
	b := NewBundle()
	b.loadIdeas()
	return map[string]any{"ideas": b.ideas}
}

// DocsPayload is returned by GET /pmai/web/docs.
func DocsPayload() map[string]any {
	b := NewBundle()
	return map[string]any{
		"docs":      b.enhancedDocs(),
		"doc_audit": b.docAudit(),
	}
}

// ThreadsPayload is returned by GET /pmai/web/threads.
func ThreadsPayload() map[string]any {
	b := NewBundle()
	b.loadPlans()
	b.loadTasks()
	b.loadCommits()
	b.loadDecisions()
	threads, _ := store.ListThreads("")
	return map[string]any{
		"threads":             threads,
		"thread_suggestions":  analyze.AnalyzeThreadSuggestions(),
		"thread_status":       analyze.AnalyzeThreadStatus(),
		"plans":               b.plans,
		"tasks":               b.enhancedTasks(),
		"commits":             b.enhancedCommits(),
		"decisions":           b.enhancedDecisions(),
	}
}

// AgentsPayload is returned by GET /pmai/web/agents.
func AgentsPayload() map[string]any {
	agents, _ := store.ListAgentProfiles()
	if agents == nil {
		agents = []map[string]any{}
	}
	return map[string]any{"agents": agents}
}

// AuditPayload is returned by GET /pmai/web/audit.
func AuditPayload() map[string]any {
	logs, _ := store.ListAuditLog("", "", 100)
	if logs == nil {
		logs = []map[string]any{}
	}
	return map[string]any{"audit_logs": logs}
}

// DailyPayload is returned by GET /pmai/web/daily.
func DailyPayload() map[string]any {
	b := NewBundle()
	b.loadDaily()
	b.loadTasks()
	b.loadCommits()
	return map[string]any{
		"daily":   b.daily,
		"tasks":   b.enhancedTasks(),
		"commits": b.enhancedCommits(),
	}
}
