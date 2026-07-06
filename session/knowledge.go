package session

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// CrossSessionKnowledge aggregates L2 summaries across multiple sessions.
type CrossSessionKnowledge struct {
	SessionsAnalyzed int            `json:"sessions_analyzed"`
	FilePatterns     []FilePattern  `json:"file_patterns"`
	RecurringLessons []string       `json:"recurring_lessons"`
	EntityClusters   []EntityCluster `json:"entity_clusters"`
	GeneratedAt      string         `json:"generated_at"`
}

// FilePattern indicates a file touched by multiple sessions with shared issues.
type FilePattern struct {
	FilePath     string   `json:"file_path"`
	SessionCount int      `json:"session_count"`
	SessionIDs   []string `json:"session_ids"`
	CommonIssues []string `json:"common_issues"`
}

// EntityCluster groups sessions linked to the same entity.
type EntityCluster struct {
	EntityType string   `json:"entity_type"`
	EntityID   string   `json:"entity_id"`
	SessionIDs []string `json:"session_ids"`
	Topics     []string `json:"topics"`
}

// AggregateCrossSessionKnowledge processes L2 summaries and extracts
// file-level patterns, recurring lessons, and entity-session clusters.
func AggregateCrossSessionKnowledge(summaries []store.SessionSummary) CrossSessionKnowledge {
	out := CrossSessionKnowledge{
		GeneratedAt:      u.NowISO(),
		FilePatterns:     []FilePattern{},
		RecurringLessons: []string{},
		EntityClusters:   []EntityCluster{},
	}

	type parsed struct {
		sessionID string
		summary   SessionL2Summary
	}
	var items []parsed
	for _, s := range summaries {
		if s.Summary == "" {
			continue
		}
		var l2 SessionL2Summary
		if err := json.Unmarshal([]byte(s.Summary), &l2); err != nil || l2.Goal == "" {
			continue
		}
		items = append(items, parsed{sessionID: s.SessionID, summary: l2})
	}
	out.SessionsAnalyzed = len(items)
	// Fallback: when L2 AI summaries are completely unavailable (AI not configured),
	// extract file overlap and entity refs from B1 ReviewJSON Layer0Edges.
	// Only triggers when no L2 data exists — avoids double-counting sessions
	// that already have both L2 and B1 data.
	if len(items) == 0 {
		fallbackCount := 0
		for _, s := range summaries {
			if s.ReviewJSON == "" {
				continue
			}
			var review ReviewResult
			if err := json.Unmarshal([]byte(s.ReviewJSON), &review); err != nil {
				continue
			}
			for _, edge := range review.Layer0Edges {
				switch edge.Type {
				case "file_overlap":
					path := strings.TrimPrefix(edge.To, "file:")
					item := parsed{sessionID: s.SessionID, summary: SessionL2Summary{Files: []string{path}}}
					items = append(items, item)
				case "entity_ref":
					item := parsed{sessionID: s.SessionID, summary: SessionL2Summary{Entities: []string{edge.To}}}
					items = append(items, item)
				}
			}
			fallbackCount++
		}
		out.SessionsAnalyzed = fallbackCount
	}
	if len(items) == 0 {
		return out
	}

	// Build file → sessions index
	fileMap := map[string][]parsed{}
	for _, p := range items {
		for _, f := range p.summary.Files {
			fileMap[f] = append(fileMap[f], p)
		}
	}

	// Detect file patterns (≥2 sessions touching same file)
	for file, sessions := range fileMap {
		if len(sessions) < 2 {
			continue
		}
		sids := make([]string, 0, len(sessions))
		issueCounts := map[string]int{}
		for _, s := range sessions {
			sids = append(sids, s.sessionID)
			for _, fix := range s.summary.Fixes {
				issueCounts[fix]++
			}
			for _, rc := range s.summary.RootCauses {
				issueCounts[rc]++
			}
		}
		topIssues := topN(issueCounts, 3)
		out.FilePatterns = append(out.FilePatterns, FilePattern{
			FilePath:     file,
			SessionCount: len(sessions),
			SessionIDs:   sids,
			CommonIssues: topIssues,
		})
	}
	sort.Slice(out.FilePatterns, func(i, j int) bool {
		return out.FilePatterns[i].SessionCount > out.FilePatterns[j].SessionCount
	})

	// Build entity → sessions index
	entityMap := map[string][]parsed{}
	for _, p := range items {
		for _, e := range p.summary.Entities {
			entityMap[e] = append(entityMap[e], p)
		}
	}
	for entity, sessions := range entityMap {
		if len(sessions) < 2 {
			continue
		}
		parts := splitEntityID(entity)
		sids := make([]string, 0, len(sessions))
		topicCounts := map[string]int{}
		for _, s := range sessions {
			sids = append(sids, s.sessionID)
			for _, p := range s.summary.Patterns {
				topicCounts[p]++
			}
		}
		out.EntityClusters = append(out.EntityClusters, EntityCluster{
			EntityType: parts[0],
			EntityID:   entity,
			SessionIDs: sids,
			Topics:     topN(topicCounts, 3),
		})
	}

	// Collect recurring lessons (≥2 sessions mention same pattern)
	lessonCounts := map[string]int{}
	for _, p := range items {
		for _, pattern := range p.summary.Patterns {
			lessonCounts[pattern]++
		}
	}
	for lesson, count := range lessonCounts {
		if count >= 2 {
			out.RecurringLessons = append(out.RecurringLessons, lesson)
		}
	}
	sort.Strings(out.RecurringLessons)

	return out
}

// topN returns the top N entries from a count map, sorted by count descending.
func topN(counts map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	out := make([]string, 0, n)
	for i := 0; i < len(pairs) && i < n; i++ {
		out = append(out, pairs[i].k)
	}
	return out
}

// WriteLessonsFile writes aggregated cross-session knowledge as JSON.
// Path is typically ".pmai/cache/recent_lessons.json".
func WriteLessonsFile(path string, knowledge CrossSessionKnowledge) error {
	body, err := json.MarshalIndent(knowledge, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0644)
}

func splitEntityID(eid string) [2]string {
	parts := [2]string{}
	for i, ch := range eid {
		if ch == '-' {
			parts[0] = eid[:i]
			parts[1] = eid
			return parts
		}
	}
	parts[1] = eid
	return parts
}
