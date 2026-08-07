package session

import (
	"os"
	"strings"
	"time"

	"aipmc/ai"
	"aipmc/u"
)

// RunAuto starts a background goroutine that periodically runs the session
// review pipeline (B1 → L2 → cross-session knowledge) and L3 reconciliation
// across all registered projects. The home project runs first, then all
// other registered projects in sequence.
// The first run happens after 5 seconds, then every interval thereafter.
// getSummarizer is called each tick to fetch the current AI client,
// so config changes (e.g. model switch via web UI) take effect without restart.
func RunAuto(getSummarizer func() ai.Summarizer, interval time.Duration, projectPaths []string) {
	home, _ := os.Getwd()
	u.LogShared("PIPELINE", "auto-run started interval=%v projects=%d home=%s", interval, len(projectPaths), home)

	go func() {
		time.Sleep(5 * time.Second)
		runAllProjects(home, projectPaths, getSummarizer())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			runAllProjects(home, projectPaths, getSummarizer())
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

	var result RunResult
	err := retryPipelineBusy(func() error {
		r, rerr := Run(RunOpts{
			Since:       time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05"),
			Limit:       50,
			ProjectPath: projectPath,
			Summarizer:  summarizer,
		})
		if rerr == nil {
			result = r
		}
		return rerr
	})
	if err != nil {
		u.LogShared("PIPELINE", "review error: %v", err)
	} else {
		u.LogShared("PIPELINE", "review done sessions=%d completed=%d baseline=%d",
			result.Reviewed, result.Completed, result.Baseline)
	}

	var recResult ReconcileResult
	recErr := retryPipelineBusy(func() error {
		r, rerr := Reconcile(
			time.Now().Add(-6 * time.Hour).Format("2006-01-02T15:04:05"),
			projectPath,
		)
		if rerr == nil {
			recResult = r
		}
		return rerr
	})
	if recErr != nil {
		u.LogShared("PIPELINE", "reconcile error: %v", recErr)
	} else {
		u.LogShared("RECONCILE", "project=%s sessions=%d auto_linked=%d tentative=%d",
			projectPath, recResult.SessionsReviewed, len(recResult.AutoLinked), len(recResult.TentativeLinks))
		u.LogShared("PIPELINE", "reconcile done auto_linked=%d tentative=%d",
			len(recResult.AutoLinked), len(recResult.TentativeLinks))
	}

	// Phase 2: emergence detection (zero-LLM, rules-driven)
	emergeOnce(projectPath)
	u.LogShared("EMERGE", "project=%s detection complete", projectPath)
}

// retryPipelineBusy retries Run/Reconcile on SQLITE_BUSY — multi-agent
// concurrent writes are the dominant pipeline failure (8/7 实测 45/54
// review+reconcile error 为 database is locked)。Store 层写操作已有
// retryOnBusy，但 Run/Reconcile 是长流程，内部任意一步 BUSY 都会整段失败。
// 指数退避：500ms / 1s / 2s，共 3 次。
func retryPipelineBusy(fn func() error) error {
	var err error
	for i := 0; i < 3; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(err.Error(), "database is locked") {
			return err
		}
		time.Sleep(time.Duration(1<<uint(i)) * 500 * time.Millisecond)
	}
	return err
}
