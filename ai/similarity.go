package ai

import "math"

// CosineSimilarity returns the cosine between two vectors.
// Returns 0 when either vector is zero-length.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// TrigramSimilarity computes Jaccard similarity over character trigrams.
// Robust to small spelling variations, word order changes, and mixed CJK/ASCII.
func TrigramSimilarity(a, b string) float64 {
	aGrams := extractTrigrams(a)
	bGrams := extractTrigrams(b)

	if len(aGrams) == 0 && len(bGrams) == 0 {
		return 1
	}
	if len(aGrams) == 0 || len(bGrams) == 0 {
		return 0
	}

	intersection := 0
	for g := range aGrams {
		if bGrams[g] {
			intersection++
		}
	}
	return float64(intersection) / float64(len(aGrams)+len(bGrams)-intersection)
}

func extractTrigrams(s string) map[string]bool {
	grams := make(map[string]bool)
	runes := []rune(s)
	// Pad with start/end markers so single characters still produce trigrams
	padded := make([]rune, 0, len(runes)+2)
	padded = append(padded, 0)
	padded = append(padded, runes...)
	padded = append(padded, 1)

	for i := 0; i < len(padded)-2; i++ {
		grams[string(padded[i:i+3])] = true
	}
	return grams
}
