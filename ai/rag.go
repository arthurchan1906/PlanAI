package ai

import "sort"

// HybridSearch performs a two-stage search:
//  1. FTS5 keyword recall (3x limit candidates for reranking headroom)
//  2. Embedding-based semantic rerank (when an Embedder is available)
//
// Falls back gracefully: if the embedder is nil or fails, returns FTS5 results as-is.
func HybridSearch(query string, limit int, embedder Embedder, fts5Search func(string, int) []SearchResultProvider) []SearchResultProvider {
	candidates := fts5Search(query, limit*3)
	if len(candidates) == 0 {
		return candidates
	}

	if embedder == nil {
		if len(candidates) > limit {
			return candidates[:limit]
		}
		return candidates
	}

	// Collect texts for embedding
	texts := make([]string, len(candidates))
	for i, c := range candidates {
		texts[i] = c.SearchText()
	}

	// Embed query + candidates in one batch
	allTexts := make([]string, 1+len(texts))
	allTexts[0] = query
	copy(allTexts[1:], texts)

	embs, err := embedder.Embed(allTexts)
	if err != nil || len(embs) != len(allTexts) {
		// Fall back to FTS5 order
		if len(candidates) > limit {
			return candidates[:limit]
		}
		return candidates
	}

	queryEmb := embs[0]
	candEmbs := embs[1:]

	// Score each candidate by cosine similarity to query
	type scored struct {
		candidate SearchResultProvider
		score     float64
	}
	scoredList := make([]scored, len(candidates))
	for i := range candidates {
		scoredList[i] = scored{
			candidate: candidates[i],
			score:     CosineSimilarity(queryEmb, candEmbs[i]),
		}
	}

	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	if len(scoredList) > limit {
		scoredList = scoredList[:limit]
	}

	result := make([]SearchResultProvider, len(scoredList))
	for i, s := range scoredList {
		result[i] = s.candidate
	}
	return result
}

// SearchResultProvider is the interface that FTS5 results must satisfy
// to participate in HybridSearch.
type SearchResultProvider interface {
	SearchText() string
}
