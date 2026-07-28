package store

import (
	"database/sql"
	"encoding/json"

	pmdb "aipmc/db"
)

// TraceResult is the output of a graph trace operation.
type TraceResult struct {
	FromType string      `json:"from_type"`
	FromID   string      `json:"from_id"`
	Edges    []TraceEdge `json:"edges"`
	Summary  TraceSummary `json:"summary"`
}

// TraceEdge is a single directed edge from the trace origin.
type TraceEdge struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	EdgeType   string  `json:"edge_type"`
	TargetType string  `json:"target_type"`
	TargetID   string  `json:"target_id"`
	Weight     float64 `json:"weight"`
	Direction  string  `json:"direction"` // "out" or "in"
}

// TraceSummary provides aggregated stats for the trace.
type TraceSummary struct {
	TotalEdges        int            `json:"total_edges"`
	ByEdgeType        map[string]int `json:"by_edge_type"`
	ConnectedEntities map[string]int `json:"connected_entities"`
}

// TraceContext traces graph_edges from a given entity.
// direction: "out" (entity as source), "in" (entity as target), "both" (default).
// minWeight: minimum edge weight (0 = no filter).
// limit: max edges to return (0 = default 200).
func TraceContext(fromType, fromID, direction string, minWeight float64, limit int) (*TraceResult, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if limit <= 0 {
		limit = 200
	}

	var rows *sql.Rows

	switch direction {
	case "in":
		rows, err = db.Query(
			`SELECT source_type, source_id, edge_type, target_type, target_id, weight
			 FROM graph_edges WHERE target_type=? AND target_id=? AND weight>=?
			 ORDER BY weight DESC LIMIT ?`,
			fromType, fromID, minWeight, limit)
	case "out":
		rows, err = db.Query(
			`SELECT source_type, source_id, edge_type, target_type, target_id, weight
			 FROM graph_edges WHERE source_type=? AND source_id=? AND weight>=?
			 ORDER BY weight DESC LIMIT ?`,
			fromType, fromID, minWeight, limit)
	default: // "both"
		rows, err = db.Query(
			`SELECT source_type, source_id, edge_type, target_type, target_id, weight,
			        CASE WHEN source_type=?1 AND source_id=?2 THEN 'out' ELSE 'in' END as dir
			 FROM graph_edges
			 WHERE ((source_type=?1 AND source_id=?2) OR (target_type=?1 AND target_id=?2))
			   AND weight >= ?3
			 ORDER BY weight DESC LIMIT ?4`,
			fromType, fromID, minWeight, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &TraceResult{
		FromType: fromType,
		FromID:   fromID,
		Summary: TraceSummary{
			ByEdgeType:        map[string]int{},
			ConnectedEntities: map[string]int{},
		},
	}

	for rows.Next() {
		var st, sid, et, tt, tid string
		var w float64

		if direction == "both" {
			var dir string
			if err := rows.Scan(&st, &sid, &et, &tt, &tid, &w, &dir); err != nil {
				continue
			}
			result.Edges = append(result.Edges, TraceEdge{st, sid, et, tt, tid, w, dir})
		} else {
			if err := rows.Scan(&st, &sid, &et, &tt, &tid, &w); err != nil {
				continue
			}
			result.Edges = append(result.Edges, TraceEdge{st, sid, et, tt, tid, w, direction})
		}
	}

	if result.Edges == nil {
		result.Edges = []TraceEdge{}
	}

	// Build summary
	result.Summary.TotalEdges = len(result.Edges)
	for _, e := range result.Edges {
		result.Summary.ByEdgeType[e.EdgeType]++
		if e.Direction == "in" {
			result.Summary.ConnectedEntities[e.SourceType+"(←)"]++
		} else {
			result.Summary.ConnectedEntities[e.TargetType+"(→)"]++
		}
	}

	return result, nil
}

// TraceContextJSON wraps TraceContext for MCP output.
func TraceContextJSON(fromType, fromID, direction string, minWeight float64, limit int) (string, error) {
	r, err := TraceContext(fromType, fromID, direction, minWeight, limit)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ListSessionsWithEdges returns distinct session IDs that have graph edges.
func ListSessionsWithEdges(limit int) ([]string, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT DISTINCT source_id FROM graph_edges WHERE source_type='session'
		 UNION
		 SELECT DISTINCT target_id FROM graph_edges WHERE target_type='session'
		 ORDER BY 1 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}
