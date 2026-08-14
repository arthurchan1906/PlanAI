package analyze

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"aipmc/ai"
	"aipmc/session"
	"aipmc/store"
	"aipmc/u"
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

// scopeDriftCommitLimit bounds how many recent commits AnalyzeScopeDrift
// examines. The full history can be thousands of commits; drift checking is a
// briefing aid, not an audit, and unbounded scans previously ballooned the
// briefing (EncryptDrive: 734 entries, ~180KB).
const scopeDriftCommitLimit = 50

// minOutOfScopeRatio controls false positives from scope-keyword mismatch:
// a commit is flagged only when a strict majority of its files fall outside
// the plan scope.
const minOutOfScopeRatio = 0.5

// Briefing rendering caps. A briefing is injected into every agent's context;
// unbounded lists made it unusable (EncryptDrive was ~180KB from scope drift
// alone). Cap items shown and summarize the remainder.
const (
	briefingDriftCap = 15
	briefingFileCap  = 5
	briefingListCap  = 15
)

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
func AnalyzeScopeDrift() []DriftResult {
	commits, err := store.ListCommits("", "", "", "", scopeDriftCommitLimit)
	if err != nil {
		return nil
	}

	var results []DriftResult
	for _, c := range commits {
		taskID := u.Str(c["task_id"])
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
		task, err := store.GetTaskSimple(taskID)
		if err != nil {
			continue
		}
		planID := u.Str(task["plan_id"])
		if planID == "" {
			continue
		}

		// Get the plan to check its scope
		plan, err := store.GetPlan(planID)
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

		// Only a strict majority of changed files out of scope counts as
		// drift — one unmatched path is usually a scope-keyword mismatch.
		if len(outOfScope) > 0 && float64(len(outOfScope))/float64(len(filesList)) > minOutOfScopeRatio {
			changedFiles := make([]string, 0, len(filesList))
			for _, f := range filesList {
				if s, ok := f.(string); ok {
					changedFiles = append(changedFiles, s)
				}
			}
			results = append(results, DriftResult{
				CommitID:     u.Str(c["id"]),
				CommitTitle:  u.Str(c["title"]),
				TaskID:       taskID,
				TaskTitle:    u.Str(task["title"]),
				PlanID:       planID,
				PlanTitle:    u.Str(plan["title"]),
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

// analyzeOrphanTasks finds in_progress tasks with no commits and no recent discussion activity.
func AnalyzeOrphanTasks() []OrphanResult {
	tasks, err := store.ListTasks("in_progress", "")
	if err != nil {
		return nil
	}
	discussionCutoff := time.Now().Add(-3 * 24 * time.Hour).Format("2006-01-02T15:04:05")

	var results []OrphanResult
	for _, t := range tasks {
		commits, err := store.ListCommitsByTask(t.ID)
		if err != nil {
			continue
		}
		if len(commits) > 0 {
			continue
		}
		if store.HasRecentDiscussionLink("task", t.ID, discussionCutoff) {
			continue
		}
		results = append(results, OrphanResult{
			TaskID:    t.ID,
			TaskTitle: t.Title,
			PlanID:    t.PlanID,
			Status:    t.Status,
		})
	}
	if results == nil {
		results = []OrphanResult{}
	}
	return results
}

// analyzeDuplicatePlans finds plans with similar titles that may be duplicates.
func AnalyzeDuplicatePlans() []DuplicateResult {
	plans, err := store.ListPlans("", "")
	if err != nil {
		return nil
	}

	// Filter out inactive plans for duplicate detection
	var activePlans []map[string]any
	for _, p := range plans {
		s := u.Str(p["status"])
		if s != "cancelled" && s != "archived" {
			activePlans = append(activePlans, p)
		}
	}

	var results []DuplicateResult
	for i := 0; i < len(activePlans); i++ {
		for j := i + 1; j < len(activePlans); j++ {
			title1 := u.Str(activePlans[i]["title"])
			title2 := u.Str(activePlans[j]["title"])
			sim := TitleSimilarity(title1, title2)
			if sim > 0.7 {
				results = append(results, DuplicateResult{
					EntityType: "plan",
					ID1:        u.Str(activePlans[i]["id"]),
					Title1:     title1,
					ID2:        u.Str(activePlans[j]["id"]),
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
func AnalyzeBlockedTasks() []BlockedResult {
	tasks, err := store.ListTasks("blocked", "")
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
func RunFullAnalysis() AnalyzeReport {
	report := AnalyzeReport{
		Drifts:     AnalyzeScopeDrift(),
		Orphans:    AnalyzeOrphanTasks(),
		Duplicates: AnalyzeDuplicatePlans(),
		Blocked:    AnalyzeBlockedTasks(),
		Conflicts:   AnalyzeConflicts(),
		Progress:    AnalyzeProgress(),
		Impacts:     AnalyzeDecisionImpact(),
		CrossTasks:  AnalyzeCrossTaskFiles(),
	}

	// Build summary
	parts := []string{}
	if len(report.Drifts) > 0 {
		parts = append(parts, Plural(len(report.Drifts), "scope drift", "scope drifts"))
	}
	if len(report.Orphans) > 0 {
		parts = append(parts, Plural(len(report.Orphans), "orphan task", "orphan tasks"))
	}
	if len(report.Duplicates) > 0 {
		parts = append(parts, Plural(len(report.Duplicates), "duplicate", "duplicates"))
	}
	if len(report.Blocked) > 0 {
		parts = append(parts, Plural(len(report.Blocked), "blocked task", "blocked tasks"))
	}
	if len(report.Conflicts) > 0 {
		parts = append(parts, Plural(len(report.Conflicts), "conflict", "conflicts"))
	}
	atRisk := 0
	for _, p := range report.Progress {
		if p.RiskLevel == "at_risk" || p.RiskLevel == "off_track" {
			atRisk++
		}
	}
	if atRisk > 0 {
		parts = append(parts, Plural(atRisk, "at-risk plan", "at-risk plans"))
	}
	if len(report.CrossTasks) > 0 {
		parts = append(parts, Plural(len(report.CrossTasks), "cross-task link", "cross-task links"))
	}
	if len(report.Impacts) > 0 {
		parts = append(parts, Plural(len(report.Impacts), "decision impact", "decision impacts"))
	}

	if len(parts) == 0 {
		report.Summary = "All clear — no issues detected."
	} else {
		report.Summary = "Found " + strings.Join(parts, ", ") + "."
	}

	return report
}

// analyzeConflicts detects tasks under the same plan with potentially conflicting approaches.
func AnalyzeConflicts() []ConflictResult {
	plans, err := store.ListPlans("", "active")
	if err != nil {
		return nil
	}

	var results []ConflictResult
	for _, p := range plans {
		planID := u.Str(p["id"])
		tasks, err := store.ListTasks("", planID)
		if err != nil || len(tasks) < 2 {
			continue
		}

		// Compare task phases — tasks in different phases under same plan may conflict
		phases := make(map[string][]store.Task)
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
func AnalyzeProgress() []ProgressResult {
	plans, err := store.ListPlans("", "active")
	if err != nil {
		return nil
	}

	var results []ProgressResult
	for _, p := range plans {
		planID := u.Str(p["id"])
		planTitle := u.Str(p["title"])

		tasks, err := store.ListTasks("", planID)
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
func AnalyzeCrossTaskFiles() []CrossTaskResult {
	commits, err := store.ListCommits("", "", "", "", 50)
	if err != nil {
		return nil
	}

	// Build a map: file path → list of tasks that have modified it
	fileTaskMap := make(map[string]map[string]bool)
	for _, c := range commits {
		taskID := u.Str(c["task_id"])
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
		t1, _ := store.GetTaskSimple(r.TaskID)
		t2, _ := store.GetTaskSimple(r.OtherTaskID)
		if t1 != nil {
			r.TaskTitle = u.Str(t1["title"])
		}
		if t2 != nil {
			r.OtherTitle = u.Str(t2["title"])
		}
		results = append(results, r)
	}
	if results == nil {
		results = []CrossTaskResult{}
	}
	return results
}

// analyzeDecisionImpact finds tasks linked to recently changed decisions.
func AnalyzeDecisionImpact() []ImpactResult {
	decisions, err := store.ListDecisions()
	if err != nil {
		return nil
	}

	var results []ImpactResult
	for _, d := range decisions {
		decisionID := u.Str(d["id"])
		decisionTitle := u.Str(d["title"])
		status := u.Str(d["status"])

		// Only check recently proposed/changed decisions
		if status != "proposed" && status != "recently_accepted" {
			continue
		}

		// Find links from this decision to tasks
		links, err := store.ListLinks(decisionID, "", "")
		if err != nil {
			continue
		}

		var affectedTasks []string
		var affectedPlans []string
		for _, l := range links {
			if u.Str(l["target_type"]) == "task" {
				affectedTasks = append(affectedTasks, u.Str(l["target_id"]))
			}
			if u.Str(l["target_type"]) == "plan" {
				affectedPlans = append(affectedPlans, u.Str(l["target_id"]))
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
func TitleSimilarity(a, b string) float64 {
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
	SourceEntities []store.ThreadItem    `json:"source_entities"`
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

// analyzeThreadSuggestions uses multi-dimensional similarity to cluster
// related commits and suggest threads. It considers title keyword overlap,
// file path affinity, plan membership, and time proximity — so work that
// spans multiple plans or evolves organically can still be recognized.
func AnalyzeThreadSuggestions() []ThreadSuggestResult {
	commits, err := store.ListCommits("", "", "", "", 100)
	if err != nil || len(commits) < 2 {
		return nil
	}

	items, planTitles := ParseCommitItems(commits)
	if len(items) < 2 {
		return nil
	}

	clusters := ClusterBySimilarity(items, 0.2)

	var results []ThreadSuggestResult
	for _, cl := range clusters {
		if len(cl) < 2 {
			continue
		}
		sug := BuildThreadSuggestion(items, cl, planTitles, len(items))
		results = append(results, sug)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	if results == nil {
		results = []ThreadSuggestResult{}
	}
	return results
}

// commitItem holds enriched commit data for similarity comparison.
type commitItem struct {
	id        string
	title     string
	files     []string
	taskID    string
	planID    string
	ts        time.Time
	keywords  []string
}

// stopWords filters out common non-semantic words during keyword extraction.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true, "can": true,
	"could": true, "should": true, "may": true, "might": true, "must": true,
	"to": true, "of": true, "in": true, "for": true, "on": true, "with": true,
	"at": true, "by": true, "from": true, "as": true, "into": true, "through": true,
	"and": true, "or": true, "nor": true, "not": true, "no": true, "but": true,
	"this": true, "that": true, "it": true, "its": true, "so": true, "yet": true,
	"all": true, "any": true, "few": true, "more": true, "most": true, "some": true,
	"each": true, "both": true, "just": true, "only": true, "very": true, "too": true,
	"also": true, "now": true, "then": true, "here": true, "there": true,
	"add": true, "fix": true, "update": true, "remove": true, "delete": true,
	"refactor": true, "implement": true, "use": true, "make": true, "get": true,
	"set": true, "change": true, "test": true, "wip": true, "clean": true,
	"support": true, "handle": true, "improve": true, "allow": true, "enable": true,
}

// parseCommitItems converts raw commit maps into enriched commitItem structs,
// resolving task → plan relationships via a local cache.
func ParseCommitItems(commits []map[string]any) ([]commitItem, map[string]string) {
	type taskMeta struct{ planID string }
	taskCache := map[string]*taskMeta{}
	planTitles := map[string]string{}

	var items []commitItem
	for _, c := range commits {
		ci := commitItem{
			id:     u.Str(c["id"]),
			title:  u.Str(c["title"]),
			taskID: u.Str(c["task_id"]),
		}
		if t, err := time.Parse("2006-01-02T15:04:05", u.Str(c["created_at"])); err == nil {
			ci.ts = t
		}
		if rawFiles, ok := c["files"].([]any); ok {
			for _, f := range rawFiles {
				if s, ok := f.(string); ok {
					ci.files = append(ci.files, s)
				}
			}
		}
		// Resolve task → plan
		if ci.taskID != "" {
			if tc, ok := taskCache[ci.taskID]; ok {
				ci.planID = tc.planID
			} else {
				if t, err := store.GetTaskSimple(ci.taskID); err == nil {
					taskCache[ci.taskID] = &taskMeta{u.Str(t["plan_id"])}
					ci.planID = u.Str(t["plan_id"])
				}
			}
		}
		// Cache plan title
		if ci.planID != "" {
			if _, ok := planTitles[ci.planID]; !ok {
				if p, err := store.GetPlan(ci.planID); err == nil && p != nil {
					planTitles[ci.planID] = u.Str(p["title"])
				}
			}
		}
		ci.keywords = ExtractKeywords(ci.title)
		items = append(items, ci)
	}
	return items, planTitles
}

// extractKeywords splits a title into meaningful lowercase tokens, filtering
// out stop words and single characters.
func ExtractKeywords(title string) []string {
	title = strings.ToLower(title)
	// Split on common delimiters including CJK-unfriendly ones
	words := strings.FieldsFunc(title, func(r rune) bool {
		return r == ' ' || r == ':' || r == ',' || r == ';' || r == '-' || r == '_' ||
			r == '.' || r == '/' || r == '(' || r == ')' || r == '[' || r == ']' ||
			r == '→' || r == '{' || r == '}' || r == '"' || r == '\''
	})
	var result []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len(w) < 2 || stopWords[w] {
			continue
		}
		result = append(result, w)
	}
	return result
}

// commitPairSim computes a weighted multi-dimensional similarity between two commits.
// Weights: title keywords 0.30, file overlap 0.35, same plan 0.15, time proximity 0.20.
func CommitPairSim(a, b commitItem) float64 {
	const wtTitle, wtFiles, wtPlan, wtTime = 0.30, 0.35, 0.15, 0.20
	var score float64

	// 1. Title keyword overlap (Jaccard)
	if len(a.keywords) > 0 && len(b.keywords) > 0 {
		score += wtTitle * JaccardStrings(a.keywords, b.keywords)
	}

	// 2. File path overlap (Jaccard) — strongest signal for related work
	if len(a.files) > 0 && len(b.files) > 0 {
		score += wtFiles * JaccardStrings(a.files, b.files)
	}

	// 3. Same plan membership — soft signal (work can span multiple plans)
	if a.planID != "" && b.planID != "" && a.planID == b.planID {
		score += wtPlan
	}

	// 4. Time proximity — decays linearly over 72 hours
	if !a.ts.IsZero() && !b.ts.IsZero() {
		hours := b.ts.Sub(a.ts).Hours()
		if hours < 0 {
			hours = -hours
		}
		if hours < 72 {
			score += wtTime * (1.0 - hours/72.0)
		}
	}

	return score
}

// jaccardStrings computes Jaccard similarity: |A ∩ B| / |A ∪ B|.
func JaccardStrings(a, b []string) float64 {
	setA := make(map[string]bool, len(a))
	for _, s := range a {
		setA[s] = true
	}
	intersection := 0
	union := make(map[string]bool, len(a)+len(b))
	for _, s := range a {
		union[s] = true
	}
	for _, s := range b {
		union[s] = true
		if setA[s] {
			intersection++
		}
	}
	if len(union) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(union))
}

// clusterBySimilarity groups commits into clusters using union-find on the
// similarity graph. Two commits are connected if their similarity ≥ threshold.
func ClusterBySimilarity(items []commitItem, threshold float64) [][]int {
	n := len(items)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y int) {
		parent[find(x)] = find(y)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if CommitPairSim(items[i], items[j]) >= threshold {
				union(i, j)
			}
		}
	}

	// Collect clusters
	groups := map[int][]int{}
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	var clusters [][]int
	for _, g := range groups {
		clusters = append(clusters, g)
	}
	return clusters
}

// buildThreadSuggestion generates a ThreadSuggestResult from a cluster of commits.
func BuildThreadSuggestion(items []commitItem, indices []int, planTitles map[string]string, total int) ThreadSuggestResult {
	// Collect cluster stats
	planSet := map[string]bool{}
	fileCounts := map[string]int{}
	keywordCounts := map[string]int{}
	var clusterFiles, clusterKeywords []string
	var firstTime, lastTime time.Time

	for _, idx := range indices {
		ci := items[idx]
		if ci.planID != "" {
			planSet[ci.planID] = true
		}
		for _, f := range ci.files {
			fileCounts[f]++
			clusterFiles = append(clusterFiles, f)
		}
		for _, kw := range ci.keywords {
			keywordCounts[kw]++
			clusterKeywords = append(clusterKeywords, kw)
		}
		if !ci.ts.IsZero() {
			if firstTime.IsZero() || ci.ts.Before(firstTime) {
				firstTime = ci.ts
			}
			if lastTime.IsZero() || ci.ts.After(lastTime) {
				lastTime = ci.ts
			}
		}
	}

	// Generate title
	title := GenerateThreadTitle(items, indices, planSet, planTitles, fileCounts, keywordCounts)

	// Generate rationale
	rationale := GenerateThreadRationale(len(indices), planSet, planTitles, fileCounts, keywordCounts, firstTime, lastTime)

	// Score: cluster size / total * cross-plan bonus * file-concentration bonus
	sizeScore := float64(len(indices)) / float64(total)
	crossPlanBonus := 1.0
	if len(planSet) >= 2 {
		crossPlanBonus = 1.0 + 0.15*float64(len(planSet)-1)
	}
	// File concentration: higher = more focused work
	fileConcentration := 0.5
	if len(clusterFiles) > 0 {
		uniqueFiles := len(fileCounts)
		fileConcentration = float64(uniqueFiles) / float64(len(clusterFiles))
	}

	score := sizeScore * crossPlanBonus * (fileConcentration + 0.5) * 2.5
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.1 {
		score = 0.1
	}

	// Build source entities (top 8)
	entities := []store.ThreadItem{}
	for i, idx := range indices {
		if i >= 8 {
			break
		}
		ci := items[idx]
		entities = append(entities, store.ThreadItem{
			EntityType: "commit",
			EntityID:   ci.id,
			Title:      ci.title,
			Status:     "committed",
		})
	}

	return ThreadSuggestResult{
		SuggestedTitle: title,
		Rationale:      rationale,
		SourceEntities: entities,
		Score:          score,
	}
}

// generateThreadTitle produces a human-readable title for a cluster.
func GenerateThreadTitle(items []commitItem, indices []int, planSet map[string]bool, planTitles map[string]string, fileCounts map[string]int, keywordCounts map[string]int) string {
	// Strategy 1: If cluster is dominated by a single plan (>60% of commits), use plan title as base
	planVotes := map[string]int{}
	for _, idx := range indices {
		if items[idx].planID != "" {
			planVotes[items[idx].planID]++
		}
	}
	var dominantPlan string
	for pid, cnt := range planVotes {
		if float64(cnt)/float64(len(indices)) >= 0.6 {
			if dominantPlan == "" || cnt > planVotes[dominantPlan] {
				dominantPlan = pid
			}
		}
	}
	if dominantPlan != "" {
		title := planTitles[dominantPlan]
		if title == "" {
			title = dominantPlan
		}
		return title + " 相关工作"
	}

	// Strategy 2: Use the most common file path prefix (directory level)
	if len(fileCounts) > 0 {
		dirVotes := map[string]int{}
		for fp, cnt := range fileCounts {
			parts := strings.Split(fp, "/")
			// Try 2-level deep prefix for specificity
			var prefix string
			if len(parts) >= 2 {
				prefix = strings.Join(parts[:2], "/")
			} else if len(parts) == 1 && parts[0] != "" {
				prefix = parts[0]
			}
			if prefix != "" {
				dirVotes[prefix] += cnt
			}
		}
		var bestDir string
		var bestCnt int
		for d, c := range dirVotes {
			if c > bestCnt {
				bestCnt = c
				bestDir = d
			}
		}
		if bestDir != "" {
			return fmt.Sprintf("Work on %s", bestDir)
		}
	}

	// Strategy 3: Use top 2-3 keywords
	type kv struct{ k string; v int }
	var kvs []kv
	for k, v := range keywordCounts {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v })
	var topKws []string
	for i := 0; i < len(kvs) && i < 3; i++ {
		topKws = append(topKws, kvs[i].k)
	}
	if len(topKws) > 0 {
		return strings.Join(topKws, ", ") + " 相关变更"
	}

	// Fallback
	return fmt.Sprintf("最近 %d 条相关工作", len(indices))
}

// generateThreadRationale explains why this cluster forms a meaningful thread.
func GenerateThreadRationale(n int, planSet map[string]bool, planTitles map[string]string, fileCounts map[string]int, keywordCounts map[string]int, firstTime, lastTime time.Time) string {
	parts := []string{fmt.Sprintf("%d 条 commit", n)}

	// Plans
	if len(planSet) > 0 {
		var pnames []string
		for pid := range planSet {
			if t, ok := planTitles[pid]; ok && t != "" {
				pnames = append(pnames, t)
			}
		}
		if len(pnames) > 0 {
			if len(pnames) <= 2 {
				parts = append(parts, fmt.Sprintf("涉及计划: %s", strings.Join(pnames, ", ")))
			} else {
				parts = append(parts, fmt.Sprintf("跨 %d 个计划", len(pnames)))
			}
		}
	} else {
		parts = append(parts, "未关联计划")
	}

	// File paths: list top 2 directories
	if len(fileCounts) > 0 {
		type fkv struct{ f string; c int }
		var fkvs []fkv
		for f, c := range fileCounts {
			fkvs = append(fkvs, fkv{f, c})
		}
		sort.Slice(fkvs, func(i, j int) bool { return fkvs[i].c > fkvs[j].c })
		var topFiles []string
		for i := 0; i < len(fkvs) && i < 2; i++ {
			topFiles = append(topFiles, fkvs[i].f)
		}
		if len(topFiles) > 0 {
			parts = append(parts, fmt.Sprintf("主要文件: %s", strings.Join(topFiles, ", ")))
		}
	}

	// Time span
	if !firstTime.IsZero() && !lastTime.IsZero() {
		span := lastTime.Sub(firstTime)
		if span.Hours() >= 24 {
			parts = append(parts, fmt.Sprintf("历时 %d 天", int(span.Hours()/24)))
		} else if span.Hours() >= 1 {
			parts = append(parts, fmt.Sprintf("历时 %d 小时", int(span.Hours())))
		}
	}

	return strings.Join(parts, " · ")
}

// analyzeThreadStatus checks existing threads for activity gaps.
func AnalyzeThreadStatus() []ThreadStatusResult {
	threads, err := store.ListThreads("active")
	if err != nil {
		return nil
	}

	var results []ThreadStatusResult
	for _, t := range threads {
		tid := u.Str(t["id"])
		items, ok := t["items"].([]map[string]any)
		if !ok || len(items) == 0 {
			continue
		}
		// Find the most recent activity date from thread items
		latestAdded := ""
		for _, item := range items {
			if at := u.Str(item["added_at"]); at > latestAdded {
				latestAdded = at
			}
		}
		tr := ThreadStatusResult{
			ThreadID:    tid,
			ThreadTitle: u.Str(t["title"]),
			Status:      u.Str(t["status"]),
			ItemCount:   len(items),
		}
		if latestAdded != "" {
			tr.DaysSinceLastActivity = DaysSince(latestAdded)
			tr.Paused = tr.DaysSinceLastActivity > 7
		}
		results = append(results, tr)
	}
	if results == nil {
		results = []ThreadStatusResult{}
	}
	return results
}

func DaysSince(dateStr string) int {
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
// BuildBriefing 生成结构化 Markdown 简报；返回 (简报文本, 展示的 unconsumed 事件 ids)。
// 后者供 W2（8/13）事件→动作漏斗的 surfaced 记录——MCP get_briefing 返回时
// 由调用方 LogShared，与 hook 侧调用记录（session+ts）按 agent+时间窗对齐。
func BuildBriefing(aiClient *ai.Client, graphSection string) (string, []string) {
	report := RunFullAnalysis()
	tasks, _ := store.ListTasks("in_progress", "")
	events, _ := store.GetUnconsumedEvents()
	threadSummary := BuildThreadSummary()
	suggestions := AnalyzeThreadSuggestions()
	threadStatus := AnalyzeThreadStatus()
	var surfaced []string

	var b strings.Builder
	b.WriteString("🏗️ 项目简报 — AIPM\n\n")

	// Thread summary first — this is the "story so far"
	if threadSummary != "" {
		b.WriteString(threadSummary)
		b.WriteString("\n")
	}

	// ── Layer 1: ⚠️ 立即行动 ──
	hasImmediate := false
	if len(events) > 0 || len(report.Blocked) > 0 || len(report.Orphans) > 0 || len(report.Progress) > 0 {
		// Collect urgency flags from progress analysis
		hasOffTrack := false
		for _, p := range report.Progress {
			if p.RiskLevel == "off_track" {
				hasOffTrack = true
				break
			}
		}
		if len(events) > 0 || len(report.Blocked) > 0 || len(report.Orphans) > 0 || hasOffTrack {
			hasImmediate = true
			b.WriteString("## ⚠️ 立即行动\n\n")
		}

		if len(events) > 0 {
			b.WriteString("### PM 最新变更\n")
			for _, e := range events {
				b.WriteString(fmt.Sprintf("- [%s] %s\n", e["type"], e["summary"]))
				if id, ok := e["id"].(string); ok && id != "" {
					surfaced = append(surfaced, id)
				}
			}
			b.WriteString("  → 建议: 事件处理完毕用 aipm_mark_event_processed(entity_id=..., event_type=...) 标记已处理（计入 D2 已处理率）；仅浏览用 aipm_mark_consumed 标记已读\n\n")
		}

		if len(report.Blocked) > 0 {
			b.WriteString("### 阻塞任务\n")
			for _, bl := range report.Blocked {
				b.WriteString(fmt.Sprintf("- **%s** — 阻塞 %d 天\n", bl.TaskTitle, bl.DaysBlocked))
				b.WriteString(fmt.Sprintf("  → 建议: 检查阻塞原因，必要时联系 PM 做决策\n"))
			}
			b.WriteString("\n")
		}

		if len(report.Orphans) > 0 {
			b.WriteString("### 孤儿任务 (in_progress，3 天内无 commit 且无讨论)\n")
			for _, o := range report.Orphans {
				b.WriteString(fmt.Sprintf("- **%s** [%s]\n", o.TaskTitle, o.TaskID))
			}
			b.WriteString(fmt.Sprintf("  → 建议: 检查这些任务是否需要 commit 或更新状态\n\n"))
		}

		for _, p := range report.Progress {
			if p.RiskLevel == "off_track" {
				b.WriteString(fmt.Sprintf("### 严重偏离: **%s** (%d%% 完成, %d/%d tasks)\n",
					p.PlanTitle, p.ProgressPct, p.DoneTasks, p.TotalTasks))
				b.WriteString(fmt.Sprintf("  → 建议: 立即评估是否需要调整 scope 或追加资源\n\n"))
			}
		}
	}

	// ── Layer 2: 📋 应该知道 ──
	b.WriteString("## 📋 应该知道\n\n")

	if len(tasks) > 0 {
		b.WriteString("### 当前进行中的任务\n")
		activitySince := activityWindowSince()
		for _, t := range tasks[:min(5, len(tasks))] {
			b.WriteString(fmt.Sprintf("- **%s** [%s] _%s_\n", t.Title, t.ID, t.Status))
			sug := GetActionableSuggestion(t)
			if sug != "" {
				b.WriteString(fmt.Sprintf("  → %s\n", sug))
			}
			if store.HasRecentDiscussionLink("task", t.ID, activitySince) {
				b.WriteString("  → 💬 最近有讨论涉及此 task\n")
			}
		}
		b.WriteString("\n")
	}

	if len(report.Drifts) > 0 {
		b.WriteString("### Scope 漂移\n")
		shown := report.Drifts
		if len(shown) > briefingDriftCap {
			shown = shown[:briefingDriftCap]
		}
		for _, d := range shown {
			files := d.OutOfScope
			extra := ""
			if len(files) > briefingFileCap {
				extra = fmt.Sprintf(" 等 %d 个文件", len(files)-briefingFileCap)
				files = files[:briefingFileCap]
			}
			b.WriteString(fmt.Sprintf("- Commit **%s**: 文件 %v%s 超出 plan scope\n", d.CommitTitle, files, extra))
		}
		if len(report.Drifts) > briefingDriftCap {
			b.WriteString(fmt.Sprintf("  → 共 %d 条，已省略 %d 条\n", len(report.Drifts), len(report.Drifts)-briefingDriftCap))
		}
		b.WriteString(fmt.Sprintf("  → 建议: 确认这些文件是否应属于当前 task\n\n"))
	}

	if len(report.Duplicates) > 0 {
		b.WriteString("### 检测到重复\n")
		shown := report.Duplicates
		if len(shown) > briefingListCap {
			shown = shown[:briefingListCap]
		}
		for _, d := range shown {
			b.WriteString(fmt.Sprintf("- **%s** ≈ **%s** (%.0f%%)\n", d.Title1, d.Title2, d.Similarity*100))
		}
		if len(report.Duplicates) > briefingListCap {
			b.WriteString(fmt.Sprintf("  → 共 %d 条，已省略 %d 条\n", len(report.Duplicates), len(report.Duplicates)-briefingListCap))
		}
		b.WriteString(fmt.Sprintf("  → 建议: 检查是否为重复工作，避免并行开发冲突\n\n"))
	}

	atRiskCount := 0
	for _, p := range report.Progress {
		if p.RiskLevel == "at_risk" {
			if atRiskCount == 0 {
				b.WriteString("### 进度风险\n")
			}
			b.WriteString(fmt.Sprintf("- **%s**: %d%% 完成 (%d/%d tasks)\n",
				p.PlanTitle, p.ProgressPct, p.DoneTasks, p.TotalTasks))
			atRiskCount++
		}
	}
	if atRiskCount > 0 {
		b.WriteString(fmt.Sprintf("  → 建议: 检查剩余工作量和截止日期，必要时调整优先级\n\n"))
	}

	if len(report.Impacts) > 0 {
		b.WriteString(fmt.Sprintf("### %d 个决策影响待评估\n", len(report.Impacts)))
		shown := report.Impacts
		if len(shown) > briefingListCap {
			shown = shown[:briefingListCap]
		}
		for _, imp := range shown {
			b.WriteString(fmt.Sprintf("- Decision **%s** 影响 %d plans, %d tasks\n",
				imp.DecisionTitle, len(imp.AffectedPlans), len(imp.AffectedTasks)))
		}
		if len(report.Impacts) > briefingListCap {
			b.WriteString(fmt.Sprintf("  → 已省略 %d 条\n", len(report.Impacts)-briefingListCap))
		}
		b.WriteString(fmt.Sprintf("  → 建议: 检查受影响的 task 是否需要重新评估\n\n"))
	}

	if len(report.CrossTasks) > 0 {
		b.WriteString(fmt.Sprintf("### %d 个跨 task 文件关联\n", len(report.CrossTasks)))
		b.WriteString(fmt.Sprintf("  → 建议: 检查是否有潜在的合并冲突或逻辑依赖\n\n"))
	}

	if len(suggestions) > 0 {
		b.WriteString("### 🧵 建议的新线索\n")
		for _, s := range suggestions {
			b.WriteString(fmt.Sprintf("- **%s** — %s\n", s.SuggestedTitle, s.Rationale))
		}
		b.WriteString(fmt.Sprintf("  → 建议: 用 aipmc thread add 确认或创建\n\n"))
	}

	// ── Layer 3: 💡 参考 ──
	hasReference := false
	if len(threadStatus) > 0 {
		pausedCount := 0
		for _, ts := range threadStatus {
			if ts.Paused {
				pausedCount++
			}
		}
		if pausedCount > 0 {
			if !hasReference {
				b.WriteString("## 💡 参考\n\n")
				hasReference = true
			}
			b.WriteString("### 暂停的线索 (超过 7 天无活动)\n")
			for _, ts := range threadStatus {
				if ts.Paused {
					b.WriteString(fmt.Sprintf("- **%s** (%d 天无活动, %d items)\n", ts.ThreadTitle, ts.DaysSinceLastActivity, ts.ItemCount))
				}
			}
			b.WriteString("\n")
		}
	}

	if !hasImmediate && report.Summary == "All clear — no issues detected." && threadSummary == "" && len(suggestions) == 0 {
		b.WriteString("✅ 一切正常，无问题检测。\n")
	}

	// Recent agent activity since last briefing consume (fallback: 24h)
	activitySince := activityWindowSince()
	activityLabel := activityWindowLabel(activitySince)
	if sessions, err := store.RecentAgentActivity(activitySince, 8); err == nil && len(sessions) > 0 {
		_, _ = store.AutoLinkDiscussions(sessions)
		b.WriteString(fmt.Sprintf("## 📞 最近 Agent 活动 (%s)\n\n", activityLabel))
		seen := map[string]bool{}
		for _, s := range sessions {
			if !seen[s.Source] {
				seen[s.Source] = true
				ago := relativeTime(s.LastSeen)
				count := 0
				for _, x := range sessions {
					if x.Source == s.Source {
						count++
					}
				}
				b.WriteString(fmt.Sprintf("**%s** (%d sessions, latest: %s)\n", s.Source, count, ago))
			}
		}
		for _, s := range sessions {
			label := firstLine(s.UserPrompts, s.Source)
			date := dateShort(s.FirstSeen)
			b.WriteString(fmt.Sprintf("  • [%s] %s", shortSID(s.SessionID), label))
			parts := []string{}
			if date != "" {
				parts = append(parts, date)
			}
			if s.ToolCallCount > 0 {
				parts = append(parts, fmt.Sprintf("💬×%d 🔧×%d", s.UserPromptCount, s.ToolCallCount))
			}
			if len(parts) > 0 {
				b.WriteString(fmt.Sprintf(" (%s)", strings.Join(parts, ", ")))
			}
			if linked, err := store.LinkedEntityIDsForSession(s.SessionID); err == nil && len(linked) > 0 {
				b.WriteString(fmt.Sprintf("\n    涉及: %s", strings.Join(linked, ", ")))
			}
			b.WriteString("\n")
		}
		b.WriteString("用 aipm_list_sessions 可查看完整 session_id，再用 aipm_read_discussions(session_id=...) 精准读取某个会话。\n")
		b.WriteString("\n")
	}

	// L2 Session Knowledge (available when L2 summaries exist)
	if summaryRows, err := store.ListSessionSummariesWithSummary("", 20); err == nil && len(summaryRows) > 0 {
		b.WriteString("## 🧠 Session Knowledge\n\n")
		b.WriteString(fmt.Sprintf("Analyzed %d sessions with AI-generated summaries.\n\n", len(summaryRows)))

		// Show recent 3 session goals
		b.WriteString("### Recent Session Goals\n")
		shown := 0
		for _, sr := range summaryRows {
			if shown >= 3 {
				break
			}
			var l2 session.SessionL2Summary
			if json.Unmarshal([]byte(sr.Summary), &l2) == nil && l2.Goal != "" {
				b.WriteString(fmt.Sprintf("- [%s] %s\n", sessionIDPrefix(sr.SessionID), l2.Goal))
				shown++
			}
		}
		b.WriteString("\n")

		// Cross-session patterns
		knowledge := session.AggregateCrossSessionKnowledge(summaryRows)
		if len(knowledge.FilePatterns) > 0 {
			b.WriteString("### 高频文件\n")
			for i, fp := range knowledge.FilePatterns {
				if i >= 3 {
					break
				}
				b.WriteString(fmt.Sprintf("- **%s**: %d sessions\n", fp.FilePath, fp.SessionCount))
			}
			b.WriteString("\n")
		}
		if len(knowledge.RecurringLessons) > 0 {
			b.WriteString("### 经验教训\n")
			for i, lesson := range knowledge.RecurringLessons {
				if i >= 5 {
					break
				}
				b.WriteString(fmt.Sprintf("- %s\n", lesson))
			}
			b.WriteString("\n")
		}
	}


	// Graph section — injected before AI summary so it survives truncation
	if graphSection != "" {
		b.WriteString(graphSection)
	}
	// AI executive summary when available
	if aiClient != nil && aiClient.Enabled() {
		summary, err := aiClient.Summarize(b.String(),
			"Generate a 1-2 sentence executive summary in Chinese of this project briefing, focusing on what needs the most attention right now.")
		if err == nil && summary != "" {
			b.WriteString("\n---\n")
			b.WriteString("### 🤖 AI 简报摘要\n")
			b.WriteString(summary + "\n")
		}
	}

	return b.String(), surfaced
}

// getActionableSuggestion returns a context-aware suggestion for a task.
func GetActionableSuggestion(task store.Task) string {
	switch task.Status {
	case "blocked":
		return "确认: 此任务是否仍需阻塞？联系 PM 或检查 blocker 状态"
	case "in_progress":
		return "确认: 是否有未记录的 commit？推进到下一个检查点"
	case "todo":
		return "确认: 是否已准备好开始？检查依赖是否已满足"
	case "done":
		return ""
	}
	return ""
}

func BuildThreadSummary() string {
	threads, err := store.ListThreads("active")
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
		b.WriteString(fmt.Sprintf("- **%s** _(%d items, since %s)_\n", u.Str(t["title"]), itemCount, u.Str(t["created_at"])))
	}
	return b.String()
}

func Plural(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return Itoa(n) + " " + plural
}

func Itoa(n int) string {
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

// activityWindowSince prefers time since last aipm_mark_consumed, else last 24h.
func activityWindowSince() string {
	if ts := store.LastBriefingConsumedAt(); ts != "" {
		return ts
	}
	return since(24 * time.Hour)
}

func activityWindowLabel(sinceISO string) string {
	if ts := store.LastBriefingConsumedAt(); ts != "" && ts == sinceISO {
		return "自上次 mark_consumed"
	}
	return "24h"
}

// sessionIDPrefix returns a short display prefix for a session ID.
func sessionIDPrefix(sessionID string) string {
	if len(sessionID) <= 8 {
		return sessionID
	}
	return sessionID[:8]
}

// since returns an ISO timestamp for the given duration ago from now.
func since(d time.Duration) string {
	return time.Now().Add(-d).Format("2006-01-02T15:04:05")
}

// relativeTime returns a human-readable relative time label for an ISO timestamp.
func relativeTime(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05", iso)
	if err != nil {
		if len(iso) >= 16 {
			return iso[11:16]
		}
		return iso
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 分钟前"
		}
		return u.Itoa(m) + " 分钟前"
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 小时前"
		}
		return u.Itoa(h) + " 小时前"
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "昨天"
		}
		return u.Itoa(days) + " 天前"
	}
}

// dateShort returns a short date label (MM/DD) from an ISO timestamp.
func dateShort(iso string) string {
	if len(iso) < 10 {
		return ""
	}
	return iso[5:10] // "06/23"
}

// firstLine returns the first line of the first user prompt, or a fallback label.
func firstLine(prompts []string, fallback string) string {
	if len(prompts) == 0 {
		return fallback
	}
	content := prompts[len(prompts)-1] // oldest first in the slice
	if idx := strings.IndexByte(content, '\n'); idx > 0 {
		content = content[:idx]
	}
	runes := []rune(content)
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return content
}

// shortSID renders a compact session handle for briefing output so agents can
// map an activity bullet to a session and read it precisely via
// aipm_read_discussions(session_id=...).
func shortSID(sid string) string {
	if sid == "" || sid == "unknown" {
		return "?"
	}
	if len(sid) > 13 {
		return sid[:13]
	}
	return sid
}
