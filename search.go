package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"aipmc/ai"
	"aipmc/analyze"
	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/u"
)

// searchFTS5WithDB queries FTS5 using the provided DB connection (for cross-project search).
func searchFTS5WithDB(db *sql.DB, query string, limit int) []searchHit {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return []searchHit{}
	}
	ftsQuery := strings.Join(terms, " ") + "*"
	rows, err := db.Query(`
		SELECT entity_type, entity_id, title, rank
		FROM fts5_index
		WHERE fts5_index MATCH ?
		ORDER BY rank
		LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var results []searchHit
	for rows.Next() {
		var entityType, entityID, title string
		var rank float64
		rows.Scan(&entityType, &entityID, &title, &rank)
		results = append(results, searchHit{
			Type: entityType, ID: entityID, Title: title,
			Score: int(rank * 100),
			Command: fmt.Sprintf("aipmc %s show --id %s", entityType, entityID),
		})
	}
	if results == nil { results = []searchHit{} }
	return results
}

// searchFTS5 queries the FTS5 index with BM25 ranking.
// Returns nil if the index is unavailable, so callers can fall back.
func searchFTS5(query string, limit int) []searchHit {
	db, err := pmdb.Open()
	if err != nil {
		return nil
	}
	defer db.Close()

	// Build a MATCH query: prefix-wildcard on the last term for partial matching.
	terms := searchTerms(query)
	if len(terms) == 0 {
		return []searchHit{}
	}
	ftsQuery := strings.Join(terms, " ") + "*"

	rows, err := db.Query(`
		SELECT entity_type, entity_id, title, rank
		FROM fts5_index
		WHERE fts5_index MATCH ?
		ORDER BY rank
		LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil // fall back to linear search
	}
	defer rows.Close()

	var results []searchHit
	for rows.Next() {
		var entityType, entityID, title string
		var rank float64
		rows.Scan(&entityType, &entityID, &title, &rank)
		results = append(results, searchHit{
			Type:    entityType,
			ID:      entityID,
			Title:   title,
			Score:   int(rank * 100),
			Command: fmt.Sprintf("aipmc %s show --id %s", entityType, entityID),
		})
	}
	if results == nil {
		results = []searchHit{}
	}
	return results
}

// searchProjectContext searches across all entity types.
func searchProjectContext(query string, limit int) map[string]any {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return map[string]any{"query": query, "count": 0, "results": []any{}}
	}

	// Try FTS5 first — BM25 ranking with proper full-text search.
	results := searchFTS5(query, limit*3)
	if results == nil {
		// FTS5 unavailable: fall back to linear scan.
		results = searchLinear(query)
		// Sort and truncate linear results
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score != results[j].Score {
				return results[i].Score > results[j].Score
			}
			if results[i].Type != results[j].Type {
				return results[i].Type < results[j].Type
			}
			return results[i].Title < results[j].Title
		})
	}

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

// searchLinear performs the original linear scan across all entity tables.
// Used as a fallback when FTS5 is unavailable.
func searchLinear(query string) []searchHit {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return []searchHit{}
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
	if plans, err := store.ListPlans("", ""); err == nil {
		for _, p := range plans {
			haystack := strings.ToLower(u.Str(p["title"]) + " " + u.Str(p["goal"]) + " " + u.Str(p["status"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "plan", ID: u.Str(p["id"]), Title: u.Str(p["title"]), Status: u.Str(p["status"]), Score: score,
					Command: fmt.Sprintf("aipmc plan show --id %s", u.Str(p["id"]))})
			}
		}
	}

	// Search commits
	if commits, err := store.ListCommits("", "", "", "", 0); err == nil {
		for _, c := range commits {
			haystack := strings.ToLower(u.Str(c["title"]) + " " + u.Str(c["summary"]) + " " + u.Str(c["commit_hash"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "commit", ID: u.Str(c["id"]), Title: u.Str(c["title"]), Status: u.Str(c["status"]), Score: score,
					Command: fmt.Sprintf("aipmc commit show --id %s", u.Str(c["id"]))})
			}
		}
	}

	// Search bugs
	if bugs, err := store.ListBugs("", "", "", 0); err == nil {
		for _, b := range bugs {
			haystack := strings.ToLower(u.Str(b["title"]) + " " + u.Str(b["description"]) + " " + u.Str(b["error"]) + " " + u.Str(b["root_cause"]) + " " + u.Str(b["fix"]) + " " + u.Str(b["tags"]) + " " + u.Str(b["files"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "bug", ID: u.Str(b["id"]), Title: u.Str(b["title"]), Status: u.Str(b["status"]), Score: score,
					Command: fmt.Sprintf("aipmc bug show --id %s", u.Str(b["id"]))})
			}
		}
	}

	// Search decisions
	if decs, err := store.ListDecisions(); err == nil {
		for _, d := range decs {
			haystack := strings.ToLower(u.Str(d["title"]) + " " + u.Str(d["status"]) + " " + u.Str(d["background"]) + " " + u.Str(d["decision"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "decision", ID: u.Str(d["id"]), Title: u.Str(d["title"]), Status: u.Str(d["status"]), Score: score,
					Command: fmt.Sprintf("aipmc decision show --id %s", u.Str(d["id"]))})
			}
		}
	}

	// Search ideas
	if ideas, err := store.ListIdeas(""); err == nil {
		for _, i := range ideas {
			haystack := strings.ToLower(u.Str(i["title"]) + " " + u.Str(i["summary"]) + " " + u.Str(i["current_summary"]) + " " + u.Str(i["status"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "idea", ID: u.Str(i["id"]), Title: u.Str(i["title"]), Status: u.Str(i["status"]), Score: score,
					Command: fmt.Sprintf("aipmc idea show --id %s", u.Str(i["id"]))})
			}
		}
	}

	// Search roadmaps
	if rds, err := store.ListRoadmaps(""); err == nil {
		for _, r := range rds {
			haystack := strings.ToLower(u.Str(r["title"]) + " " + u.Str(r["status"]) + " " + u.Str(r["priority"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "roadmap", ID: u.Str(r["id"]), Title: u.Str(r["title"]), Status: u.Str(r["status"]), Score: score,
					Command: fmt.Sprintf("aipmc roadmap show --id %s", u.Str(r["id"]))})
			}
		}
	}

	// Search threads
	if threads, err := store.ListThreads(""); err == nil {
		for _, t := range threads {
			haystack := strings.ToLower(u.Str(t["title"]) + " " + u.Str(t["summary"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "thread", ID: u.Str(t["id"]), Title: u.Str(t["title"]), Status: u.Str(t["status"]), Score: score,
					Command: fmt.Sprintf("aipmc thread show --id %s", u.Str(t["id"]))})
			}
		}
	}

	// Search principles
	if prs, err := store.ListPrinciples("", ""); err == nil {
		for _, p := range prs {
			haystack := strings.ToLower(u.Str(p["title"]) + " " + u.Str(p["summary"]))
			score := matchScore(haystack, terms)
			if score > 0 {
				results = append(results, searchHit{Type: "principle", ID: u.Str(p["id"]), Title: u.Str(p["title"]), Status: u.Str(p["status"]), Score: score})
			}
		}
	}

	return results
}

type searchHit struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Score   int    `json:"score"`
	Command string `json:"command,omitempty"`
}

// SearchText returns the text used for semantic embedding comparison.
func (h searchHit) SearchText() string { return h.Title }

// aiSearchRerank wraps ai.HybridSearch for the searchHit type.
// Returns nil if AI is unavailable or fails.
func aiSearchRerank(query string, limit int, candidates []searchHit) []searchHit {
	// Convert to interface slice
	providers := make([]ai.SearchResultProvider, len(candidates))
	for i := range candidates {
		providers[i] = candidates[i]
	}
	reranked := ai.HybridSearch(query, limit, aiClient, func(q string, l int) []ai.SearchResultProvider {
		hits := searchFTS5(q, l)
		out := make([]ai.SearchResultProvider, len(hits))
		for i := range hits {
			out[i] = hits[i]
		}
		return out
	})
	// Convert back
	result := make([]searchHit, len(reranked))
	for i, p := range reranked {
		if h, ok := p.(searchHit); ok {
			result[i] = h
		}
	}
	return result
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

func mustListTasks() []store.Task {
	tasks, _ := store.ListTasks("", "")
	return tasks
}


// ---- Agent Runtime ----

func buildAgentStartPacket() map[string]any {
	tasks, _ := store.ListTasks("in_progress", "")
	plans, _ := store.ListPlans("", "active")
	threads, _ := store.ListThreads("active")

	// Fetch unconsumed PM events to inject into Agent's context
	events, _ := store.GetUnconsumedEvents()
	alerts := []map[string]any{}
	for _, e := range events {
		alerts = append(alerts, map[string]any{
			"type":    e["type"],
			"summary": e["summary"],
			"entity":  e["entity_type"],
		})
	}

	// Thread suggestions
	threadSugs := analyze.AnalyzeThreadSuggestions()

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
			"active_threads":    threads[:min(3, len(threads))],
		},
		"thread_suggestions": threadSugs,
		"briefing": analyze.BuildBriefing(aiClient), // Markdown briefing for Agent consumption
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

func buildContextPack() map[string]any {
	tasks, _ := store.ListTasks("in_progress", "")
	plans, _ := store.ListPlans("", "active")
	docs, _ := store.ListDocRecords("", "")

	sotDocs := []any{}
	for _, d := range docs {
		if sot, _ := d["source_of_truth"].(bool); sot {
			sotDocs = append(sotDocs, d)
		}
	}

	// Include analysis results for Agent awareness
	report := analyze.RunFullAnalysis()
	events, _ := store.GetUnconsumedEvents()
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
	tasks, _ := store.ListTasks("", "")
	bugs, _ := store.ListBugs("open", "", "", 0)
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
	ideas, _ := store.ListIdeas("new")
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
