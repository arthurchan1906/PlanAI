package webdata

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"aipmc/analyze"
	"aipmc/store"
	"aipmc/u"
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

// ActivityPayload is returned by GET /pmai/web/activity.
// Provides light-narrative session cards, alerts, and graph edges.
func ActivityPayload() map[string]any {
	since := time.Now().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05")
	sessions, _ := store.RecentAgentActivity(since, 30)
	if sessions == nil {
		sessions = []store.AgentSessionSummary{}
	}

	// L2 summaries (may be empty)
	summaries, _ := store.ListSessionSummariesWithSummary("", 30)
	summaryMap := map[string]store.SessionSummary{}
	for _, s := range summaries {
		summaryMap[s.SessionID] = s
	}

	// Build activity cards
	type activityCard struct {
		SessionID    string   `json:"session_id"`
		Source       string   `json:"source"`
		Intent       string   `json:"intent"`
		Goal         string   `json:"goal"`
		FirstPrompt  string   `json:"first_prompt"`
		TouchedFiles []string `json:"touched_files"`
		Entities     []string `json:"entities"`
		QualityScore int      `json:"quality_score"`
		MsgCount     int      `json:"msg_count"`
		Directive    bool     `json:"directive"`
		HasL2        bool     `json:"has_l2"`
		FirstSeen    string   `json:"first_seen"`
	}

	var cards []activityCard
	fileCounts := map[string]int{}
	var graphEdges [][3]string // [source, target, relation]

	for _, s := range sessions {
		card := activityCard{
			SessionID:   s.SessionID,
			Source:      s.Source,
			MsgCount:    s.UserPromptCount + s.ToolCallCount,
			FirstSeen:   s.FirstSeen,
		}

		// B1 data: intent + first user prompt
		if ss, ok := summaryMap[s.SessionID]; ok {
			// Parse B1 review data from review_json
			if ss.ReviewJSON != "" {
				var review struct {
					Intent           string `json:"intent"`
					DirectiveSession bool   `json:"directive_session"`
					QualityScore     int    `json:"quality_score"`
				}
				json.Unmarshal([]byte(ss.ReviewJSON), &review)
				card.Intent = review.Intent
				card.Directive = review.DirectiveSession
				card.QualityScore = review.QualityScore
			}
			card.Entities = parseEntityRefs(ss.EntityRefs)
		}
		if card.Intent == "" {
			card.Intent = "unknown"
		}

		// First user prompt text
		if len(s.UserPrompts) > 0 {
			card.FirstPrompt = firstLine(s.UserPrompts[len(s.UserPrompts)-1])
		}

		// L2 data if available
		if ss, ok := summaryMap[s.SessionID]; ok && ss.Summary != "" {
			var l2 struct {
				Goal   string   `json:"goal"`
				Files  []string `json:"files"`
			}
			if json.Unmarshal([]byte(ss.Summary), &l2) == nil && l2.Goal != "" {
				card.Goal = l2.Goal
				card.TouchedFiles = l2.Files
				card.HasL2 = true
			}
		}

		// File hotspot counting (from L2 files or entity refs)
		for _, f := range card.TouchedFiles {
			fileCounts[f]++
		}

		// Graph edges: session → entity
		for _, eid := range card.Entities {
			parts := strings.SplitN(eid, "-", 2)
			entityType := ""
			if len(parts) > 0 {
				entityType = parts[0]
			}
			graphEdges = append(graphEdges, [3]string{card.SessionID, eid, "refers_to:" + entityType})
		}
		// Graph edges: session → file
		for _, f := range card.TouchedFiles {
			graphEdges = append(graphEdges, [3]string{card.SessionID, "file:" + f, "touched"})
		}

		cards = append(cards, card)
	}

	// Build alerts
	var hotspots []map[string]any
	for f, cnt := range fileCounts {
		if cnt >= 2 {
			hotspots = append(hotspots, map[string]any{"file": f, "count": cnt})
		}
	}
	// Sort by count desc
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i]["count"].(int) > hotspots[j]["count"].(int)
	})

	// Count tentative links from events
	tentativeCount := 0
	if events, err := store.GetUnconsumedEvents(); err == nil {
		for _, e := range events {
			if u.Str(e["type"]) == "tentative_link" {
				tentativeCount++
			}
		}
	}

	if cards == nil {
		cards = []activityCard{}
	}
	if hotspots == nil {
		hotspots = []map[string]any{}
	}
	if graphEdges == nil {
		graphEdges = [][3]string{}
	}

	return map[string]any{
		"sessions":    cards,
		"alerts":      map[string]any{"file_hotspots": hotspots, "tentative_links": tentativeCount},
		"graph_edges": graphEdges,
	}
}

func parseEntityRefs(raw string) []string {
	if raw == "" || raw == "[]" {
		return []string{}
	}
	var refs []string
	json.Unmarshal([]byte(raw), &refs)
	if refs == nil {
		refs = []string{}
	}
	return refs
}

func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx > 0 {
		text = text[:idx]
	}
	runes := []rune(text)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return text
}
