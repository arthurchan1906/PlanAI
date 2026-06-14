package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pmdb "aipmc/db"
)

// EditPair is one old/new snippet from Cursor afterFileEdit.
type EditPair struct {
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

type cursorCachedEdit struct {
	FilePath      string     `json:"file_path"`
	Edits         []EditPair `json:"edits"`
	UpdatedAt     int64      `json:"updated_at"`
	LoggedToDB    bool       `json:"logged_to_db,omitempty"`
	GenerationID  string     `json:"generation_id,omitempty"`
	EditSignature string     `json:"edit_signature,omitempty"`
}

type cursorEditCacheFile struct {
	Entries map[string]cursorCachedEdit `json:"entries"`
}

var cursorEditCacheMu sync.Mutex

func cursorEditCachePath() (string, error) {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(runtimeDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "cursor-edits.json"), nil
}

func cursorEditLockPath() (string, error) {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(runtimeDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "cursor-edits.lock"), nil
}

func cursorEditCacheKey(sessionID, filePath string) string {
	abs, err := filepath.Abs(filepath.Clean(filePath))
	if err != nil {
		abs = filepath.Clean(filePath)
	}
	return sessionID + "|" + strings.ToLower(abs)
}

// withCursorEditCacheLock serializes cache access across concurrent hook processes.
func withCursorEditCacheLock(fn func()) {
	lockPath, err := cursorEditLockPath()
	if err != nil {
		fn()
		return
	}
	var release func()
	for i := 0; i < 200; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			release = func() {
				f.Close()
				_ = os.Remove(lockPath)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if release != nil {
		defer release()
	}
	fn()
}

func loadCursorEditCache() cursorEditCacheFile {
	path, err := cursorEditCachePath()
	if err != nil {
		return cursorEditCacheFile{Entries: map[string]cursorCachedEdit{}}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return cursorEditCacheFile{Entries: map[string]cursorCachedEdit{}}
	}
	var cache cursorEditCacheFile
	if json.Unmarshal(data, &cache) != nil || cache.Entries == nil {
		return cursorEditCacheFile{Entries: map[string]cursorCachedEdit{}}
	}
	return cache
}

func saveCursorEditCache(cache cursorEditCacheFile) {
	path, err := cursorEditCachePath()
	if err != nil {
		return
	}
	if cache.Entries == nil {
		cache.Entries = map[string]cursorCachedEdit{}
	}
	cutoff := time.Now().Add(-10 * time.Minute).Unix()
	for k, v := range cache.Entries {
		if v.UpdatedAt < cutoff {
			delete(cache.Entries, k)
		}
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

func cacheCursorFileEdit(sessionID, filePath string, edits []EditPair) {
	if sessionID == "" || filePath == "" || len(edits) == 0 {
		return
	}
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		cache.Entries[cursorEditCacheKey(sessionID, filePath)] = cursorCachedEdit{
			FilePath:  filePath,
			Edits:     edits,
			UpdatedAt: time.Now().Unix(),
		}
		saveCursorEditCache(cache)
	})
}

func markCursorFileEditLogged(sessionID, filePath string) {
	if sessionID == "" || filePath == "" {
		return
	}
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		key := cursorEditCacheKey(sessionID, filePath)
		entry, ok := cache.Entries[key]
		if !ok {
			return
		}
		entry.LoggedToDB = true
		entry.UpdatedAt = time.Now().Unix()
		cache.Entries[key] = entry
		saveCursorEditCache(cache)
	})
}

func wasCursorFileEditLogged(sessionID, filePath string) bool {
	if sessionID == "" || filePath == "" {
		return false
	}
	var logged bool
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		entry, ok := cache.Entries[cursorEditCacheKey(sessionID, filePath)]
		logged = ok && entry.LoggedToDB
	})
	return logged
}

// wasCursorFileEditRecentlyLogged returns true if the same (session, file) was
// logged to DB within the last 5 seconds — used to suppress rapid duplicate
// afterFileEdit events from Cursor.
func wasCursorFileEditRecentlyLogged(sessionID, filePath string) bool {
	if sessionID == "" || filePath == "" {
		return false
	}
	var recent bool
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		entry, ok := cache.Entries[cursorEditCacheKey(sessionID, filePath)]
		recent = ok && entry.LoggedToDB && time.Now().Unix()-entry.UpdatedAt < 5
	})
	return recent
}

func takeCursorFileEdit(sessionID, filePath string) *cursorCachedEdit {
	if sessionID == "" || filePath == "" {
		return nil
	}
	var entry *cursorCachedEdit
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		key := cursorEditCacheKey(sessionID, filePath)
		e, ok := cache.Entries[key]
		if !ok {
			return
		}
		delete(cache.Entries, key)
		saveCursorEditCache(cache)
		copy := e
		entry = &copy
	})
	return entry
}

// takeCursorFileEditWithRetry waits briefly for afterFileEdit to finish caching.
func takeCursorFileEditWithRetry(sessionID, filePath string, maxWait time.Duration) *cursorCachedEdit {
	deadline := time.Now().Add(maxWait)
	for {
		if entry := takeCursorFileEdit(sessionID, filePath); entry != nil {
			return entry
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// CacheFileEdit stores afterFileEdit payloads for pairing with postToolUse.
func CacheFileEdit(sessionID, filePath string, edits []EditPair) {
	cacheCursorFileEdit(sessionID, filePath, edits)
}

func parseCursorFileEdits(raw json.RawMessage) []EditPair {
	if len(raw) == 0 {
		return nil
	}
	var edits []EditPair
	if json.Unmarshal(raw, &edits) == nil && len(edits) > 0 {
		return normalizeEditPairs(edits)
	}
	// JSON parse failed (e.g., Chinese characters containing unescaped bytes
	// that break the JSON structure). Fall back to lenient extraction from
	// the raw "edits" array segment.
	if fallback := parseEditsLenient(string(raw)); len(fallback) > 0 {
		return normalizeEditPairs(fallback)
	}
	return nil
}

// ParseEditsLenient extracts edit pairs from raw hook edits JSON (exported for tests).
func ParseEditsLenient(raw string) []EditPair {
	return parseEditsLenient(raw)
}

// parseEditsLenient extracts edit pairs from a potentially corrupted JSON
// string by scanning for "old_string" / "new_string" keys within the edits
// array. Used as fallback when json.Unmarshal fails on the whole payload.
func parseEditsLenient(raw string) []EditPair {
	raw = strings.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		if pairs := extractEditPairsFromArray(raw); len(pairs) > 0 {
			return pairs
		}
		if pairs := extractEditPairsFieldScan(raw); len(pairs) > 0 {
			return pairs
		}
	}
	// Locate the "edits" array in a full hook JSON body.
	const marker = `"edits":`
	i := strings.Index(raw, marker)
	if i < 0 {
		return extractEditPairsFieldScan(raw)
	}
	rest := strings.TrimSpace(raw[i+len(marker):])
	if len(rest) == 0 || rest[0] != '[' {
		return extractEditPairsFieldScan(raw)
	}
	arrayJSON := extractBalancedJSONStr(rest, '[', ']')
	if arrayJSON == "" {
		return extractEditPairsFieldScan(raw)
	}
	if pairs := extractEditPairsFromArray(arrayJSON); len(pairs) > 0 {
		return pairs
	}
	return extractEditPairsFieldScan(arrayJSON)
}

// extractBalancedJSONStr returns a balanced JSON [...] or {...} substring.
func extractBalancedJSONStr(s string, open, close byte) string {
	if len(s) == 0 || s[0] != open {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		if inStr {
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return ""
}

// extractEditPairsFromArray scans a JSON array string like
// [{"old_string":"...","new_string":"..."},...] and extracts each pair
// leniently using regex, without requiring the whole string to be valid JSON.
func extractEditPairsFromArray(arrayStr string) []EditPair {
	var pairs []EditPair
	// Find each {...} object in the array.
	for i := 0; i < len(arrayStr); i++ {
		if arrayStr[i] == '{' {
			obj := extractBalancedJSONStr(arrayStr[i:], '{', '}')
			if obj == "" {
				continue
			}
			i += len(obj) - 1
			oldStr := extractLenientStringField(obj, "old_string")
			newStr := extractLenientStringField(obj, "new_string")
			if oldStr != "" || newStr != "" {
				pairs = append(pairs, EditPair{OldString: oldStr, NewString: newStr})
			}
		}
	}
	return pairs
}

// extractEditPairsFieldScan finds old/new pairs by key markers, tolerating broken JSON.
func extractEditPairsFieldScan(s string) []EditPair {
	const oldMarker = `"old_string":"`
	const newMarker = `"new_string":"`
	var pairs []EditPair
	searchFrom := 0
	for {
		oRel := strings.Index(s[searchFrom:], oldMarker)
		if oRel < 0 {
			break
		}
		oStart := searchFrom + oRel + len(oldMarker)
		oldStr, oUsed := scanLenientJSONStringContent(s[oStart:])
		afterOld := oStart + oUsed
		nRel := strings.Index(s[afterOld:], newMarker)
		if nRel < 0 {
			break
		}
		nStart := afterOld + nRel + len(newMarker)
		newStr, nUsed := scanLenientJSONStringContent(s[nStart:])
		if oldStr != "" || newStr != "" {
			pairs = append(pairs, EditPair{OldString: oldStr, NewString: newStr})
		}
		searchFrom = nStart + nUsed
	}
	return pairs
}

// extractLenientStringField extracts a string field value from a JSON object
// fragment using regex, tolerating invalid UTF-8 and minor JSON corruption.
func extractLenientStringField(obj, field string) string {
	// Build a regex that finds "field":"<value>" inside the object.
	// Use a raw search to avoid regex issues with invalid bytes.
	marker := `"` + field + `":`
	i := strings.Index(obj, marker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(obj[i+len(marker):])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:] // skip opening quote
	oldStr, _ := scanLenientJSONStringContent(rest)
	return oldStr
}

// scanLenientJSONStringContent reads a JSON string value; rest begins after the opening quote.
func scanLenientJSONStringContent(rest string) (value string, consumed int) {
	var buf strings.Builder
	esc := false
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if esc {
			buf.WriteByte('\\')
			buf.WriteByte(c)
			esc = false
			continue
		}
		if c == '\\' {
			esc = true
			continue
		}
		if c == '"' {
			// Cursor sometimes emits unescaped " before \" (Python """ docstrings).
			if j+1 < len(rest) && rest[j+1] == '\\' {
				buf.WriteByte(c)
				continue
			}
			return unescapeJSONString(buf.String()), j + 1
		}
		buf.WriteByte(c)
	}
	return unescapeJSONString(buf.String()), len(rest)
}

// unescapeJSONString decodes JSON string escapes (\n, \uXXXX, \") from a raw fragment.
func unescapeJSONString(raw string) string {
	if raw == "" {
		return raw
	}
	var s string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &s); err == nil {
		return s
	}
	// Fallback: manual unescape for common sequences.
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(raw)
}

// normalizeEditPairs repairs GBK mojibake in afterFileEdit diff strings.
func normalizeEditPairs(edits []EditPair) []EditPair {
	out := make([]EditPair, len(edits))
	for i, e := range edits {
		out[i] = EditPair{
			OldString: FixHookText(e.OldString),
			NewString: FixHookText(e.NewString),
		}
	}
	return out
}

// IsNewFileFromEdits returns true when a single empty-old edit matches full write content.
func IsNewFileFromEdits(edits []EditPair, writeContent string) bool {
	return isCursorNewFileFromEdits(edits, writeContent)
}

func isCursorNewFileFromEdits(edits []EditPair, writeContent string) bool {
	if len(edits) != 1 || edits[0].OldString != "" {
		return false
	}
	if writeContent == "" {
		return false
	}
	return edits[0].NewString == writeContent
}

// IsLikelyNewFileEdit detects whole-file creation deferred to postToolUse Write.
func IsLikelyNewFileEdit(edits []EditPair) bool {
	return isLikelyNewFileEdit(edits)
}

func isLikelyNewFileEdit(edits []EditPair) bool {
	if len(edits) != 1 {
		return false
	}
	e := edits[0]
	if e.OldString != "" {
		return false
	}
	s := strings.TrimSpace(e.NewString)
	if strings.HasPrefix(s, "package ") || strings.HasPrefix(s, "#!/") {
		return true
	}
	return strings.Count(e.NewString, "\n") >= 2 && len(e.NewString) > 20
}

func primaryCursorEdit(edits []EditPair) (oldStr, newStr string) {
	for _, e := range edits {
		if e.OldString != "" {
			return e.OldString, e.NewString
		}
	}
	var best EditPair
	for _, e := range edits {
		if len(e.NewString) > len(best.NewString) {
			best = e
		}
	}
	return best.OldString, best.NewString
}

// ApplyEditsToMeta writes diff metadata for the frontend DiffPanel.
func ApplyEditsToMeta(meta map[string]any, filePath string, edits []EditPair, writeContent string) {
	applyEditsToMeta(meta, filePath, edits, writeContent)
}

func applyEditsToMeta(meta map[string]any, filePath string, edits []EditPair, writeContent string) {
	if filePath != "" {
		meta["file_path"] = filePath
	}
	if len(edits) == 0 {
		return
	}

	if isLikelyNewFileEdit(edits) {
		meta["type"] = "new_file"
		if _, newStr := primaryCursorEdit(edits); newStr != "" {
			meta["new_string"] = FixHookText(newStr)
		}
		return
	}

	if isCursorNewFileFromEdits(edits, writeContent) {
		meta["type"] = "new_file"
		return
	}

	meta["type"] = "edit"
	oldStr, newStr := primaryCursorEdit(edits)
	oldStr = FixHookText(oldStr)
	newStr = FixHookText(newStr)
	if oldStr != "" {
		meta["old_string"] = oldStr
	}
	if newStr != "" {
		meta["new_string"] = newStr
	}
	if len(edits) > 1 {
		all := make([]map[string]string, 0, len(edits))
		for _, e := range edits {
			all = append(all, map[string]string{
				"old_string": FixHookText(e.OldString),
				"new_string": FixHookText(e.NewString),
			})
		}
		meta["all_edits"] = all
	}
}

func buildAfterFileEditMeta(baseMetaJSON, filePath string, edits []EditPair) string {
	var meta map[string]any
	if err := json.Unmarshal([]byte(baseMetaJSON), &meta); err != nil {
		meta = map[string]any{}
	}
	applyEditsToMeta(meta, filePath, edits, "")
	b, _ := json.Marshal(meta)
	return string(b)
}

func editSignature(edits []EditPair) string {
	b, err := json.Marshal(edits)
	if err != nil {
		return ""
	}
	return string(b)
}

func fileEditLogKey(sessionID, generationID, filePath string) string {
	gen := generationID
	if gen == "" {
		gen = "unknown"
	}
	return sessionID + "|" + gen + "|" + strings.ToLower(filepath.Clean(filePath))
}

func wasDuplicateFileEdit(sessionID, generationID, filePath string, edits []EditPair) bool {
	if sessionID == "" || filePath == "" {
		return false
	}
	sig := editSignature(edits)
	var dupe bool
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		entry, ok := cache.Entries[fileEditLogKey(sessionID, generationID, filePath)]
		if !ok {
			return
		}
		// Only suppress rapid re-fires of the exact same payload (Cursor duplicate hook).
		if entry.LoggedToDB && entry.EditSignature == sig && time.Now().Unix()-entry.UpdatedAt < 1 {
			dupe = true
		}
	})
	return dupe
}

func markFileEditLogged(sessionID, generationID, filePath string, edits []EditPair) {
	if sessionID == "" || filePath == "" {
		return
	}
	withCursorEditCacheLock(func() {
		cache := loadCursorEditCache()
		key := fileEditLogKey(sessionID, generationID, filePath)
		entry := cache.Entries[key]
		entry.FilePath = filePath
		entry.Edits = edits
		entry.UpdatedAt = time.Now().Unix()
		entry.LoggedToDB = true
		entry.GenerationID = generationID
		entry.EditSignature = editSignature(edits)
		if cache.Entries == nil {
			cache.Entries = map[string]cursorCachedEdit{}
		}
		cache.Entries[key] = entry
		saveCursorEditCache(cache)
	})
}
