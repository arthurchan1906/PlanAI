package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"aipmc/store"
	"aipmc/u"
)

const maxEmergPerRule = 5

// emergeOnce runs all emergence detection rules for the given project.
// It temporarily switches CWD to projectPath so that CWD-based store
// functions (CreateEvent, ListEvents, etc.) write to the correct database.
func emergeOnce(projectPath string) {
	if projectPath != "" {
		home, _ := os.Getwd()
		if err := os.Chdir(projectPath); err != nil {
			u.LogShared("EMERGE", "chdir fail %s: %v", projectPath, err)
			return
		}
		defer os.Chdir(home)
	}

	commitOrphans()
	staleFileTasks()
	hotspotUntracked()
}

// commitOrphans flags commits from last 7 days that have no task link.
func commitOrphans() {
	since := time.Now().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05")
	commits, err := store.ListCommits("", "", "", since, 200)
	if err != nil {
		return
	}
	count := 0
	for _, c := range commits {
		if count >= maxEmergPerRule {
			break
		}
		cid := u.Str(c["id"])
		ctitle := u.Str(c["title"])
		// Check for existing task links (both directions)
		hasTask := false
		for _, dir := range [][3]string{{cid, "", ""}, {"", cid, ""}} {
			links, _ := store.ListLinks(dir[0], dir[1], dir[2])
			for _, l := range links {
				if u.Str(l["target_type"]) == "task" || u.Str(l["source_type"]) == "task" {
					hasTask = true
					break
				}
			}
			if hasTask {
				break
			}
		}
		if hasTask || dupEvent("commit_orphan", "commit", cid) {
			continue
		}
		store.CreateEvent("commit_orphan", "commit", cid,
			fmt.Sprintf("orphan commit '%s' has no task link", ctitle))
		count++
	}
	if count > 0 {
		u.LogShared("EMERGE", "orphans=%d", count)
	}
}

// staleFileTasks finds done tasks whose linked files still appear in newer commits.
func staleFileTasks() {
	tasks, err := store.ListTasks("done", "")
	if err != nil || len(tasks) == 0 {
		return
	}
	count := 0
	for _, t := range tasks {
		if count >= maxEmergPerRule {
			break
		}
		tid := t.ID
		ttitle := t.Title
		// Find the newest commit linked to this task
		links, _ := store.ListLinks("", tid, "")
		var newestTime string
		var taskCommitIDs []string
		for _, l := range links {
			if u.Str(l["source_type"]) == "commit" {
				cid := u.Str(l["source_id"])
				taskCommitIDs = append(taskCommitIDs, cid)
				cm, err := store.GetCommit(cid)
				if err == nil && cm != nil {
					if ct := u.Str(cm["created_at"]); ct > newestTime {
						newestTime = ct
					}
				}
			}
		}
		if newestTime == "" {
			continue
		}
		// Find commits newer than the task's newest commit
		newerCommits, _ := store.ListCommits("", "", "", newestTime, 200)
		stale := false
		for _, nc := range newerCommits {
			cid := u.Str(nc["id"])
			// Skip commits already linked to this task
			own := false
			for _, tcid := range taskCommitIDs {
				if tcid == cid {
					own = true
					break
				}
			}
			if own {
				continue
			}
			// Check file overlap via graph_edges
			for _, tcid := range taskCommitIDs {
				edges, _ := store.ListGraphEdges("", tcid, "file_touch")
				for _, e := range edges {
					if u.Str(e["target_type"]) == "commit" && u.Str(e["target_id"]) == cid {
						stale = true
						break
					}
				}
				if stale {
					break
				}
			}
			if stale {
				break
			}
		}
		if !stale || dupEvent("task_stale_file", "task", tid) {
			continue
		}
		store.CreateEvent("task_stale_file", "task", tid,
			fmt.Sprintf("done task '%s' — files still modified by newer commits", ttitle))
		count++
	}
	if count > 0 {
		u.LogShared("EMERGE", "stale=%d", count)
	}
}

// hotspotUntracked finds files modified by 2+ sessions with no tracking task.
func hotspotUntracked() {
	sessions, err := store.ListSessionsWithEdges(50)
	if err != nil || len(sessions) < 2 {
		return
	}
	// Collect files per session from file_touch evidence
	sessFiles := map[string]map[string]bool{}
	globalCount := map[string]int{}
	for _, sid := range sessions {
		edges, _ := store.ListGraphEdges(sid, "", "file_touch")
		files := map[string]bool{}
		for _, e := range edges {
			evJSON := u.Str(e["evidence_json"])
			extractFiles(evJSON, "intersect", files)
		}
		if len(files) > 0 {
			sessFiles[sid] = files
			for f := range files {
				globalCount[f]++
			}
		}
	}
	count := 0
	for f, sc := range globalCount {
		if count >= maxEmergPerRule {
			break
		}
		if sc < 2 {
			continue
		}
		if dupEvent("hotspot_untracked", "file", f) {
			continue
		}
		// Count sessions touching this file
		var sids []string
		for sid, files := range sessFiles {
			if files[f] {
				sids = append(sids, u.Prefix(sid, 8))
			}
		}
		store.CreateEvent("hotspot_untracked", "file", f,
			fmt.Sprintf("file '%s' modified by %d sessions (%s) without task", f, sc, strings.Join(sids, ",")))
		count++
	}
	if count > 0 {
		u.LogShared("EMERGE", "hotspot=%d", count)
	}
}

// helpers

func dupEvent(typ, entityType, eid string) bool {
	return store.HasEvent(typ, entityType, eid)
}

func extractFiles(evJSON, key string, out map[string]bool) {
	// Parse evidence JSON to extract "intersect" array, then add filenames
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(evJSON), &m); err != nil {
		// Fallback: crude extraction
		return
	}
	if arr, ok := m[key].([]interface{}); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				out[s] = true
			}
		}
	}
}
