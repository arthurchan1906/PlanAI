package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// ReconcileResult is the output of a reconciliation run.
type ReconcileResult struct {
	Since          string          `json:"since"`
	SessionsReviewed int           `json:"sessions_reviewed"`
	AutoLinked     []LinkAction    `json:"auto_linked"`
	TentativeLinks []TentativeLink `json:"tentative_links"`
	Suggestions    []Suggestion    `json:"suggestions"`
}

// LinkAction is an automatically executed entity link.
type LinkAction struct {
	SessionID  string `json:"session_id"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Confidence string `json:"confidence"` // "hard" or "soft"
	Reason     string `json:"reason"`
}

// TentativeLink is a low-confidence link for manual confirmation.
type TentativeLink struct {
	SessionID  string  `json:"session_id"`
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	TargetType string  `json:"target_type"`
	TargetID   string  `json:"target_id"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	EventID    string  `json:"event_id"` // PM event created for this
}

// Suggestion is a recommended action that requires human judgment.
type Suggestion struct {
	Action   string `json:"action"`   // "create_task", "assign_plan", "merge_sessions"
	Title    string `json:"title"`    // suggested entity title
	Context  string `json:"context"`  // why this suggestion
	SourceID string `json:"source_id"` // originating entity (commit/session)
}

const reconcileWindow = 30 // minutes around session for commit matching

// Reconcile scans recent sessions and commits, auto-links related entities,
// and generates tentative events for low-confidence matches.
// projectPath overrides CWD for multi-project scanning.
func Reconcile(since, projectPath string) (ReconcileResult, error) {
	if projectPath != "" {
		home, _ := os.Getwd()
		if err := os.Chdir(projectPath); err != nil {
			return ReconcileResult{}, err
		}
		defer os.Chdir(home)
	}
	out := ReconcileResult{Since: since}

	summaries, err := store.ListSessionSummariesSince(since, 50)
	if err != nil {
		return out, err
	}
	out.SessionsReviewed = len(summaries)

	for _, ss := range summaries {
		// Get session messages for Hard-path file extraction (works without L2)
		messages, err := store.GetSessionMessages(ss.SessionID)
		if err != nil || len(messages) == 0 {
			continue
		}

		touchedFiles, readFiles := classifyFiles(messages)
		u.LogShared("PIPELINE", "L3 session=%s messages=%d touched=%d read=%d",
			u.Prefix(ss.SessionID, 8), len(messages), len(touchedFiles), len(readFiles))
		if len(touchedFiles) == 0 {
			continue
		}

		// Parse L2 summary for Soft-path data (optional, may be empty)
		var l2 SessionL2Summary
		hasL2 := false
		if ss.Summary != "" {
			if err := json.Unmarshal([]byte(ss.Summary), &l2); err == nil && l2.Goal != "" {
				hasL2 = true
			}
		}

		// Find commits in time window
		commits, err := store.ListCommits("", "", "", ss.CreatedAt, 200)
		if err != nil {
			continue
		}

		for _, c := range commits {
			cid := u.Str(c["id"])
			ctitle := u.Str(c["title"])
			ctaskID := u.Str(c["task_id"])
			cfilesJSON := u.Str(c["files_json"])

			var commitFiles []string
			if err := json.Unmarshal([]byte(cfilesJSON), &commitFiles); err != nil {
					commitFiles = []string{}
				}

			// Dual-path file matching
			hardMatches := intersectFiles(touchedFiles, commitFiles)
			var softMatches []string
				if hasL2 {
					softMatches = intersectFiles(l2.Files, commitFiles)
				}

			switch {
			case len(hardMatches) >= 2:
				// High confidence: auto-link commit ↔ session
				out.AutoLinked = append(out.AutoLinked, LinkAction{
					SessionID:  ss.SessionID,
					SourceType: "commit",
					SourceID:   cid,
					TargetType: "session",
					TargetID:   ss.SessionID,
					Confidence: "hard",
					Reason:     fmt.Sprintf("2+ file intersection: %s", strings.Join(safeSlice(hardMatches, 3), ", ")),
				})
				u.LogShared("PIPELINE", "L3 link commit=%s session=%s confidence=hard files=%d", u.Prefix(cid, 8), u.Prefix(ss.SessionID, 8), len(hardMatches))
				// Also link commit → task if task exists
				if ctaskID != "" {
					out.AutoLinked = append(out.AutoLinked, LinkAction{
						SessionID:  ss.SessionID,
						SourceType: "commit",
						SourceID:   cid,
						TargetType: "task",
						TargetID:   ctaskID,
						Confidence: "hard",
						Reason:     "commit already linked to task",
					})
				} else {
					// Suggest task creation
					taskTitle := fmt.Sprintf("[auto] %s", ctitle)
					out.Suggestions = append(out.Suggestions, Suggestion{
						Action:   "create_task",
						Title:    taskTitle,
						Context:  fmt.Sprintf("session %s modified files matching commit %s", u.Prefix(ss.SessionID, 8), u.Prefix(cid, 8)),
						SourceID: cid,
					})
				}

			case len(hardMatches) == 1 || len(softMatches) >= 1:
				// Medium confidence: generate tentative event
				reason := fmt.Sprintf("%d hard + %d soft file matches", len(hardMatches), len(softMatches))
				eventID := createTentativeEvent("commit", cid, ss.SessionID, reason)
				out.TentativeLinks = append(out.TentativeLinks, TentativeLink{
					SessionID:  ss.SessionID,
					SourceType: "commit",
					SourceID:   cid,
					TargetType: "session",
					TargetID:   ss.SessionID,
					Confidence: float64(len(hardMatches)*2 + len(softMatches)) / 4.0,
					Reason:     reason,
					EventID:    eventID,
				})
				u.LogShared("PIPELINE", "L3 tentative commit=%s session=%s confidence=%.2f", u.Prefix(cid, 8), u.Prefix(ss.SessionID, 8), float64(len(hardMatches)*2+len(softMatches))/4.0)
			}
		}

		// Execute auto-links for this session's batch
		for _, link := range out.AutoLinked {
			if link.SessionID == ss.SessionID {
				store.CreateLink("", link.SourceType, link.SourceID, "relates_to",
					link.TargetType, link.TargetID,
					fmt.Sprintf("reconcile: %s", link.Reason))
			}
		}

		// Cross-session: check for sessions touching same files
		// (deferred to cross-session aggregation in knowledge.go)
	}

	if out.AutoLinked == nil {
		out.AutoLinked = []LinkAction{}
	}
	if out.TentativeLinks == nil {
		out.TentativeLinks = []TentativeLink{}
	}
	if out.Suggestions == nil {
		out.Suggestions = []Suggestion{}
	}

	return out, nil
}

// createTentativeEvent records a low-confidence link as a PM event.
func createTentativeEvent(sourceType, sourceID, sessionID, reason string) string {
	prefix := sessionID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	summary := fmt.Sprintf("reconcile: session %s may relate to %s %s (reason: %s, confidence: low)",
		prefix, sourceType, sourceID, reason)
	evt, err := store.CreateEvent("tentative_link", sourceType, sourceID, summary)
	if err != nil {
		return ""
	}
	return u.Str(evt["id"])
}

// intersectFiles returns the intersection of two file path slices.
func intersectFiles(a, b []string) []string {
	// Match by filename suffix (basename) for robustness
	set := map[string]bool{}
	for _, f := range a {
		set[f] = true
	}

	var out []string
	for _, f := range b {
		if set[f] {
			out = append(out, f)
			continue
		}
		// Also try basename matching for cross-repo scenarios
		for _, af := range a {
			if basename(af) == basename(f) && af != f {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// safeSlice returns s[:n] without panicking when cap(s) < n.
func safeSlice(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// basename extracts the filename portion of a path.
func basename(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx >= 0 {
		return path[idx+1:]
	}
	return path
}
