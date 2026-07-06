package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aipmc/ai"
	"aipmc/store"
	"aipmc/u"
)

// RunOpts controls batch session review.
type RunOpts struct {
	Since      string
	Limit      int
	SamplePath string
	Summarizer ai.Summarizer // optional AI summarizer for L2 summary generation
}

// RunResult is the CLI/MCP output for a review batch.
type RunResult struct {
	Since         string         `json:"since"`
	Reviewed      int            `json:"reviewed"`
	Completed     int            `json:"workflow_completed"`
	Baseline      int            `json:"workflow_baseline"`
	SamplePath    string         `json:"sample_path,omitempty"`
	Sessions      []ReviewResult `json:"sessions"`
	UnmergedOrphans []orphanEvent `json:"unmerged_orphans"`
}

// Run reviews recent agent sessions and writes session_summaries rows.
func Run(opts RunOpts) (RunResult, error) {
	if err := store.EnsureSessionSummariesTable(); err != nil {
		return RunResult{}, err
	}
	since := opts.Since
	if since == "" {
		since = time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	sessions, err := store.RecentAgentActivity(since, limit)
	if err != nil {
		return RunResult{}, err
	}
	orphans, err := store.ListOrphanMCPRows(since)
	if err != nil {
		return RunResult{}, err
	}

	anchors := make([]SessionAnchor, 0, len(sessions))
	for _, s := range sessions {
		anchors = append(anchors, SessionAnchor{
			Source: s.Source, SessionID: s.SessionID, FirstSeen: s.FirstSeen, LastSeen: s.LastSeen,
		})
	}
	consumed := map[string]bool{}
	merged := MergeOrphans(anchors, orphans, consumed)

	var unmerged []orphanEvent
	for _, o := range orphans {
		id := u.Str(o["id"])
		if consumed[id] {
			continue
		}
		unmerged = append(unmerged, orphanEvent{
			ID: id, Source: u.Str(o["source"]), CreatedAt: u.Str(o["created_at"]), Content: u.Str(o["content"]),
		})
	}
	if unmerged == nil {
		unmerged = []orphanEvent{}
	}

	out := RunResult{Since: since, UnmergedOrphans: unmerged}
	for _, s := range sessions {
		rows, err := store.GetSessionMessages(s.SessionID)
		if err != nil {
			continue
		}
		key := s.Source + "|" + s.SessionID
		mergedRows := merged[key]
		messages := CombineMessages(rows, mergedRows)
		review := ReviewSession(s.SessionID, s.Source, messages, len(mergedRows), nil)

		// Inject commits in the session's time window as B1 context
		// Dual source: git log (authoritative) + AIPM commits (entity links)
		if gitCommits, err := store.FindGitCommitsInWindow(s.FirstSeen, s.LastSeen); err == nil {
			review.CommitsInWindow = gitCommits
		}
		if aipmCommits, err := store.FindCommitsInWindow(s.FirstSeen, s.LastSeen); err == nil {
			// Merge AIPM commits — if not already present, add for entity context
			seen := make(map[string]bool)
			for _, c := range review.CommitsInWindow {
				seen[c.Title] = true
			}
			for _, c := range aipmCommits {
				if !seen[c.Title] {
					review.CommitsInWindow = append(review.CommitsInWindow, c)
				}
			}
		}

		// L2 semantic summary (gracefully degrades if AI not configured)
		summary := ""
		l2status := "skip_no_ai"
		if opts.Summarizer != nil {
			summary = GenerateL2Summary(messages, review, opts.Summarizer)
			if summary != "" {
				l2status = "ok"
			} else {
				l2status = "skip_empty"
			}
		}
		// Preserve existing summary if current run produced nothing
		if summary == "" {
			if old, _ := store.GetSessionSummary(s.SessionID); old != nil && old.Summary != "" {
				summary = old.Summary
				l2status = "cached"
			}
		}
		// Per-session observability log
		goalPreview := ""
		if summary != "" {
			var l2 SessionL2Summary
			if json.Unmarshal([]byte(summary), &l2) == nil && l2.Goal != "" {
				goalPreview = l2.Goal
				if len(goalPreview) > 60 {
					goalPreview = goalPreview[:60] + "..."
				}
			}
		}
		if goalPreview != "" {
			u.LogShared("PIPELINE", "session=%s src=%s intent=%s score=%d files=%d entities=%d L2=%s goal=%s",
				s.SessionID[:8], s.Source, review.Intent, review.QualityScoreValue(),
				len(review.Layer0FilePaths()), len(review.Layer0EntityIDs()), l2status, goalPreview)
		} else {
			u.LogShared("PIPELINE", "session=%s src=%s intent=%s score=%d files=%d entities=%d L2=%s",
				s.SessionID[:8], s.Source, review.Intent, review.QualityScoreValue(),
				len(review.Layer0FilePaths()), len(review.Layer0EntityIDs()), l2status)
		}

		entityRefs := review.EntityRefsJSON()
		if err := store.UpsertSessionSummary(store.SessionSummary{
			SessionID:    s.SessionID,
			Source:       s.Source,
			ReviewJSON:   review.ReviewJSON(),
			Summary:      summary,
			Intent:       review.Intent,
			EntityRefs:   entityRefs,
			QualityScore: review.QualityScoreValue(),
			CreatedAt:    u.NowISO(),
		}); err != nil {
			return out, err
		}

		out.Sessions = append(out.Sessions, review)
		out.Reviewed++
		if review.WorkflowBaseline {
			out.Baseline++
		}
		if review.WorkflowCompleted {
			out.Completed++
		}
	}

	if opts.SamplePath != "" {
		if err := writeSample(opts.SamplePath, out.Sessions, limit); err != nil {
			return out, err
		}
		out.SamplePath = opts.SamplePath
	}

	// Write cross-session lessons file if L2 summaries exist
	if summaries, err := store.ListSessionSummariesWithSummary("", 50); err == nil && len(summaries) > 0 {
		knowledge := AggregateCrossSessionKnowledge(summaries)
		lessonsPath := ".pmai/cache/recent_lessons.json"
		if err := WriteLessonsFile(lessonsPath, knowledge); err != nil {
				// non-fatal
			}
	}

	return out, nil
}

func writeSample(path string, sessions []ReviewResult, max int) error {
	if max <= 0 {
		max = 20
	}
	sample := sessions
	if len(sample) > max {
		sample = sample[:max]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	b, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// ParseSince converts CLI values like 24h, 7d, or ISO timestamps.
func ParseSince(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05")
	}
	if t, err := time.Parse("2006-01-02T15:04:05", raw); err == nil {
		return t.Format("2006-01-02T15:04:05")
	}
	if len(raw) >= 2 {
		n, err := strconv.Atoi(raw[:len(raw)-1])
		if err == nil {
			switch raw[len(raw)-1] {
			case 'h', 'H':
				return time.Now().Add(-time.Duration(n) * time.Hour).Format("2006-01-02T15:04:05")
			case 'd', 'D':
				return time.Now().Add(-time.Duration(n) * 24 * time.Hour).Format("2006-01-02T15:04:05")
			}
		}
	}
	return time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05")
}
