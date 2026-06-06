package ai

// noopEmbedder returns an error so callers can detect missing AI config.
type noopEmbedder struct{}

func (noopEmbedder) Embed(texts []string) ([][]float64, error) {
	return nil, errNoConfig
}

// noopSummarizer returns the original text unchanged.
type noopSummarizer struct{}

func (noopSummarizer) Summarize(text, instruction string) (string, error) {
	return text, nil
}

// noopSearcher returns empty results.
type noopSearcher struct{}

func (noopSearcher) RankSimilarity(query string, candidates []string) ([]RankedResult, error) {
	return nil, errNoConfig
}

var errNoConfig = &NoConfigError{}

// NoConfigError is returned when AI is not configured.
type NoConfigError struct{}

func (e *NoConfigError) Error() string {
	return "AI not configured: set AI_ENDPOINT environment variable or ai_endpoint in config.json"
}
