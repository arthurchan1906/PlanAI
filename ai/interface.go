// Package ai provides lightweight AI capabilities via OpenAI-compatible HTTP APIs.
// All implementations degrade gracefully when no AI endpoint is configured.
package ai

// Embedder converts text to embedding vectors.
// Batch interface: accepts multiple texts and returns one vector per text.
type Embedder interface {
	Embed(texts []string) ([][]float64, error)
}

// Summarizer condenses text according to an instruction.
// Returns the original text (possibly truncated) when AI is unavailable.
type Summarizer interface {
	Summarize(text, instruction string) (string, error)
}

// Searcher provides semantic similarity ranking over candidate strings.
type Searcher interface {
	RankSimilarity(query string, candidates []string) ([]RankedResult, error)
}

// RankedResult is a single similarity-ranked item.
type RankedResult struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
	Index int     `json:"index"`
}
