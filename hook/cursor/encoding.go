package cursor

import (
	"bytes"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// FixHookText repairs Cursor hook strings garbled on Chinese Windows.
// Cursor UTF-8-encodes Chinese, mis-decodes those bytes as GBK, then stores the
// result as UTF-8 in JSON. Re-encoding as GBK and decoding as UTF-8 reverses it.
func FixHookText(s string) string {
	if s == "" {
		return s
	}
	if fixed := fixUTF8MisreadAsGBK(s); fixed != s {
		return fixed
	}
	if isMostlyASCII(s) {
		if fixed := fixLatin1Mojibake(s); fixed != s && preferFixedText(s, fixed) {
			return fixed
		}
		return s
	}
	return fixHookTextBySegment(s)
}

func fixHookTextBySegment(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); {
		if runes[i] < 128 {
			j := i + 1
			for j < len(runes) && runes[j] < 128 {
				j++
			}
			b.WriteString(string(runes[i:j]))
			i = j
			continue
		}
		j := i + 1
		for j < len(runes) && runes[j] >= 128 {
			j++
		}
		segment := string(runes[i:j])
		if fixed := fixUTF8MisreadAsGBK(segment); fixed != segment {
			b.WriteString(fixed)
		} else if fixed := fixLatin1Mojibake(segment); fixed != segment && preferFixedText(segment, fixed) {
			b.WriteString(fixed)
		} else {
			b.WriteString(segment)
		}
		i = j
	}
	return b.String()
}

func fixUTF8MisreadAsGBK(s string) string {
	// Process in segments: ASCII passes through; consecutive non-ASCII
	// characters are GBK-encoded as a block to produce valid UTF-8.
	// Per-character encoding doesn't work because individual 2-byte GBK
	// outputs are fragments of larger UTF-8 sequences.
	runes := []rune(s)
	var result strings.Builder
	i := 0
	fixedAny := false

	for i < len(runes) {
		if runes[i] < 128 {
			result.WriteRune(runes[i])
			i++
			continue
		}
		// Collect consecutive non-ASCII runes into a segment.
		j := i
		for j < len(runes) && runes[j] >= 128 {
			j++
		}
		segment := string(runes[i:j])
		if fixed := fixNonASCIISegment(segment); fixed != segment {
			result.WriteString(fixed)
			fixedAny = true
		} else {
			result.WriteString(segment)
		}
		i = j
	}

	if !fixedAny {
		return s
	}
	out := result.String()
	if !utf8.ValidString(out) {
		return s
	}
	if countMojibakeMarkers(out) < countMojibakeMarkers(s) || preferFixedText(s, out) {
		return out
	}
	return s
}

// fixNonASCIISegment tries to reverse GBK mojibake in a contiguous block of
// non-ASCII characters. It first attempts encoding the whole segment as GBK;
// if that fails (PUA characters present), it retries character-by-character,
// rejecting the fix entirely if any individual character fails.
func fixNonASCIISegment(segment string) string {
	// Fast path: encode the whole segment as GBK at once.
	reader := transform.NewReader(strings.NewReader(segment), simplifiedchinese.GBK.NewEncoder())
	out, err := io.ReadAll(reader)
	if err == nil && len(out) > 0 && utf8.Valid(out) && string(out) != segment {
		return string(out)
	}

	// Slow path: some characters (PUA) can't be GBK-encoded. Try one by one;
	// if any character fails, keep the original segment to avoid mixing GBK
	// byte fragments with UTF-8 runes (which would produce invalid output).
	gbkEnc := simplifiedchinese.GBK.NewEncoder()
	var buf bytes.Buffer
	for _, r := range segment {
		rdr := transform.NewReader(strings.NewReader(string(r)), gbkEnc)
		b, err := io.ReadAll(rdr)
		if err != nil || len(b) == 0 {
			return segment // can't fix — keep original
		}
		buf.Write(b)
	}
	candidate := buf.String()
	if utf8.ValidString(candidate) && candidate != segment {
		return candidate
	}
	return segment
}

// matchesGBKMojibakeForward checks that UTF-8 bytes of text mis-decoded as GBK
// reproduce the garbled hook string (Cursor's Windows encoding bug).
func matchesGBKMojibakeForward(correct, garbled string) bool {
	out, err := io.ReadAll(transform.NewReader(bytes.NewReader([]byte(correct)), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return false
	}
	return string(out) == garbled
}

func fixLatin1Mojibake(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return s
		}
		b = append(b, byte(r))
	}
	if !utf8.Valid(b) {
		return s
	}
	return string(b)
}

func preferFixedText(original, fixed string) bool {
	if fixed == "" || fixed == original {
		return false
	}
	if !utf8.ValidString(fixed) {
		return false
	}
	if hasCorruptRunes(fixed) {
		return false
	}
	origScore := chineseTextScore(original)
	fixedScore := chineseTextScore(fixed)
	if fixedScore > origScore {
		return true
	}
	// Prefer fixed when original has mojibake markers and fixed does not.
	if looksLikeMojibake(original) && !looksLikeMojibake(fixed) {
		return true
	}
	return false
}

func chineseTextScore(s string) int {
	score := 0
	for _, r := range s {
		switch {
		case r == '\uFFFD':
			score -= 5
		case unicode.Is(unicode.Han, r):
			score += 2
		case r < 128:
			score += 1
		case unicode.IsPunct(r) || unicode.IsSpace(r):
			score += 1
		default:
			score -= 1
		}
	}
	return score
}

func hasCorruptRunes(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' || (r >= 0xE000 && r <= 0xF8FF) {
			return true
		}
	}
	return false
}

func looksLikeMojibake(s string) bool {
	if s == "" || isMostlyASCII(s) {
		return false
	}
	return countMojibakeMarkers(s) >= 2
}

// countMojibakeMarkers counts common GBK-mojibake artifact characters in a string.
// Used to compare UTF-8 vs GBK→UTF-8 versions of the same raw hook data.
func countMojibakeMarkers(s string) int {
	markers := 0
	for _, r := range s {
		switch r {
		// Common artifacts when UTF-8 Chinese is mis-decoded as GBK on Windows.
		case '鏂', '娴', '涓', '涔', '鐨', '鍙', '缁', '璇', '閲', '鐩', '杩', '鏄', '鍦', '瀛', '樺', '湪', '闂', '爜', '枃', '繕',
			'姒', '浣', '宸', '鍚', '彲', '浠', '鏃', '鐢', '紝', '鍒', '鎯', '鎵', '鑰', '閮', '瑕', '浼',
			// High private-use / replacement characters also indicate mojibake.
			'�':
			markers++
		}
	}
	return markers
}

// PreferUserPromptAtSubmit resolves hook payload only (no transcript).
// Transcript is read later in flushDeferredUserPrompt once the new turn is written.
func PreferUserPromptAtSubmit(hookPrompt string) string {
	return cleanCursorPrompt(FixHookText(hookPrompt))
}

// PreferUserPrompt returns the best UTF-8 user prompt from hook payload and transcript.
// Deprecated for beforeSubmitPrompt — use PreferUserPromptAtSubmit to avoid stale transcript reads.
func PreferUserPrompt(hookPrompt, transcriptPath string) string {
	fixed := PreferUserPromptAtSubmit(hookPrompt)
	if fixed != "" && !looksLikeMojibake(fixed) && !looksLikeMojibake(hookPrompt) {
		return fixed
	}
	if transcriptPath != "" {
		if t := readLatestUserQueryFromTranscript(transcriptPath); t != "" {
			t = cleanCursorPrompt(t)
			if t != "" {
				return t
			}
		}
	}
	return fixed
}
