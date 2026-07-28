package session

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"aipmc/store"
	"aipmc/u"
)

const maxEdgeAgeDays = 7

// buildGraphEdges creates graph_edges for a single session's commits.
//   - file_touch: session → commit with file intersection (Jaccard × IDF)
//   - file_read: session → commit with read-file intersection
// same_session edges are built separately via buildSameSessionEdges
// after reconcile has confirmed session↔commit links.
func buildGraphEdges(sessionID string, touchedFiles, readFiles []string, commits []map[string]any) {
	if len(touchedFiles) == 0 {
		return
	}

	// Compute IDF (inverse document frequency) across all commits.
	// Hotspot files that appear in many commits get low IDF → low weight.
	idf := computeIDF(commits)

	commitFiles := map[string][]string{}
	commitTimes := map[string]string{}

	for _, c := range commits {
		cid := u.Str(c["id"])
		var cfiles []string
		if err := json.Unmarshal([]byte(u.Str(c["files_json"])), &cfiles); err != nil || len(cfiles) == 0 {
			continue
		}
		commitFiles[cid] = cfiles
		commitTimes[cid] = u.Str(c["created_at"])
	}

	if len(commitFiles) == 0 {
		return
	}

	now := time.Now()
	cutoff := now.AddDate(0, 0, -maxEdgeAgeDays).Format("2006-01-02")

	for cid, cfiles := range commitFiles {
		ctime := commitTimes[cid]
		if ctime < cutoff {
			continue
		}

		// file_touch: session → commit (Jaccard × IDF)
		inter := intersectFiles(touchedFiles, cfiles)
		if len(inter) >= 2 {
			union := unionFiles(touchedFiles, cfiles)
			jaccard := float64(len(inter)) / float64(len(union))
			avgIDF := avgFileIDF(inter, idf)
			weight := jaccard * avgIDF
			if weight >= 0.1 {
				store.CreateGraphEdge("session", sessionID, "file_touch", "commit", cid, weight,
					map[string]any{
						"intersect":     safeSlice(inter, 5),
						"session_files": len(touchedFiles),
						"commit_files":  len(cfiles),
						"jaccard":       fmt.Sprintf("%.3f", jaccard),
						"avg_idf":       fmt.Sprintf("%.3f", avgIDF),
					})
			}
		}

		// file_read: session read files that match commit files
		readInter := intersectFiles(readFiles, cfiles)
		if len(readInter) > 0 {
			weight := float64(len(readInter)) / float64(len(cfiles))
			store.CreateGraphEdge("session", sessionID, "file_read", "commit", cid, weight,
				map[string]any{"read_files": safeSlice(readInter, 3)})
		}
	}
}

// buildSameSessionEdges creates same_session edges between commits that
// reconcile has linked to the same session. Only commits with confirmed
// session membership get connected — no time-window heuristics.
func buildSameSessionEdges(links []LinkAction) {
	// Group commit IDs by session
	sessionCommits := map[string][]string{}
	for _, link := range links {
		if link.SourceType == "commit" && link.TargetType == "session" {
			sessionCommits[link.TargetID] = append(sessionCommits[link.TargetID], link.SourceID)
		}
	}

	for sessionID, commits := range sessionCommits {
		if len(commits) < 2 {
			continue
		}
		// Create edges between all pairs of commits in the same session
		for i := 0; i < len(commits); i++ {
			for j := i + 1; j < len(commits); j++ {
				store.CreateGraphEdge("commit", commits[i], "same_session", "commit", commits[j], 1.0,
					map[string]any{"session_id": sessionID})
			}
		}
	}
}

// crossSessionEdges creates derived_from edges between sessions that share
// the same commit via file_touch edges.
func crossSessionEdges(projectPath, since string) {
	// Deferred: requires graph_edges to already have file_touch edges
	// for multiple sessions, then finds sessions touching same commits.
	_ = projectPath
	_ = since
}

// computeIDF returns an IDF map for all unique file paths found in commits.
// IDF = ln(N / df) where N = total commits with files, df = # commits containing the file.
// Hotspot files that appear in many commits get IDF close to 0.
func computeIDF(commits []map[string]any) map[string]float64 {
	df := map[string]int{} // document frequency per file path
	var total int
	seenPerCommit := map[string]bool{}
	for _, c := range commits {
		var cfiles []string
		if err := json.Unmarshal([]byte(u.Str(c["files_json"])), &cfiles); err != nil || len(cfiles) == 0 {
			continue
		}
		total++
		for k := range seenPerCommit {
			delete(seenPerCommit, k)
		}
		for _, f := range cfiles {
			// Normalize: strip leading dot-path entries like ".schema"
			if strings.HasPrefix(f, ".") && !strings.Contains(f, "/") {
				continue
			}
			if seenPerCommit[f] {
				continue
			}
			seenPerCommit[f] = true
			df[f]++
		}
	}
	if total == 0 {
		return map[string]float64{}
	}
	idf := make(map[string]float64, len(df))
	for f, d := range df {
		idf[f] = math.Log(float64(total) / float64(d))
	}
	return idf
}

// avgFileIDF returns the average IDF of a set of file paths.
func avgFileIDF(files []string, idf map[string]float64) float64 {
	if len(files) == 0 {
		return 0
	}
	var sum float64
	for _, f := range files {
		if v, ok := idf[f]; ok {
			sum += v
		}
	}
	if sum == 0 {
		return 0.1 // floor: even all-hotspot files get minimal weight
	}
	return sum / float64(len(files))
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
