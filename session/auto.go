package session

import (
	"time"

	"aipmc/ai"
	"aipmc/u"
)

// RunAuto starts a background goroutine that periodically runs the session
// review pipeline (B1 → L2 → cross-session knowledge) and L3 reconciliation.
// The first run happens after 5 seconds, then every interval thereafter.
// summarizer may be nil (L2 gracefully degrades without AI).
func RunAuto(summarizer ai.Summarizer, interval time.Duration) {
	u.LogShared("PIPELINE", "auto-run started interval=%v", interval)
	go func() {
		time.Sleep(5 * time.Second)
		runOnce(summarizer)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			runOnce(summarizer)
		}
	}()
}

func runOnce(summarizer ai.Summarizer) {
	u.LogShared("PIPELINE", "scan start since=24h")

	result, err := Run(RunOpts{
		Since:      time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05"),
		Limit:      50,
		Summarizer: summarizer,
	})
	if err != nil {
		u.LogShared("PIPELINE", "review error: %v", err)
	} else {
		u.LogShared("PIPELINE", "review done sessions=%d completed=%d baseline=%d",
			result.Reviewed, result.Completed, result.Baseline)
	}

	recResult, err := Reconcile(time.Now().Add(-6 * time.Hour).Format("2006-01-02T15:04:05"))
	if err != nil {
		u.LogShared("PIPELINE", "reconcile error: %v", err)
	} else {
		u.LogShared("PIPELINE", "reconcile done auto_linked=%d tentative=%d",
			len(recResult.AutoLinked), len(recResult.TentativeLinks))
	}
}
