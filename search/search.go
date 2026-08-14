package search

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"aipmc/ai"
	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/u"
)

// Hit is a single search result across PMAI entities.
type Hit struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Score   int    `json:"score"`
	Command string `json:"command,omitempty"`
}

// SearchText returns text used for semantic embedding comparison.
func (h Hit) SearchText() string { return h.Title }

// ToMap converts a Hit to the generic map form used by MCP handlers.
func (h Hit) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"type": h.Type, "id": h.ID, "title": h.Title,
		"status": h.Status, "score": h.Score, "command": h.Command,
	}
}

// HitsToMaps converts hits for MCP JSON responses.
func HitsToMaps(hits []Hit) []map[string]interface{} {
	out := make([]map[string]interface{}, len(hits))
	for i, h := range hits {
		out[i] = h.ToMap()
	}
	return out
}

// HitsFromMaps parses MCP-style hit maps back into Hits.
func HitsFromMaps(maps []map[string]interface{}) []Hit {
	out := make([]Hit, len(maps))
	for i, m := range maps {
		out[i] = Hit{
			Type:    strVal(m["type"]),
			ID:      strVal(m["id"]),
			Title:   strVal(m["title"]),
			Status:  strVal(m["status"]),
			Score:   intVal(m["score"]),
			Command: strVal(m["command"]),
		}
	}
	return out
}

func strVal(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intVal(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// FTS5WithDB queries FTS5 using the provided DB connection (cross-project search).
func FTS5WithDB(db *sql.DB, query string, limit int) []Hit {
	terms := terms(query)
	if len(terms) == 0 {
		return []Hit{}
	}

	// Build FTS5 query with OR semantics for multi-term searches.
	// Single term: "term*" (prefix match).
	// Multiple terms: "term1* OR term2* OR ..." so any term can match,
	// avoiding the implicit AND between bare terms that treats the query
	// as an indivisible whole.
	var ftsQuery string
	if len(terms) == 1 {
		ftsQuery = `"` + terms[0] + `"*`
	} else {
		var parts []string
		for _, t := range terms {
			parts = append(parts, `"`+t+`"*`)
		}
		ftsQuery = strings.Join(parts, " OR ")
	}
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
	var results []Hit
	for rows.Next() {
		var entityType, entityID, title string
		var rank float64
		rows.Scan(&entityType, &entityID, &title, &rank)
		results = append(results, Hit{
			Type: entityType, ID: entityID, Title: title,
			Score:   int(rank * 100),
			Command: fmt.Sprintf("aipmc %s show --id %s", entityType, entityID),
		})
	}
	if results == nil {
		results = []Hit{}
	}
	return results
}

// FTS5 queries the FTS5 index with BM25 ranking for the current project DB.
func FTS5(query string, limit int) []Hit {
	db, err := pmdb.Open()
	if err != nil {
		return nil
	}
	defer db.Close()
	return FTS5WithDB(db, query, limit)
}

// ProjectContext searches across all entity types.
func ProjectContext(query string, limit int) map[string]any {
	terms := terms(query)
	if len(terms) == 0 {
		return map[string]any{"query": query, "count": 0, "results": []any{}}
	}
	results := FTS5(query, limit*3)
	if results == nil {
		results = Linear(query)
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

// Linear performs a linear scan across entity tables (FTS5 fallback).
func Linear(query string) []Hit {
	terms := terms(query)
	if len(terms) == 0 {
		return []Hit{}
	}
	var results []Hit
	for _, t := range mustListTasks() {
		haystack := strings.ToLower(t.Title + " " + t.Status + " " + t.Phase + " " + t.LastNote)
		if score := matchScore(haystack, terms); score > 0 {
			results = append(results, Hit{Type: "task", ID: t.ID, Title: t.Title, Status: t.Status, Score: score,
				Command: fmt.Sprintf("aipmc task show --id %s", t.ID)})
		}
	}
	appendEntityHits(&results, terms, "plan", func() ([]map[string]any, error) {
		return store.ListPlans("", "")
	}, func(p map[string]any) (string, string, string) {
		return u.Str(p["id"]), u.Str(p["title"]), u.Str(p["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc plan show --id %s", id) },
		func(p map[string]any) string {
			return u.Str(p["title"]) + " " + u.Str(p["goal"]) + " " + u.Str(p["status"])
		})
	appendEntityHits(&results, terms, "commit", listCommits(), func(c map[string]any) (string, string, string) {
		return u.Str(c["id"]), u.Str(c["title"]), u.Str(c["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc commit show --id %s", id) },
		func(c map[string]any) string {
			return u.Str(c["title"]) + " " + u.Str(c["summary"]) + " " + u.Str(c["commit_hash"])
		})
	appendEntityHits(&results, terms, "bug", listBugs(), func(b map[string]any) (string, string, string) {
		return u.Str(b["id"]), u.Str(b["title"]), u.Str(b["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc bug show --id %s", id) },
		func(b map[string]any) string {
			return u.Str(b["title"]) + " " + u.Str(b["description"]) + " " + u.Str(b["error"])
		})
	appendEntityHits(&results, terms, "decision", listDecisions(), func(d map[string]any) (string, string, string) {
		return u.Str(d["id"]), u.Str(d["title"]), u.Str(d["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc decision show --id %s", id) },
		func(d map[string]any) string {
			return u.Str(d["title"]) + " " + u.Str(d["status"]) + " " + u.Str(d["background"])
		})
	appendEntityHits(&results, terms, "idea", listIdeas(), func(i map[string]any) (string, string, string) {
		return u.Str(i["id"]), u.Str(i["title"]), u.Str(i["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc idea show --id %s", id) },
		func(i map[string]any) string {
			return u.Str(i["title"]) + " " + u.Str(i["summary"]) + " " + u.Str(i["current_summary"])
		})
	appendEntityHits(&results, terms, "roadmap", listRoadmaps(), func(r map[string]any) (string, string, string) {
		return u.Str(r["id"]), u.Str(r["title"]), u.Str(r["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc roadmap show --id %s", id) },
		func(r map[string]any) string {
			return u.Str(r["title"]) + " " + u.Str(r["status"]) + " " + u.Str(r["priority"])
		})
	appendEntityHits(&results, terms, "thread", listThreads(), func(t map[string]any) (string, string, string) {
		return u.Str(t["id"]), u.Str(t["title"]), u.Str(t["status"])
	}, func(id string) string { return fmt.Sprintf("aipmc thread show --id %s", id) },
		func(t map[string]any) string {
			return u.Str(t["title"]) + " " + u.Str(t["summary"])
		})
	if prs, err := store.ListPrinciples("", ""); err == nil {
		for _, p := range prs {
			haystack := strings.ToLower(u.Str(p["title"]) + " " + u.Str(p["summary"]))
			if score := matchScore(haystack, terms); score > 0 {
				results = append(results, Hit{Type: "principle", ID: u.Str(p["id"]), Title: u.Str(p["title"]), Status: u.Str(p["status"]), Score: score})
			}
		}
	}
	return results
}

type listFn func() ([]map[string]any, error)

func listCommits() listFn {
	return func() ([]map[string]any, error) { return store.ListCommits("", "", "", "", 0) }
}
func listBugs() listFn {
	return func() ([]map[string]any, error) { return store.ListBugs("", "", "", 0) }
}
func listDecisions() listFn {
	return func() ([]map[string]any, error) { return store.ListDecisions() }
}
func listIdeas() listFn {
	return func() ([]map[string]any, error) { return store.ListIdeas("") }
}
func listRoadmaps() listFn {
	return func() ([]map[string]any, error) { return store.ListRoadmaps("") }
}
func listThreads() listFn {
	return func() ([]map[string]any, error) { return store.ListThreads("") }
}

func appendEntityHits(results *[]Hit, terms []string, typ string, fetch listFn,
	idTitleStatus func(map[string]any) (string, string, string),
	command func(string) string, haystackFn func(map[string]any) string) {
	items, err := fetch()
	if err != nil {
		return
	}
	for _, item := range items {
		id, title, status := idTitleStatus(item)
		if score := matchScore(strings.ToLower(haystackFn(item)), terms); score > 0 {
			*results = append(*results, Hit{Type: typ, ID: id, Title: title, Status: status, Score: score, Command: command(id)})
		}
	}
}

// RerankWithAI re-ranks candidates using hybrid FTS + embedding search.
func RerankWithAI(client *ai.Client, query string, limit int, candidates []Hit) []Hit {
	providers := make([]ai.SearchResultProvider, len(candidates))
	for i := range candidates {
		providers[i] = candidates[i]
	}
	// Re-rank the provided candidates instead of re-running FTS5: the caller
	// already fetched them (FTS5 or Linear). Re-querying here doubled the
	// search work and silently ignored the candidates argument.
	reranked := ai.HybridSearch(query, limit, client, func(q string, l int) []ai.SearchResultProvider {
		if len(providers) > l {
			return providers[:l]
		}
		return providers
	})
	result := make([]Hit, len(reranked))
	for i, p := range reranked {
		if h, ok := p.(Hit); ok {
			result[i] = h
		}
	}
	return result
}

func terms(query string) []string {
	var out []string
	for _, t := range strings.Fields(strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(query, "_", " "), "-", " "))) {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
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
