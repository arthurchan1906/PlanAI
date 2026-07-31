package webdata

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"aipmc/analyze"
	pmdb "aipmc/db"
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

	// B1 review data (available for all sessions)
	// Use ListSessionSummariesSince to get ALL session_summaries, not just L2 ones
	summaries, _ := store.ListSessionSummariesSince(since, 50)
	if summaries == nil {
		summaries = []store.SessionSummary{}
	}
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

	// ── Edge sources, by priority: graph_edges (pipeline) > MCP (Agent refs) > time-window (fallback) ──
	geEdges := buildGraphEdgesSupplement(since)     // pipeline: file_touch, relates_to, link_entities dual-write
	mcpEdges := buildMCPEdges()                     // Agent MCP tool calls
	commitEdges := buildCommitWindowEdges(sessions) // time-window heuristic (fallback)

	seenEdges := map[string]bool{}
	addEdges := func(edges [][3]string) {
		for _, e := range edges {
			key := e[0] + "|" + e[1]
			if seenEdges[key] {
				continue
			}
			seenEdges[key] = true
			graphEdges = append(graphEdges, e)
		}
	}
	addEdges(geEdges)
	addEdges(mcpEdges)
	addEdges(commitEdges)

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
				if err := json.Unmarshal([]byte(ss.ReviewJSON), &review); err == nil {
					card.Intent = review.Intent
					card.Directive = review.DirectiveSession
					card.QualityScore = review.QualityScore
				}
			}
			card.Entities = parseEntityRefs(ss.EntityRefs)
		}
		if card.Intent == "" {
			card.Intent = "unknown"
		}

		// First user prompt text
		if len(s.UserPrompts) > 0 {
			card.FirstPrompt = firstLine(s.UserPrompts[0])
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

		// Graph edges: session → entity (with type prefix for frontend label resolution)
		for _, eid := range card.Entities {
			parts := strings.SplitN(eid, "-", 2)
			entityType := ""
			if len(parts) > 0 {
				entityType = parts[0]
			}
			entityID := entityType + ":" + eid // "task:task-20260615-xxx" — frontend needs prefix for lookupEntityTitle
			sessionEdges := [][3]string{{card.SessionID, entityID, "refers_to:" + entityType}}
			addEdges(sessionEdges)
		}
		// Graph edges: session → file
		for _, f := range card.TouchedFiles {
			sessionEdges := [][3]string{{card.SessionID, "file:" + f, "touched"}}
			addEdges(sessionEdges)
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

	// Build entity title lookup for graph labels
	entityLabels := map[string]string{}
	for _, edge := range graphEdges {
		for _, id := range []string{edge[0], edge[1]} {
			if strings.Contains(id, ":") && !strings.HasPrefix(id, "file:") {
				entityLabels[id] = lookupEntityTitle(id)
			}
		}
	}

	return map[string]any{
		"sessions":      cards,
		"alerts":        map[string]any{"file_hotspots": hotspots, "tentative_links": tentativeCount},
		"graph_edges":   graphEdges,
		"entity_labels": entityLabels,
	}
}

func lookupEntityTitle(id string) string {
	// id format: "task:task-20260615-xxx" or "bug:bug-20260624-xxx"
	parts := strings.SplitN(id, ":", 2)
	if len(parts) < 2 {
		return id
	}
	prefix := parts[0]  // "task", "bug", "commit", "plan", "decision"
	eid := parts[1]     // "task-20260615-xxx"

	switch prefix {
	case "task":
		if t, err := store.GetTaskSimple(eid); err == nil {
			return u.Str(t["title"])
		}
	case "bug":
		if b, err := store.GetBug(eid); err == nil {
			return u.Str(b["title"])
		}
	case "commit":
		if c, err := store.GetCommit(eid); err == nil {
			return u.Str(c["title"])
		}
	case "plan":
		if p, err := store.GetPlan(eid); err == nil {
			return u.Str(p["title"])
		}
	case "decision":
		if d, err := store.GetDecision(eid); err == nil {
			return u.Str(d["title"])
		}
	}
	return id
}

var mcpEntityRE = regexp.MustCompile(`(?i)(task|plan|decision|commit|bug)-\d{8}-\d{6}-[a-f0-9]{6}`)

// buildMCPEdges scans discussion_log for MCP calls with resolved session_ids.
func buildMCPEdges() [][3]string {
	db, err := pmdb.Open()
	if err != nil {
		return nil
	}
	defer db.Close()

	rows, err := db.Query(`SELECT session_id, content FROM discussion_log
		WHERE (content LIKE '📡 aipm_record_commit%' OR content LIKE '📡 aipm_create_task%' OR content LIKE '📡 aipm_record_bug%')
		AND session_id != '' AND session_id != 'unknown'
		ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var edges [][3]string
	seen := map[string]bool{}
	for rows.Next() {
		var sid, content string
		rows.Scan(&sid, &content)
		for _, eid := range mcpEntityRE.FindAllString(content, -1) {
			key := sid + "|" + eid
			if seen[key] {
				continue
			}
			seen[key] = true
			entityType := "entity"
			if idx := strings.IndexByte(eid, '-'); idx > 0 {
				entityType = eid[:idx]
			}
			edges = append(edges, [3]string{sid, entityType + ":" + eid, "refers_to:" + entityType})
		}
	}
	return edges
}

// buildCommitWindowEdges matches sessions to commits by time window (FirstSeen → LastSeen).
func buildCommitWindowEdges(sessions []store.AgentSessionSummary) [][3]string {
	var edges [][3]string
	seen := map[string]bool{}

	for _, s := range sessions {
		if s.FirstSeen == "" {
			continue
		}
		end := s.LastSeen
		if end == "" {
			end = s.FirstSeen
		}
		// Extend window by 30 min on each side
		commits, err := store.FindCommitsInWindow(s.FirstSeen, end)
		if err != nil || len(commits) == 0 {
			continue
		}
		for _, c := range commits {
			key := s.SessionID + "|" + c.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, [3]string{s.SessionID, "commit:" + c.ID, "refers_to:commit"})
		}
	}
	return edges
}

// buildGraphEdgesSupplement queries graph_edges for pipeline-computed edges.
// Includes: file_touch (weight >= 0.2), relates_to, fixes, implements, blocked_by, depends_on.
// Excludes: same_session, file_read.
// Source-type-normalized: session-sourced edges keep direction; others swap so session is always source.
func buildGraphEdgesSupplement(since string) [][3]string {
	db, err := pmdb.Open()
	if err != nil {
		return nil
	}
	defer db.Close()

	rows, err := db.Query(`SELECT source_type, source_id, edge_type, target_type, target_id, weight
		FROM graph_edges
		WHERE edge_type IN ('file_touch','relates_to','fixes','implements','blocked_by','depends_on')
		AND (edge_type != 'file_touch' OR weight >= 0.2)
		AND created_at >= ?
		ORDER BY weight DESC LIMIT 500`, since)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var edges [][3]string
	seen := map[string]bool{}
	for rows.Next() {
		var st, sid, et, tt, tid string
		var w float64
		rows.Scan(&st, &sid, &et, &tt, &tid, &w)

		var source, target, relation string

		// Normalize direction: session is always source in the activity graph
		if st == "session" {
			source = sid
			target = tt + ":" + tid
			relation = et + ":" + tt
		} else if tt == "session" {
			source = tid
			target = st + ":" + sid
			relation = et + ":" + st
		} else {
			// Entity ↔ entity: prefix both
			source = st + ":" + sid
			target = tt + ":" + tid
			relation = et + ":" + tt
		}

		key := source + "|" + target
		if seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, [3]string{source, target, relation})
	}
	// Resolve FK chains: commit→task→plan→roadmap (not stored in graph_edges)
	// These are critical for showing structured hierarchy in the activity graph.
	edges, seen = resolveFKChains(db, edges, seen)
	return edges
}

func resolveFKChains(db *sql.DB, edges [][3]string, seen map[string]bool) ([][3]string, map[string]bool) {
	// Collect entity IDs from edges
	commitIDs := map[string]bool{}
	taskIDs := map[string]bool{}
	planIDs := map[string]bool{}

	for _, e := range edges {
		src := e[0]
		tgt := e[1]
		for _, id := range []string{src, tgt} {
			parts := strings.SplitN(id, ":", 2)
			if len(parts) < 2 {
				continue
			}
			switch parts[0] {
			case "commit":
				commitIDs[parts[1]] = true
			case "task":
				taskIDs[parts[1]] = true
			case "plan":
				planIDs[parts[1]] = true
			}
		}
	}

	// Inject commit→task edges
	if len(commitIDs) > 0 {
		cids := make([]any, 0, len(commitIDs))
		for id := range commitIDs {
			cids = append(cids, id)
		}
		q := "SELECT id, task_id FROM commits WHERE id IN (?" + strings.Repeat(",?", len(cids)-1) + ")"
		rows, err := db.Query(q, cids...)
		if err == nil {
			for rows.Next() {
				var cid, tid string
				rows.Scan(&cid, &tid)
				if tid == "" {
					continue
				}
				key := "commit:" + cid + "|task:" + tid + "|has_task"
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, [3]string{"commit:" + cid, "task:" + tid, "has_task"})
				taskIDs[tid] = true // expand further
			}
			if rows.Err() != nil {
				// continue with partial results
			}
			rows.Close()
		}
	}

	// Inject task→plan edges
	if len(taskIDs) > 0 {
		tids := make([]any, 0, len(taskIDs))
		for id := range taskIDs {
			tids = append(tids, id)
		}
		q := "SELECT id, plan_id FROM tasks WHERE id IN (?" + strings.Repeat(",?", len(tids)-1) + ")"
		rows, err := db.Query(q, tids...)
		if err == nil {
			for rows.Next() {
				var tid, pid string
				rows.Scan(&tid, &pid)
				if pid == "" {
					continue
				}
				key := "task:" + tid + "|plan:" + pid + "|belongs_to"
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, [3]string{"task:" + tid, "plan:" + pid, "belongs_to"})
				planIDs[pid] = true // expand further
			}
			if rows.Err() != nil {
				// continue with partial results
			}
			rows.Close()
		}
	}

	// Inject plan→roadmap edges
	if len(planIDs) > 0 {
		pids := make([]any, 0, len(planIDs))
		for id := range planIDs {
			pids = append(pids, id)
		}
		q := "SELECT id, roadmap_id FROM plans WHERE id IN (?" + strings.Repeat(",?", len(pids)-1) + ")"
		rows, err := db.Query(q, pids...)
		if err == nil {
			for rows.Next() {
				var pid, rid string
				rows.Scan(&pid, &rid)
				if rid == "" {
					continue
				}
				key := "plan:" + pid + "|roadmap:" + rid + "|belongs_to"
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, [3]string{"plan:" + pid, "roadmap:" + rid, "belongs_to"})
			}
			if rows.Err() != nil {
				// continue with partial results
			}
			rows.Close()
		}
	}

	return edges, seen
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
