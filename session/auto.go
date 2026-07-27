package session

import (
	"os"
	"time"

	"aipmc/ai"
	"aipmc/u"
)

// RunAuto starts a background goroutine that periodically runs the session
// review pipeline (B1 → L2 → cross-session knowledge) and L3 reconciliation
// across all registered projects. The home project runs first, then all
// other registered projects in sequence.
// The first run happens after 5 seconds, then every interval thereafter.
// summarizer may be nil (L2 gracefully degrades without AI).
func RunAuto(summarizer ai.Summarizer, interval time.Duration, projectPaths []string) {
	home, _ := os.Getwd()
	u.LogShared("PIPELINE", "auto-run started interval=%v projects=%d home=%s", interval, len(projectPaths), home)

	go func() {
		time.Sleep(5 * time.Second)
		runAllProjects(home, projectPaths, summarizer)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			runAllProjects(home, projectPaths, summarizer)
		}
	}()
}

func runAllProjects(home string, projectPaths []string, summarizer ai.Summarizer) {
	all := dedupeProjects(home, projectPaths)
	for i, p := range all {
		u.LogShared("PIPELINE", "project=%s (%d/%d)", p, i+1, len(all))
		runOnce(p, summarizer)
	}
}

func dedupeProjects(home string, paths []string) []string {
	seen := map[string]bool{home: true}
	result := []string{home}
	for _, p := range paths {
		if p == "" || p == home || seen[p] {
			continue
		}
		// Skip paths without .pmai directory
		if _, err := os.Stat(p + "/.pmai"); os.IsNotExist(err) {
			continue
		}
		seen[p] = true
		result = append(result, p)
	}
	return result
}

func runOnce(projectPath string, summarizer ai.Summarizer) {
	u.LogShared("PIPELINE", "scan start since=24h")

	result, err := Run(RunOpts{
		Since:       time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05"),
		Limit:       50,
		ProjectPath: projectPath,
		Summarizer:  summarizer,
	})
	if err != nil {
		u.LogShared("PIPELINE", "review error: %v", err)
	} else {
		u.LogShared("PIPELINE", "review done sessions=%d completed=%d baseline=%d",
			result.Reviewed, result.Completed, result.Baseline)
	}

	recResult, err := Reconcile(
		time.Now().Add(-6 * time.Hour).Format("2006-01-02T15:04:05"),
		projectPath,
	)
	if err != nil {
		u.LogShared("PIPELINE", "reconcile error: %v", err)
	} else {
		u.LogShared("RECONCILE", "project=%s sessions=%d auto_linked=%d tentative=%d",
			projectPath, recResult.SessionsReviewed, len(recResult.AutoLinked), len(recResult.TentativeLinks))
		u.LogShared("PIPELINE", "reconcile done auto_linked=%d tentative=%d",
			len(recResult.AutoLinked), len(recResult.TentativeLinks))
	}
}
