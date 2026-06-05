package main

import (
	"fmt"
	"strings"
	"time"
)

// ============================================================
// Analysis Engine — 分析引擎
// ============================================================

// DriftResult indicates a commit that may have drifted from its task's plan scope.
type DriftResult struct {
	CommitID     string   `json:"commit_id"`
	CommitTitle  string   `json:"commit_title"`
	TaskID       string   `json:"task_id"`
	TaskTitle    string   `json:"task_title"`
	PlanID       string   `json:"plan_id"`
	PlanTitle    string   `json:"plan_title"`
	ChangedFiles []string `json:"changed_files"`
	OutOfScope   []string `json:"out_of_scope"`
	Severity     string   `json:"severity"` // "warn" | "info"
}

// OrphanResult indicates a task that's in_progress but has no commits.
type OrphanResult struct {
	TaskID          string `json:"task_id"`
	TaskTitle       string `json:"task_title"`
	PlanID          string `json:"plan_id"`
	Status          string `json:"status"`
	DaysSinceUpdate int    `json:"days_since_update"`
}

// DuplicateResult indicates potentially duplicate plans or tasks.
type DuplicateResult struct {
	EntityType string  `json:"entity_type"`
	ID1        string  `json:"id1"`
	Title1     string  `json:"title1"`
	ID2        string  `json:"id2"`
	Title2     string  `json:"title2"`
	Similarity float64 `json:"similarity"`
}

// BlockedResult indicates a task that has been blocked too long.
type BlockedResult struct {
	TaskID          string `json:"task_id"`
	TaskTitle       string `json:"task_title"`
	PlanID          string `json:"plan_id"`
	DaysBlocked     int    `json:"days_blocked"`
	LastNote        string `json:"last_note"`
}

// CrossTaskResult indicates a commit that touches files related to another active task.
type CrossTaskResult struct {
	CommitID    string `json:"commit_id"`
	CommitTitle string `json:"commit_title"`
	TaskID      string `json:"task_id"`
	TaskTitle   string `json:"task_title"`
	OtherTaskID string `json:"other_task_id"`
	OtherTitle  string `json:"other_title"`
	SharedFiles []string `json:"shared_files"`
}

// ConflictResult indicates two tasks under the same plan with potentially conflicting approaches.
type ConflictResult struct {
	TaskID1  string `json:"task_id1"`
	Title1   string `json:"title1"`
	TaskID2  string `json:"task_id2"`
	Title2   string `json:"title2"`
	PlanID   string `json:"plan_id"`
	Reason   string `json:"reason"`
}

// ProgressResult indicates plan progress vs time remaining.
type ProgressResult struct {
	PlanID       string  `json:"plan_id"`
	PlanTitle    string  `json:"plan_title"`
	TotalTasks   int     `json:"total_tasks"`
	DoneTasks    int     `json:"done_tasks"`
	ProgressPct  int     `json:"progress_pct"`
	DaysLeft     int     `json:"days_left"`
	RiskLevel    string  `json:"risk_level"` // "on_track" | "at_risk" | "off_track"
}

// ImpactResult indicates tasks affected by a decision change.
type ImpactResult struct {
	DecisionID    string   `json:"decision_id"`
	DecisionTitle string   `json:"decision_title"`
	AffectedPlans []string `json:"affected_plans"`
	AffectedTasks []string `json:"affected_tasks"`
}

// AnalyzeReport is the top-level analysis result.
type AnalyzeReport struct {
	Drifts      []DriftResult      `json:"drifts"`
	Orphans     []OrphanResult     `json:"orphans"`
	Duplicates  []DuplicateResult  `json:"duplicates"`
	Blocked     []BlockedResult    `json:"blocked"`
	Conflicts   []ConflictResult   `json:"conflicts"`
	Progress    []ProgressResult   `json:"progress"`
	Impacts     []ImpactResult     `json:"impacts"`
	CrossTasks  []CrossTaskResult  `json:"cross_tasks"`
	Summary     string             `json:"summary"`
}

// analyzeScopeDrift checks all commits for files that may fall outside their plan's scope.
func analyzeScopeDrift() []DriftResult {
	commits, err := listCommits("", "", "", "", 0)
	if err != nil {
		return nil
	}

	var results []DriftResult
	for _, c := range commits {
		taskID := str(c["task_id"])
		if taskID == "" {
			continue
		}

		// Get the files changed in this commit
		files := c["files"]
		filesList, _ := files.([]any)
		if len(filesList) == 0 {
			continue
		}

		// Get the task to find its plan
		task, err := getTaskSimple(taskID)
		if err != nil {
			continue
		}
		planID := str(task["plan_id"])
		if planID == "" {
			continue
		}

		// Get the plan to check its scope
		plan, err := getPlan(planID)
		if err != nil {
			continue
		}

		scope := plan["scope"]
		scopeList, _ := scope.([]any)
		if len(scopeList) == 0 {
			// Plan has no defined scope — nothing to check
			continue
		}

		// Build a set of scope keywords for matching
		scopeKeywords := make([]string, 0, len(scopeList))
		for _, item := range scopeList {
			if s, ok := item.(string); ok {
				scopeKeywords = append(scopeKeywords, strings.ToLower(s))
			}
		}

		// Check each changed file against the scope
		var outOfScope []string
		for _, f := range filesList {
			fileName, ok := f.(string)
			if !ok {
				continue
			}
			fileNameLower := strings.ToLower(fileName)

			matched := false
			for _, kw := range scopeKeywords {
				if strings.Contains(fileNameLower, kw) {
					matched = true
					break
				}
			}
			if !matched {
				outOfScope = append(outOfScope, fileName)
			}
		}

		if len(outOfScope) > 0 {
			changedFiles := make([]string, 0, len(filesList))
			for _, f := range filesList {
				if s, ok := f.(string); ok {
					changedFiles = append(changedFiles, s)
				}
			}
			results = append(results, DriftResult{
				CommitID:     str(c["id"]),
				CommitTitle:  str(c["title"]),
				TaskID:       taskID,
				TaskTitle:    str(task["title"]),
				PlanID:       planID,
				PlanTitle:    str(plan["title"]),
				ChangedFiles: changedFiles,
				OutOfScope:   outOfScope,
				Severity:     "warn",
			})
		}
	}
	if results == nil {
		results = []DriftResult{}
	}
	return results
}

// analyzeOrphanTasks finds tasks that are in_progress but have no linked commits.
func analyzeOrphanTasks() []OrphanResult {
	tasks, err := listTasks("in_progress", "")
	if err != nil {
		return nil
	}

	var results []OrphanResult
	for _, t := range tasks {
		commits, err := listCommitsByTask(t.ID)
		if err != nil {
			continue
		}
		if len(commits) == 0 {
			results = append(results, OrphanResult{
				TaskID:    t.ID,
				TaskTitle: t.Title,
				PlanID:    t.PlanID,
				Status:    t.Status,
			})
		}
	}
	if results == nil {
		results = []OrphanResult{}
	}
	return results
}

// analyzeDuplicatePlans finds plans with similar titles that may be duplicates.
func analyzeDuplicatePlans() []DuplicateResult {
	plans, err := listPlans("", "")
	if err != nil {
		return nil
	}

	// Filter out inactive plans for duplicate detection
	var activePlans []map[string]any
	for _, p := range plans {
		s := str(p["status"])
		if s != "cancelled" && s != "archived" {
			activePlans = append(activePlans, p)
		}
	}

	var results []DuplicateResult
	for i := 0; i < len(activePlans); i++ {
		for j := i + 1; j < len(activePlans); j++ {
			title1 := str(activePlans[i]["title"])
			title2 := str(activePlans[j]["title"])
			sim := titleSimilarity(title1, title2)
			if sim > 0.7 {
				results = append(results, DuplicateResult{
					EntityType: "plan",
					ID1:        str(activePlans[i]["id"]),
					Title1:     title1,
					ID2:        str(activePlans[j]["id"]),
					Title2:     title2,
					Similarity: sim,
				})
			}
		}
	}
	if results == nil {
		results = []DuplicateResult{}
	}
	return results
}

// analyzeBlockedTasks finds tasks that have been blocked for too long.
func analyzeBlockedTasks() []BlockedResult {
	tasks, err := listTasks("blocked", "")
	if err != nil {
		return nil
	}

	var results []BlockedResult
	for _, t := range tasks {
		results = append(results, BlockedResult{
			TaskID:    t.ID,
			TaskTitle: t.Title,
			PlanID:    t.PlanID,
			LastNote:  t.LastNote,
		})
	}
	if results == nil {
		results = []BlockedResult{}
	}
	return results
}

// runFullAnalysis runs all analysis checks and returns a report.
func runFullAnalysis() AnalyzeReport {
	report := AnalyzeReport{
		Drifts:     analyzeScopeDrift(),
		Orphans:    analyzeOrphanTasks(),
		Duplicates: analyzeDuplicatePlans(),
		Blocked:    analyzeBlockedTasks(),
		Conflicts:   analyzeConflicts(),
		Progress:    analyzeProgress(),
		Impacts:     analyzeDecisionImpact(),
		CrossTasks:  analyzeCrossTaskFiles(),
	}

	// Build summary
	parts := []string{}
	if len(report.Drifts) > 0 {
		parts = append(parts, plural(len(report.Drifts), "scope drift", "scope drifts"))
	}
	if len(report.Orphans) > 0 {
		parts = append(parts, plural(len(report.Orphans), "orphan task", "orphan tasks"))
	}
	if len(report.Duplicates) > 0 {
		parts = append(parts, plural(len(report.Duplicates), "duplicate", "duplicates"))
	}
	if len(report.Blocked) > 0 {
		parts = append(parts, plural(len(report.Blocked), "blocked task", "blocked tasks"))
	}
	if len(report.Conflicts) > 0 {
		parts = append(parts, plural(len(report.Conflicts), "conflict", "conflicts"))
	}
	atRisk := 0
	for _, p := range report.Progress {
		if p.RiskLevel == "at_risk" || p.RiskLevel == "off_track" {
			atRisk++
		}
	}
	if atRisk > 0 {
		parts = append(parts, plural(atRisk, "at-risk plan", "at-risk plans"))
	}
	if len(report.CrossTasks) > 0 {
		parts = append(parts, plural(len(report.CrossTasks), "cross-task link", "cross-task links"))
	}
	if len(report.Impacts) > 0 {
		parts = append(parts, plural(len(report.Impacts), "decision impact", "decision impacts"))
	}

	if len(parts) == 0 {
		report.Summary = "All clear — no issues detected."
	} else {
		report.Summary = "Found " + strings.Join(parts, ", ") + "."
	}

	return report
}

// analyzeConflicts detects tasks under the same plan with potentially conflicting approaches.
func analyzeConflicts() []ConflictResult {
	plans, err := listPlans("", "active")
	if err != nil {
		return nil
	}

	var results []ConflictResult
	for _, p := range plans {
		planID := str(p["id"])
		tasks, err := listTasks("", planID)
		if err != nil || len(tasks) < 2 {
			continue
		}

		// Compare task phases — tasks in different phases under same plan may conflict
		phases := make(map[string][]Task)
		for _, t := range tasks {
			phases[t.Phase] = append(phases[t.Phase], t)
		}

		// If tasks span very different phases without clear ordering, flag it
		if len(phases) > 3 {
			var taskInfos []string
			for _, tl := range phases {
				for _, t := range tl {
					taskInfos = append(taskInfos, t.Title)
				}
			}
			if len(taskInfos) >= 2 {
				results = append(results, ConflictResult{
					TaskID1: tasks[0].ID,
					Title1:  tasks[0].Title,
					TaskID2: tasks[1].ID,
					Title2:  tasks[1].Title,
					PlanID:  planID,
					Reason:  "Multiple phases under one plan — tasks may have conflicting priorities",
				})
			}
		}
	}
	if results == nil {
		results = []ConflictResult{}
	}
	return results
}

// analyzeProgress checks plan completion rate against elapsed time.
func analyzeProgress() []ProgressResult {
	plans, err := listPlans("", "active")
	if err != nil {
		return nil
	}

	var results []ProgressResult
	for _, p := range plans {
		planID := str(p["id"])
		planTitle := str(p["title"])

		tasks, err := listTasks("", planID)
		if err != nil || len(tasks) == 0 {
			continue
		}

		total := len(tasks)
		done := 0
		for _, t := range tasks {
			if t.Status == "done" {
				done++
			}
		}

		pct := 0
		if total > 0 {
			pct = (done * 100) / total
		}

		risk := "on_track"
		if pct < 20 && total > 3 {
			risk = "off_track"
		} else if pct < 50 {
			risk = "at_risk"
		}

		results = append(results, ProgressResult{
			PlanID:      planID,
			PlanTitle:   planTitle,
			TotalTasks:  total,
			DoneTasks:   done,
			ProgressPct: pct,
			RiskLevel:   risk,
		})
	}
	if results == nil {
		results = []ProgressResult{}
	}
	return results
}

// analyzeCrossTaskFiles detects commits whose changed files overlap with other active tasks.
func analyzeCrossTaskFiles() []CrossTaskResult {
	commits, err := listCommits("", "", "", "", 50)
	if err != nil {
		return nil
	}

	// Build a map: file path → list of tasks that have modified it
	fileTaskMap := make(map[string]map[string]bool)
	for _, c := range commits {
		taskID := str(c["task_id"])
		if taskID == "" {
			continue
		}
		files, _ := c["files"].([]any)
		for _, f := range files {
			fname, ok := f.(string)
			if !ok {
				continue
			}
			if fileTaskMap[fname] == nil {
				fileTaskMap[fname] = make(map[string]bool)
			}
			fileTaskMap[fname][taskID] = true
		}
	}

	// Find files touched by multiple different tasks
	taskPairs := make(map[string]CrossTaskResult)
	for fname, tasks := range fileTaskMap {
		if len(tasks) < 2 {
			continue
		}
		taskList := make([]string, 0, len(tasks))
		for tid := range tasks {
			taskList = append(taskList, tid)
		}
		for i := 0; i < len(taskList); i++ {
			for j := i + 1; j < len(taskList); j++ {
				key := taskList[i] + "|" + taskList[j]
				if _, exists := taskPairs[key]; !exists {
					taskPairs[key] = CrossTaskResult{
						TaskID:      taskList[i],
						OtherTaskID: taskList[j],
						SharedFiles: []string{fname},
					}
				} else {
					r := taskPairs[key]
					r.SharedFiles = append(r.SharedFiles, fname)
					taskPairs[key] = r
				}
			}
		}
	}

	// Enrich with task titles
	var results []CrossTaskResult
	for _, r := range taskPairs {
		t1, _ := getTaskSimple(r.TaskID)
		t2, _ := getTaskSimple(r.OtherTaskID)
		if t1 != nil {
			r.TaskTitle = str(t1["title"])
		}
		if t2 != nil {
			r.OtherTitle = str(t2["title"])
		}
		results = append(results, r)
	}
	if results == nil {
		results = []CrossTaskResult{}
	}
	return results
}

// analyzeDecisionImpact finds tasks linked to recently changed decisions.
func analyzeDecisionImpact() []ImpactResult {
	decisions, err := listDecisions()
	if err != nil {
		return nil
	}

	var results []ImpactResult
	for _, d := range decisions {
		decisionID := str(d["id"])
		decisionTitle := str(d["title"])
		status := str(d["status"])

		// Only check recently proposed/changed decisions
		if status != "proposed" && status != "recently_accepted" {
			continue
		}

		// Find links from this decision to tasks
		links, err := listLinks(decisionID, "", "")
		if err != nil {
			continue
		}

		var affectedTasks []string
		var affectedPlans []string
		for _, l := range links {
			if str(l["target_type"]) == "task" {
				affectedTasks = append(affectedTasks, str(l["target_id"]))
			}
			if str(l["target_type"]) == "plan" {
				affectedPlans = append(affectedPlans, str(l["target_id"]))
			}
		}

		// Also check for tasks that reference this decision
		if relTasks, ok := d["related_tasks"].([]any); ok {
			for _, t := range relTasks {
				if s, ok := t.(string); ok {
					affectedTasks = append(affectedTasks, s)
				}
			}
		}

		if len(affectedTasks) > 0 || len(affectedPlans) > 0 {
			results = append(results, ImpactResult{
				DecisionID:    decisionID,
				DecisionTitle: decisionTitle,
				AffectedPlans: affectedPlans,
				AffectedTasks: affectedTasks,
			})
		}
	}
	if results == nil {
		results = []ImpactResult{}
	}
	return results
}

// titleSimilarity computes a simple word-overlap similarity between two titles.
func titleSimilarity(a, b string) float64 {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	if a == b {
		return 1.0
	}

	wordsA := strings.Fields(a)
	wordsB := strings.Fields(b)

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}

	intersection := 0
	for _, w := range wordsB {
		if setA[w] {
			intersection++
		}
	}

	// Jaccard-like: intersection / min(lenA, lenB)
	minLen := len(wordsA)
	if len(wordsB) < minLen {
		minLen = len(wordsB)
	}
	if minLen == 0 {
		return 0
	}
	return float64(intersection) / float64(minLen)
}

// ThreadSuggestResult is a suggested thread from commit analysis.
type ThreadSuggestResult struct {
	SuggestedTitle string          `json:"suggested_title"`
	Rationale      string          `json:"rationale"`
	SourceEntities []ThreadItem    `json:"source_entities"`
	Score          float64         `json:"score"`
}

// ThreadStatusResult shows the status of existing threads.
type ThreadStatusResult struct {
	ThreadID     string `json:"thread_id"`
	ThreadTitle  string `json:"thread_title"`
	Status       string `json:"status"`
	DaysSinceLastActivity int `json:"days_since_last_activity"`
	ItemCount    int    `json:"item_count"`
	Paused       bool   `json:"paused"`
}

// analyzeThreadSuggestions clusters recent commits to suggest threads.
func analyzeThreadSuggestions() []ThreadSuggestResult {
	commits, err := listCommits("", "", "", "", 50)
	if err != nil {
		return nil
	}

	// Collect commit chains: group by task_id and file patterns
	type clusterInfo struct {
		commits   []map[string]any
		filePaths map[string]bool
		taskIDs   map[string]bool
		planIDs   map[string]bool
	}
	cluster := &clusterInfo{
		filePaths: make(map[string]bool),
		taskIDs:   make(map[string]bool),
		planIDs:   make(map[string]bool),
	}

	for _, c := range commits {
		if str(c["id"]) == "" {
			continue
		}
		cluster.commits = append(cluster.commits, c)
		if tid := str(c["task_id"]); tid != "" {
			cluster.taskIDs[tid] = true
		}
		if files, ok := c["files"].([]any); ok {
			for _, f := range files {
				if fn, ok := f.(string); ok {
					cluster.filePaths[fn] = true
				}
			}
		}
	}

	// Enrich: for each task_id, find its plan
	for tid := range cluster.taskIDs {
		task, err := getTaskSimple(tid)
		if err != nil {
			continue
		}
		if pid := str(task["plan_id"]); pid != "" {
			cluster.planIDs[pid] = true
		}
	}

	// Extract top-level directory patterns from file paths
	dirCounts := make(map[string]int)
	for fp := range cluster.filePaths {
		parts := strings.Split(fp, "/")
		if len(parts) > 0 {
			dir := strings.ToLower(parts[0])
			if dir != "" && dir != "." {
				dirCounts[dir]++
			}
		}
	}

	if len(cluster.commits) == 0 {
		return nil
	}

	var results []ThreadSuggestResult

	// Suggest threads based on plan groupings
	for pid := range cluster.planIDs {
		plan, err := getPlan(pid)
		if err != nil || plan == nil {
			continue
		}

		var relatedCommits []map[string]any
		for _, c := range cluster.commits {
			tid := str(c["task_id"])
			if tid == "" {
				continue
			}
			task, err := getTaskSimple(tid)
			if err != nil {
				continue
			}
			if str(task["plan_id"]) == pid {
				relatedCommits = append(relatedCommits, c)
			}
		}

		if len(relatedCommits) == 0 {
			continue
		}

		entities := []ThreadItem{}
		for _, c := range relatedCommits[:min(5, len(relatedCommits))] {
			entities = append(entities, ThreadItem{
				EntityType: "commit",
				EntityID:   str(c["id"]),
				Title:      str(c["title"]),
				Status:     str(c["status"]),
			})
		}

		results = append(results, ThreadSuggestResult{
			SuggestedTitle: str(plan["title"]),
			Rationale:      fmt.Sprintf("%d commits linked to plan %s", len(relatedCommits), str(plan["title"])),
			SourceEntities: entities,
			Score:          float64(len(relatedCommits)) / float64(len(cluster.commits)),
		})
	}

	// Suggest threads based on directory clusters (for unlinked commits)
	suggestedDirs := []string{}
	for dir, cnt := range dirCounts {
		if cnt >= 2 {
			suggestedDirs = append(suggestedDirs, dir)
		}
	}
	if len(suggestedDirs) > 0 {
		entities := []ThreadItem{}
		count := 0
		for _, c := range cluster.commits {
			if count >= 5 {
				break
			}
			entities = append(entities, ThreadItem{
				EntityType: "commit",
				EntityID:   str(c["id"]),
				Title:      str(c["title"]),
				Status:     str(c["status"]),
			})
			count++
		}
		results = append(results, ThreadSuggestResult{
			SuggestedTitle: fmt.Sprintf("Work in %s", strings.Join(suggestedDirs, ", ")),
			Rationale:      fmt.Sprintf("Commits spread across directories: %s", strings.Join(suggestedDirs, ", ")),
			SourceEntities: entities,
			Score:          0.5,
		})
	}

	if results == nil {
		results = []ThreadSuggestResult{}
	}
	return results
}

// analyzeThreadStatus checks existing threads for activity gaps.
func analyzeThreadStatus() []ThreadStatusResult {
	threads, err := listThreads("active")
	if err != nil {
		return nil
	}

	var results []ThreadStatusResult
	for _, t := range threads {
		tid := str(t["id"])
		items, ok := t["items"].([]map[string]any)
		if !ok || len(items) == 0 {
			continue
		}
		// Find the most recent activity date from thread items
		latestAdded := ""
		for _, item := range items {
			if at := str(item["added_at"]); at > latestAdded {
				latestAdded = at
			}
		}
		tr := ThreadStatusResult{
			ThreadID:    tid,
			ThreadTitle: str(t["title"]),
			Status:      str(t["status"]),
			ItemCount:   len(items),
		}
		if latestAdded != "" {
			tr.DaysSinceLastActivity = daysSince(latestAdded)
			tr.Paused = tr.DaysSinceLastActivity > 7
		}
		results = append(results, tr)
	}
	if results == nil {
		results = []ThreadStatusResult{}
	}
	return results
}

func daysSince(dateStr string) int {
	t, err := time.Parse("2006-01-02T15:04:05", dateStr)
	if err != nil {
		t2, err2 := time.Parse("2006-01-02", dateStr)
		if err2 != nil {
			return 0
		}
		t = t2
	}
	return int(time.Since(t).Hours() / 24)
}

// BuildBriefing generates a structured Markdown briefing for the Agent.
func BuildBriefing() string {
	report := runFullAnalysis()
	tasks, _ := listTasks("in_progress", "")
	events, _ := getUnconsumedEvents()
	threadSummary := buildThreadSummary()

	var b strings.Builder
	b.WriteString("🏗️ 项目简报 — AIPM\n\n")

	// Thread summary first — this is the "story so far"
	if threadSummary != "" {
		b.WriteString(threadSummary)
		b.WriteString("\n")
	}

	// Current focus
	if len(tasks) > 0 {
		b.WriteString("## 当前进行中的任务\n")
		for _, t := range tasks[:min(3, len(tasks))] {
			b.WriteString(fmt.Sprintf("- **%s** [%s] _%s_\n", t.Title, t.ID, t.Status))
		}
		b.WriteString("\n")
	}

	// Thread suggestions
	suggestions := analyzeThreadSuggestions()
	if len(suggestions) > 0 {
		b.WriteString("## 🧵 建议的线索 (从最近 commit 推断)\n")
		b.WriteString("以下线索是 AI 从 commit 历史中自动识别的。用 `aipmc thread add` 确认：\n\n")
		for _, s := range suggestions {
			b.WriteString(fmt.Sprintf("- **%s** — %s\n", s.SuggestedTitle, s.Rationale))
		}
		b.WriteString("\n")
	}

	// Paused threads
	threadStatus := analyzeThreadStatus()
	pausedCount := 0
	for _, ts := range threadStatus {
		if ts.Paused {
			if pausedCount == 0 {
				b.WriteString("## ⏸️ 暂停的线索 (超过 7 天无活动)\n")
			}
			b.WriteString(fmt.Sprintf("- **%s** (%d 天无活动, %d items)\n", ts.ThreadTitle, ts.DaysSinceLastActivity, ts.ItemCount))
			pausedCount++
		}
	}
	if pausedCount > 0 {
		b.WriteString("\n")
	}

	// PM alerts
	if len(events) > 0 {
		b.WriteString("## ⚠️ PM 最新变更\n")
		for _, e := range events {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", e["type"], e["summary"]))
		}
		b.WriteString("\n")
	}

	// Risks
	if len(report.Orphans) > 0 {
		b.WriteString("## 🔍 孤儿任务 (in_progress 但无 commit)\n")
		for _, o := range report.Orphans {
			b.WriteString(fmt.Sprintf("- **%s** [%s]\n", o.TaskTitle, o.TaskID))
		}
		b.WriteString("\n")
	}

	atRiskCount := 0
	for _, p := range report.Progress {
		if p.RiskLevel == "at_risk" || p.RiskLevel == "off_track" {
			if atRiskCount == 0 {
				b.WriteString("## 📊 进度风险\n")
			}
			b.WriteString(fmt.Sprintf("- **%s**: %d%% 完成 (%d/%d tasks) — %s\n",
				p.PlanTitle, p.ProgressPct, p.DoneTasks, p.TotalTasks, p.RiskLevel))
			atRiskCount++
		}
	}
	if atRiskCount > 0 {
		b.WriteString("\n")
	}

	if len(report.Duplicates) > 0 {
		b.WriteString("## ⚠️ 检测到重复\n")
		for _, d := range report.Duplicates {
			b.WriteString(fmt.Sprintf("- **%s** 与 **%s** 相似度 %.0f%%\n", d.Title1, d.Title2, d.Similarity*100))
		}
		b.WriteString("\n")
	}

	if len(report.Drifts) > 0 {
		b.WriteString("## 🔗 Scope 漂移\n")
		for _, d := range report.Drifts {
			b.WriteString(fmt.Sprintf("- Commit **%s**: 文件 %v 超出 plan scope\n", d.CommitTitle, d.OutOfScope))
		}
		b.WriteString("\n")
	}

	if report.Summary == "All clear — no issues detected." && threadSummary == "" && len(suggestions) == 0 && pausedCount == 0 {
		b.WriteString("✅ 一切正常，无问题检测。\n")
	}

	return b.String()
}

func buildThreadSummary() string {
	threads, err := listThreads("active")
	if err != nil || len(threads) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 🧵 当前活跃线索\n")
	for _, t := range threads[:min(3, len(threads))] {
		items, _ := t["items"].([]map[string]any)
		itemCount := 0
		if items != nil {
			itemCount = len(items)
		}
		b.WriteString(fmt.Sprintf("- **%s** _(%d items, since %s)_\n", str(t["title"]), itemCount, str(t["created_at"])))
	}
	return b.String()
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return itoa(n) + " " + plural
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}
