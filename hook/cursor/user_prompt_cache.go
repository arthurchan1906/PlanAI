package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pmdb "aipmc/db"
	"aipmc/store"
)

type deferredUserPrompt struct {
	TranscriptPath string `json:"transcript_path"`
	HookPrompt     string `json:"hook_prompt,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

type deferredUserPromptFile struct {
	Entries map[string]deferredUserPrompt `json:"entries"`
}

var deferredUserMu sync.Mutex

func deferredUserPromptCachePath() (string, error) {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(runtimeDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "cursor-user-prompts.json"), nil
}

func deferredUserKey(sessionID, generationID string) string {
	if generationID == "" {
		generationID = "unknown"
	}
	return sessionID + "|" + generationID
}

func cacheDeferredUserPrompt(sessionID, generationID, transcriptPath, hookPrompt string) {
	if sessionID == "" {
		return
	}
	deferredUserMu.Lock()
	defer deferredUserMu.Unlock()

	path, err := deferredUserPromptCachePath()
	if err != nil {
		return
	}
	data, _ := os.ReadFile(path)
	var cache deferredUserPromptFile
	if len(data) > 0 {
		json.Unmarshal(data, &cache)
	}
	if cache.Entries == nil {
		cache.Entries = map[string]deferredUserPrompt{}
	}
	key := deferredUserKey(sessionID, generationID)
	cache.Entries[key] = deferredUserPrompt{
		TranscriptPath: transcriptPath,
		HookPrompt:     hookPrompt,
		CreatedAt:      time.Now().Unix(),
	}
	out, _ := json.Marshal(cache)
	_ = os.WriteFile(path, out, 0644)
}

func resolveDeferredUserPrompt(sessionID, transcriptPath, hookPrompt string) (prompt string, fromTranscript bool) {
	tpath := transcriptPath
	if tpath != "" {
		if q := readLatestUnloggedUserQueryFromTranscript(tpath, sessionID); q != "" {
			return cleanCursorPrompt(q), true
		}
		if q := readLatestUserQueryFromTranscript(tpath); q != "" {
			return cleanCursorPrompt(q), true
		}
	}
	fixed := cleanCursorPrompt(FixHookText(hookPrompt))
	if fixed != "" && !looksLikeMojibake(fixed) {
		return fixed, false
	}
	if fixed != "" {
		return fixed, false
	}
	raw := cleanCursorPrompt(hookPrompt)
	if raw != "" {
		return raw, false
	}
	return "", false
}

func flushDeferredUserPrompt(sessionID, generationID, transcriptPath string, rawJSON []byte) {
	if sessionID == "" {
		return
	}
	deferredUserMu.Lock()
	defer deferredUserMu.Unlock()

	path, err := deferredUserPromptCachePath()
	if err != nil {
		return
	}
	data, _ := os.ReadFile(path)
	var cache deferredUserPromptFile
	if len(data) > 0 {
		json.Unmarshal(data, &cache)
	}
	if cache.Entries == nil {
		return
	}

	key := deferredUserKey(sessionID, generationID)
	entry, ok := cache.Entries[key]
	if !ok {
		return
	}

	tpath := firstNonEmpty(transcriptPath, entry.TranscriptPath)
	prompt, fromTranscript := resolveDeferredUserPrompt(sessionID, tpath, entry.HookPrompt)
	if prompt == "" {
		AppendHookDiagnostic("cursor", time.Now().Format("2006-01-02T15:04:05.000"),
			"deferred user prompt flush skipped (empty): gen="+generationID)
		return
	}
	if !fromTranscript && looksLikeMojibake(prompt) {
		AppendHookDiagnostic("cursor", time.Now().Format("2006-01-02T15:04:05.000"),
			"deferred user prompt flush waiting (transcript not ready): gen="+generationID)
		return
	}

	meta := buildFullMeta("user_prompt", rawJSON)
	if _, err := store.LogDiscussion(sessionID, "user", "cursor", prompt, meta); err != nil {
		AppendHookDiagnostic("cursor", time.Now().Format("2006-01-02T15:04:05.000"),
			"deferred user prompt flush FAILED: "+err.Error())
		return
	}
	delete(cache.Entries, key)
	out, _ := json.Marshal(cache)
	_ = os.WriteFile(path, out, 0644)
}

func shouldDeferUserPrompt(hookPrompt, resolved string) bool {
	if strings.TrimSpace(hookPrompt) == "" && resolved == "" {
		return true
	}
	// Garbled hook payloads must wait for transcript backfill — never log stale transcript at submit.
	if looksLikeMojibake(hookPrompt) {
		return true
	}
	if resolved == "" || looksLikeMojibake(resolved) {
		return true
	}
	return false
}
