package cursor_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"aipmc/hook/cursor"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const iconEdit = "\U0001f4dd "
const iconNewFile = "\U0001f195 "
const iconGrep = "\U0001f50d "

func mojibakeUTF8AsGBK(s string) string {
	out, err := io.ReadAll(transform.NewReader(bytes.NewReader([]byte(s)), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		panic(err)
	}
	return string(out)
}

func TestIsNewFileFromEdits(t *testing.T) {
	content := "package main\n\nfunc main() {}\n"
	edits := []cursor.EditPair{{OldString: "", NewString: content}}
	if !cursor.IsNewFileFromEdits(edits, content) {
		t.Fatal("expected new file")
	}
	edits2 := []cursor.EditPair{{OldString: "", NewString: "\t\"os/exec\""}}
	if cursor.IsNewFileFromEdits(edits2, content) {
		t.Fatal("expected edit, not new file")
	}
}

func TestApplyEditsToMeta(t *testing.T) {
	meta := map[string]any{}
	edits := []cursor.EditPair{{OldString: "old", NewString: "new"}}
	cursor.ApplyEditsToMeta(meta, "foo.go", edits, "")
	if meta["type"] != "edit" {
		t.Fatalf("type=%v", meta["type"])
	}
	if meta["old_string"] != "old" || meta["new_string"] != "new" {
		t.Fatalf("meta=%v", meta)
	}
}

func TestIsLikelyNewFileEdit(t *testing.T) {
	insert := []cursor.EditPair{{OldString: "", NewString: "\t\"fmt\""}}
	if cursor.IsLikelyNewFileEdit(insert) {
		t.Fatal("insert snippet should not be new file")
	}
	newFile := []cursor.EditPair{{OldString: "", NewString: "package p\n\nfunc Main() {}\n"}}
	if !cursor.IsLikelyNewFileEdit(newFile) {
		t.Fatal("expected whole-file create")
	}
}

func TestFormatFileEditContent(t *testing.T) {
	meta := map[string]any{
		"type":        "edit",
		"file_path":   `D:\proj\foo.go`,
		"old_string":  "before",
		"new_string":  "after",
	}
	got := cursor.FormatFileEditContent(`D:\proj\foo.go`, meta)
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("expected diff preview, got %q", got)
	}
	if !strings.HasPrefix(got, iconEdit) {
		t.Fatalf("expected edit icon, got %q", got)
	}
}

func TestFormatFileEditContentNewFile(t *testing.T) {
	meta := map[string]any{"type": "new_file", "file_path": "x.go", "new_string": "package x\n"}
	got := cursor.FormatFileEditContent("x.go", meta)
	if !strings.HasPrefix(got, iconNewFile) {
		t.Fatalf("got %q", got)
	}
}

func TestRefreshFileToolContent(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{
		"type":      "new_file",
		"file_path": "x.go",
	})
	got := cursor.RefreshFileToolContent(iconEdit+"x.go", string(meta))
	want := iconNewFile + "x.go"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildToolContentGrepWithPath(t *testing.T) {
	input := json.RawMessage(`{"pattern":"afterFileEdit","file_path":"D:\\proj\\hook.go"}`)
	got := cursor.BuildToolContent("Grep", input, nil)
	want := iconGrep + "\"afterFileEdit\" @ D:\\proj\\hook.go"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildToolContentShellShowsCommand(t *testing.T) {
	input := json.RawMessage(`{"command":"go build -o dist\\aipmc.exe . 2>&1"}`)
	got := cursor.BuildToolContent("Shell", input, nil)
	if !strings.Contains(got, "go build") {
		t.Fatalf("expected command in shell content, got %q", got)
	}
}

func TestFinalizeToolContentFileFromMeta(t *testing.T) {
	meta := `{"type":"edit","file_path":"D:\\code\\AI\\PlanAI\\hook\\hook_cursor.go"}`
	got := cursor.FinalizeToolContent("Write", iconToolLabel(), meta, nil)
	want := iconEdit + `D:\code\AI\PlanAI\hook\hook_cursor.go`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func iconToolLabel() string { return "\U0001f6e0 Write" }

func TestEnrichWriteWithCachedEdit(t *testing.T) {
	session := "unit-write-edit"
	file := `D:\test\foo.go`
	cursor.CacheFileEdit(session, file, []cursor.EditPair{
		{OldString: "before", NewString: "after"},
	})

	meta := cursor.EnrichMeta(`{"_type":"post_tool"}`, session, "Write",
		json.RawMessage(`{"file_path":"D:\\test\\foo.go","content":"package x\n\nafter\n"}`),
		json.RawMessage(`{"success":true}`),
	)

	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "edit" {
		t.Fatalf("type=%v", m["type"])
	}
	if m["old_string"] != "before" || m["new_string"] != "after" {
		t.Fatalf("diff=%v", m)
	}
	got := cursor.RefreshFileToolContent("", meta)
	if !strings.Contains(got, file) || !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("content=%q", got)
	}
}

func TestReadLatestUserQueryFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n了解当前项目\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}
{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n目前中文还是显示乱码\n</user_query>"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := cursor.ReadLatestUserQueryFromTranscript(path)
	if got != "目前中文还是显示乱码" {
		t.Fatalf("got %q", got)
	}
}

func TestPreferTranscriptText(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n正确中文\n</user_query>"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := cursor.PreferTranscriptText("garbled", path, cursor.ReadLatestUserQueryFromTranscript)
	if got != "正确中文" {
		t.Fatalf("got %q", got)
	}
}

func TestFixHookTextGBKMojibake(t *testing.T) {
	garbled := mojibakeUTF8AsGBK("文件")
	want := "文件"
	got := cursor.FixHookText(garbled)
	if got != want {
		t.Fatalf("FixHookText(%q) = %q, want %q", garbled, got, want)
	}
}

func TestFixHookTextPreservesValidChinese(t *testing.T) {
	correct := "目前中文还是显示乱码"
	if got := cursor.FixHookText(correct); got != correct {
		t.Fatalf("FixHookText changed valid text: %q -> %q", correct, got)
	}
}

func TestPreferUserPromptFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n正确中文\n</user_query>"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := cursor.PreferUserPrompt(mojibakeUTF8AsGBK("乱码"), path)
	if got != "正确中文" {
		t.Fatalf("got %q", got)
	}
}

func TestPreferUserPromptAtSubmitDoesNotUseStaleTranscript(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n旧消息\n</user_query>"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// Garbled hook for a new prompt — submit path must not return stale transcript text.
	got := cursor.PreferUserPromptAtSubmit(mojibakeUTF8AsGBK("新消息"))
	if got == "旧消息" {
		t.Fatalf("submit path used stale transcript: %q", got)
	}
}

func TestReadLatestUnloggedUserQueryFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n第一条\n</user_query>"}]}}
{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\n第二条\n</user_query>"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// Without DB session, both appear unlogged; should return newest.
	got := cursor.ReadLatestUnloggedUserQueryFromTranscript(path, "test-session")
	if got != "第二条" {
		t.Fatalf("got %q want 第二条", got)
	}
}

func TestApplyEditsToMetaFixesMojibake(t *testing.T) {
	garbled := mojibakeUTF8AsGBK("文件")
	meta := map[string]any{}
	edits := []cursor.EditPair{{OldString: garbled, NewString: "文件编辑"}}
	cursor.ApplyEditsToMeta(meta, "main.py", edits, "")
	oldS, _ := meta["old_string"].(string)
	if oldS != "文件" {
		t.Fatalf("old_string not fixed: %q", oldS)
	}
}

func TestPreferUserPromptFixesMojibake(t *testing.T) {
	garbled := "prefix " + mojibakeUTF8AsGBK("文件")
	got := cursor.PreferUserPrompt(garbled, "")
	want := "prefix 文件"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseEditsLenientDirectArray(t *testing.T) {
	// Simulates json.RawMessage when json.Unmarshal fails on corrupt inner quotes.
	raw := `[{"old_string":"","new_string":"\"\"\"scratch/simple_nn 工具\"\"\"\n"}]`
	edits := cursor.ParseEditsLenient(raw)
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want 1", len(edits))
	}
	if edits[0].OldString != "" {
		t.Fatalf("old_string = %q", edits[0].OldString)
	}
	if !strings.Contains(edits[0].NewString, `"""scratch/simple_nn`) {
		t.Fatalf("new_string = %q", edits[0].NewString)
	}
}

func TestParseEditsLenientCorruptDocstring(t *testing.T) {
	// Cursor bug: unescaped " before \"\"\" in Python docstrings.
	corrupt := `[{"old_string":"","new_string":"\"\"\"人类可读统计摘要（hook batch 3）。\"\"\"\n    s = corpus_stats(text)"}]`
	// Replace valid closing with corrupt: 。 + raw " + \"
	corrupt = strings.Replace(corrupt, `。\"\"\"`, `。"\"\"`, 1)
	edits := cursor.ParseEditsLenient(corrupt)
	if len(edits) == 0 {
		t.Fatal("expected at least one edit pair")
	}
	if edits[0].NewString == "" {
		t.Fatal("new_string empty")
	}
	if !strings.Contains(edits[0].NewString, "corpus_stats") {
		t.Fatalf("new_string = %q", edits[0].NewString)
	}
}

func TestIsLowValueToolContent(t *testing.T) {
	for _, c := range []string{iconEdit + "&1", iconEdit + "Write", iconEdit + "edit.json,"} {
		if !cursor.IsLowValueToolContent(c) {
			t.Fatalf("expected low value: %q", c)
		}
	}
}
