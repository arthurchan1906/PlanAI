package session

import (
	"encoding/json"
	"time"

	"aipmc/store"
	"aipmc/u"
)

const maxEdgeAgeDays = 7

// buildGraphEdges creates graph_edges for a single session's commits.
//   - file_touch: session → commit with file intersection
//   - same_session: commit → commit within the same session (deterministic)
func buildGraphEdges(sessionID string, touchedFiles, readFiles []string, commits []map[string]any) {
	if len(touchedFiles) == 0 {
		return
	}

	// Collect commit IDs in this session for same_session edges
	var commitIDs []string
	commitFiles := map[string][]string{}
	commitTimes := map[string]string{}

	for _, c := range commits {
		cid := u.Str(c["id"])
		var cfiles []string
		if err := json.Unmarshal([]byte(u.Str(c["files_json"])), &cfiles); err != nil || len(cfiles) == 0 {
			continue
		}
		commitIDs = append(commitIDs, cid)
		commitFiles[cid] = cfiles
		commitTimes[cid] = u.Str(c["created_at"])
	}

	if len(commitIDs) == 0 {
		return
	}

	now := time.Now()
	cutoff := now.AddDate(0, 0, -maxEdgeAgeDays).Format("2006-01-02")

	for _, cid := range commitIDs {
		cfiles := commitFiles[cid]
		ctime := commitTimes[cid]

		// Skip commits older than 7 days
		if ctime < cutoff {
			continue
		}

		// file_touch: session → commit (Jaccard similarity)
		inter := intersectFiles(touchedFiles, cfiles)
		if len(inter) > 0 {
			union := unionFiles(touchedFiles, cfiles)
			weight := float64(len(inter)) / float64(len(union))
			store.CreateGraphEdge("session", sessionID, "file_touch", "commit", cid, weight,
				map[string]any{"intersect": safeSlice(inter, 5), "session_files": len(touchedFiles), "commit_files": len(cfiles)})
		}

		// file_read: session read files that match commit files
		readInter := intersectFiles(readFiles, cfiles)
		if len(readInter) > 0 {
			weight := float64(len(readInter)) / float64(len(cfiles))
			store.CreateGraphEdge("session", sessionID, "file_read", "commit", cid, weight,
				map[string]any{"read_files": safeSlice(readInter, 3)})
		}
	}

	// same_session: commit → commit (deterministic, weight=1.0)
	for i := 0; i < len(commitIDs); i++ {
		for j := i + 1; j < len(commitIDs); j++ {
			cidA, cidB := commitIDs[i], commitIDs[j]
			if commitTimes[cidA] < cutoff || commitTimes[cidB] < cutoff {
				continue
			}
			store.CreateGraphEdge("commit", cidA, "same_session", "commit", cidB, 1.0,
				map[string]any{"session_id": sessionID})
		}
	}
}

// crossSessionEdges creates derived_from edges between sessions that share
// the same commit via file_touch edges. Only runs when projectPath is set
// (deferred until full graph is built).
func crossSessionEdges(projectPath, since string) {
	// Deferred: requires graph_edges to already have file_touch edges
	// for multiple sessions, then finds sessions touching same commits.
	_ = projectPath
	_ = since
}

func unionFiles(a, b []string) []string {
	m := map[string]bool{}
	for _, f := range a {
		m[f] = true
	}
	for _, f := range b {
		m[f] = true
	}
	var out []string
	for f := range m {
		out = append(out, f)
	}
	return out
}

