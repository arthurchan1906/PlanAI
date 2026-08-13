package proxy

import (
	"sort"
	"strings"
	"testing"
)

// Regression: OpenAI Responses format (codex /v1/responses) uses an `input`
// array — previously unparsed, so codex silently extracted 0 file paths (C2).
func TestExtractFilePathsResponsesInput(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fix src/app.go and src/util.go"}]}],"instructions":"also check src/main.go"}`)
	got := extractFilePaths(body, "codex")
	sort.Strings(got)
	want := []string{"src/app.go", "src/main.go", "src/util.go"}
	if len(got) != len(want) {
		t.Fatalf("Responses input parse: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Responses input parse: got %v, want %v", got, want)
		}
	}
}

// Regression: content as plain string in Responses input must also parse.
func TestExtractFilePathsResponsesStringContent(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":"see internal/api.go"}]}`)
	got := extractFilePaths(body, "codex")
	if len(got) != 1 || got[0] != "internal/api.go" {
		t.Fatalf("string content parse: got %v", got)
	}
}

// Anthropic messages format must keep working (claude path).
func TestExtractFilePathsAnthropicMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"touch web/frontend.tsx"}]}]}`)
	got := extractFilePaths(body, "claude")
	if len(got) != 1 || got[0] != "web/frontend.tsx" {
		t.Fatalf("Anthropic parse: got %v", got)
	}
}

// OpenAI Chat Completions format must keep working (cursor/opencode path):
// messages array with role=system/user and string content.
func TestExtractFilePathsOpenAIChat(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"you are a coder"},{"role":"user","content":"refactor services/auth.go and fix models/user.go"}]}`)
	got := extractFilePaths(body, "cursor")
	sort.Strings(got)
	want := []string{"models/user.go", "services/auth.go"}
	if len(got) != len(want) {
		t.Fatalf("chat parse: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chat parse: got %v, want %v", got, want)
		}
	}
}

// Gemini format must keep working: systemInstruction.parts[].text.
func TestExtractFilePathsGeminiSystemInstruction(t *testing.T) {
	body := []byte(`{"systemInstruction":{"parts":[{"text":"examine cmd/serve.go and proxy/router.go"}]},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	got := extractFilePaths(body, "gemini")
	sort.Strings(got)
	want := []string{"cmd/serve.go", "proxy/router.go"}
	if len(got) != len(want) {
		t.Fatalf("gemini parse: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("gemini parse: got %v, want %v", got, want)
		}
	}
}

// W1 (8/13): extractSessionID — codex 在 client_metadata.session_id，兼容顶层字段。
func TestExtractSessionID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"codex client_metadata", `{"client_metadata":{"session_id":"ses_abc123"}}`, "ses_abc123"},
		{"top-level", `{"session_id":"ses_top"}`, "ses_top"},
		{"none", `{"foo":1}`, ""},
		{"empty body", ``, ""},
		{"invalid json", `not-json`, ""},
	}
	for _, c := range cases {
		if got := extractSessionID([]byte(c.body)); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// W1 (8/13): buildContextBlock 分段裁剪计数——goals 尾部超 cap 只记 goals 段。
func TestBuildContextBlockSegCounts(t *testing.T) {
	long := strings.Repeat("x", 300)
	block, sc := buildContextBlock(
		[]string{long, long, long},
		nil, nil, nil, "")
	if sc.goals != 1 {
		t.Fatalf("goals suppressed: got %d want 1", sc.goals)
	}
	if sc.total() != 1 {
		t.Fatalf("total suppressed: got %d want 1", sc.total())
	}
	if block == "" {
		t.Fatal("block should not be empty")
	}
	// 全部放得下 → 零裁剪
	if _, sc2 := buildContextBlock([]string{"short"}, nil, nil, nil, ""); sc2.total() != 0 {
		t.Fatalf("no suppression expected, got %d", sc2.total())
	}
}
