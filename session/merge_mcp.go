package session

import (
	"strings"
	"time"

	"aipmc/store"
	"aipmc/u"
)

// MCPMergeWindow is the ± duration for orphan MCP row attachment.
const MCPMergeWindow = 5 * time.Second

// IsOrphanSessionID reports whether a discussion row lacks a real session id.
func IsOrphanSessionID(sessionID string) bool {
	return sessionID == "" || sessionID == "unknown"
}

// MergeOrphans attaches orphan MCP rows to sessions by source and timestamp proximity.
// When multiple sessions match, the nearest (preferring shorter sessions on tie) wins.
func MergeOrphans(projectPath string,
	sessions []SessionAnchor,
	orphans []map[string]any,
	consumed map[string]bool,
) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, o := range orphans {
		id := u.Str(o["id"])
		if consumed[id] {
			continue
		}
		src := u.Str(o["source"])
		ts, ok := parseISO(u.Str(o["created_at"]))
		if !ok {
			continue
		}
		key, ok := bestSessionMatch(sessions, src, ts)
		if !ok {
			continue
		}
		out[key] = append(out[key], o)
		consumed[id] = true
		// Write session_id back to discussion_log
		parts := strings.SplitN(key, "|", 2)
		if len(parts) == 2 {
			store.UpdateDiscussionSessionIDFor(projectPath, id, parts[1])
		}
	}
	return out
}

func bestSessionMatch(sessions []SessionAnchor, source string, ts time.Time) (string, bool) {
	type candidate struct {
		key      string
		distance float64
		span     float64
	}
	var best *candidate
	for _, s := range sessions {
		if s.Source != source {
			continue
		}
		start, ok1 := parseISO(s.FirstSeen)
		end, ok2 := parseISO(s.LastSeen)
		if !ok1 || !ok2 {
			continue
		}
		windowStart := start.Add(-MCPMergeWindow)
		windowEnd := end.Add(MCPMergeWindow)
		if ts.Before(windowStart) || ts.After(windowEnd) {
			continue
		}
		dist := intervalDistance(ts, start, end)
		span := end.Sub(start).Seconds()
		c := candidate{key: s.Key(), distance: dist, span: span}
		if best == nil || c.distance < best.distance || (c.distance == best.distance && c.span < best.span) {
			copy := c
			best = &copy
		}
	}
	if best == nil {
		return "", false
	}
	return best.key, true
}

func intervalDistance(ts, start, end time.Time) float64 {
	if !ts.Before(start) && !ts.After(end) {
		return 0
	}
	if ts.Before(start) {
		return start.Sub(ts).Seconds()
	}
	return ts.Sub(end).Seconds()
}

// SessionAnchor identifies a session for MCP merge.
type SessionAnchor struct {
	Source, SessionID, FirstSeen, LastSeen string
}

func (s SessionAnchor) Key() string {
	return s.Source + "|" + s.SessionID
}

func parseISO(iso string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02T15:04:05", iso)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// CombineMessages merges session rows with attached orphan MCP rows in chronological order.
func CombineMessages(sessionRows, mergedOrphans []map[string]any) []map[string]any {
	combined := make([]map[string]any, 0, len(sessionRows)+len(mergedOrphans))
	combined = append(combined, sessionRows...)
	combined = append(combined, mergedOrphans...)
	sortByCreatedAt(combined)
	return combined
}

func sortByCreatedAt(rows []map[string]any) {
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if u.Str(rows[i]["created_at"]) > u.Str(rows[j]["created_at"]) {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
}
