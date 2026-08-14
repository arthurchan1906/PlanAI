package search

import (
	"testing"

	"aipmc/ai"
)

// Regression: RerankWithAI must re-rank the candidates the caller already
// fetched — it previously re-ran FTS5 inside the callback, ignoring the
// candidates argument and doubling the search work.
func TestRerankWithAIUsesProvidedCandidates(t *testing.T) {
	candidates := []Hit{
		{Type: "task", ID: "t1", Title: "候选一"},
		{Type: "task", ID: "t2", Title: "候选二"},
		{Type: "commit", ID: "c1", Title: "候选三"},
	}
	// Unconfigured client → Embed errors, HybridSearch falls back to the
	// recall candidates. The point is those candidates must be the ones the
	// caller provided (no re-run of FTS5 against the DB).
	got := RerankWithAI(&ai.Client{}, "anything", 2, candidates)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (candidates capped by limit)", len(got))
	}
	for i, h := range got {
		if h.ID != candidates[i].ID {
			t.Errorf("got[%d].ID = %q, want %q — candidates were ignored", i, h.ID, candidates[i].ID)
		}
	}
}
