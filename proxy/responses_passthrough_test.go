package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pmdb "aipmc/db"
)

// TestResponsesPassthroughForwardsNativeBody 验证端到端：给原生 Responses body，
// 路由解析后目标 URL 拼 {ResponsesURL}/responses，body 的 model 被替换为
// ModelResponses，上游收到字节级原生 body（virtual model 不泄漏）。
func TestResponsesPassthroughForwardsNativeBody(t *testing.T) {
	// 固定 codex current-model 覆盖为空，让 peeked model 决定路由（确定性）。
	prev := pmdb.LoadCurrentModel("codex")
	defer func() {
		pmdb.SaveCurrentModel("codex", prev) //nolint:errcheck
	}()
	if err := pmdb.SaveCurrentModel("codex", ""); err != nil {
		t.Fatalf("pin codex override: %v", err)
	}
	prevCfg := loadCfg()
	defer storeCfg(prevCfg)

	var gotURL string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_x","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	reg := &pmdb.ModelRegistry{
		Version: 1,
		Providers: []pmdb.Provider{{
			Name:         "deepseek",
			OpenAIURL:    "https://api.deepseek.com/v1",
			ResponsesURL: upstream.URL,
		}},
		Models: []pmdb.VirtualModel{{
			ID: "deepseek-v4-flash",
			Routes: []pmdb.ModelRoute{{
				Provider:       "deepseek",
				ModelOpenAI:    "deepseek-chat",
				ModelResponses: "deepseek-v4-real",
			}},
		}},
	}
	storeCfg(&proxyCfg{
		upstreamURL: "http://127.0.0.1:1/v1", // 不应命中：路由应指向 upstream.URL
		router:      &ModelRouter{registry: reg},
	})

	// 纯函数视角断言 URL 拼接 + model 替换（brief Step 2 的内建验证）。
	body := []byte(`{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":"hi"}]}`)
	url, replaced := buildResponsesPassthrough(upstream.URL, "deepseek-v4-real", body)
	if url != strings.TrimRight(upstream.URL, "/")+"/responses" {
		t.Fatalf("url: got %q", url)
	}
	if !strings.Contains(string(replaced), `"deepseek-v4-real"`) {
		t.Fatalf("model not replaced: %s", replaced)
	}

	// 端到端驱动 handler，断言上游真正收到替换后的原生 body。
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	handleResponsesPassthrough(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if gotURL != "/responses" {
		t.Fatalf("upstream path: got %q want /responses", gotURL)
	}
	if !strings.Contains(string(gotBody), `"deepseek-v4-real"`) {
		t.Fatalf("model not replaced upstream: %s", gotBody)
	}
	if strings.Contains(string(gotBody), `"deepseek-v4-flash"`) {
		t.Fatalf("virtual model leaked upstream: %s", gotBody)
	}
}

// TestExtractResponsesStreamUsage 验证从上游 Responses SSE 解析 token 用量。
func TestExtractResponsesStreamUsage(t *testing.T) {
	sse := `data: {"type":"response.output_text.delta","delta":"Hello"}

data: {"type":"response.completed","response":{"status":"completed"},"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33}}

`
	in, out := extractResponsesStreamUsage(sse)
	if in != 11 || out != 22 {
		t.Fatalf("usage: got in=%d out=%d want in=11 out=22", in, out)
	}
}

// Compile-time guard: responseWrapper must implement http.Flusher so the
// byte-level SSE flush path in handleResponsesPassthroughStream is live in
// production (handler() always wraps the writer in *responseWrapper). Without
// Flush(), `w.(http.Flusher)` is nil and SSE goes out in ~4KB net/http bursts.
var _ http.Flusher = (*responseWrapper)(nil)

// pinCodexModel clears the codex current-model override for the duration of the
// test and restores the previous value on completion (net-zero on
// ~/.aipmc/current_model — the same pattern as TestResponsesPassthroughForwardsNativeBody).
func pinCodexModel(t *testing.T) {
	t.Helper()
	prev := pmdb.LoadCurrentModel("codex")
	t.Cleanup(func() {
		pmdb.SaveCurrentModel("codex", prev) //nolint:errcheck
	})
	if err := pmdb.SaveCurrentModel("codex", ""); err != nil {
		t.Fatalf("pin codex override: %v", err)
	}
}

// TestHandlerGateSendsNativeToResponses drives the real handler()/router entry
// with responses configured (gate-true): the request must be routed to
// handleResponsesPassthrough, so the upstream receives POST /responses with the
// native `input` body and model replaced to ModelResponses. upstreamURL is a
// dead address to prove the route (not the global fallback) was used.
func TestHandlerGateSendsNativeToResponses(t *testing.T) {
	pinCodexModel(t)
	prevCfg := loadCfg()
	defer storeCfg(prevCfg)

	var gotPath string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_x","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	storeCfg(&proxyCfg{
		upstreamURL: "http://127.0.0.1:1/v1", // must not be hit: route should win
		router: &ModelRouter{registry: &pmdb.ModelRegistry{
			Version: 1,
			Providers: []pmdb.Provider{{
				Name:         "deepseek",
				OpenAIURL:    "http://127.0.0.1:1/v1",
				ResponsesURL: upstream.URL,
			}},
			Models: []pmdb.VirtualModel{{
				ID: "deepseek-v4-flash",
				Routes: []pmdb.ModelRoute{{
					Provider:       "deepseek",
					ModelOpenAI:    "deepseek-chat",
					ModelResponses: "deepseek-v4-real",
				}},
			}},
		}},
	})

	body := `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gotPath != "/responses" {
		t.Fatalf("upstream path: got %q want /responses", gotPath)
	}
	if !strings.Contains(string(gotBody), `"deepseek-v4-real"`) {
		t.Fatalf("model not replaced to ModelResponses: %s", gotBody)
	}
	if strings.Contains(string(gotBody), `"deepseek-v4-flash"`) {
		t.Fatalf("virtual model leaked upstream: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), `"input"`) {
		t.Fatalf("native input body not preserved: %s", gotBody)
	}
}

// TestHandlerGateFallsBackToTranslation drives the real handler()/router entry
// WITHOUT responses configured (gate-false): the request must be routed to
// handleCodexUnified, so the upstream receives POST /chat/completions with the
// translated `messages` body and model replaced to ModelOpenAI.
func TestHandlerGateFallsBackToTranslation(t *testing.T) {
	pinCodexModel(t)
	prevCfg := loadCfg()
	defer storeCfg(prevCfg)

	var gotPath string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"hi from mock"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer upstream.Close()

	storeCfg(&proxyCfg{
		upstreamURL: "http://127.0.0.1:1/v1", // must not be hit: route should win
		router: &ModelRouter{registry: &pmdb.ModelRegistry{
			Version: 1,
			Providers: []pmdb.Provider{{
				Name:      "deepseek",
				OpenAIURL: upstream.URL,
			}},
			Models: []pmdb.VirtualModel{{
				ID: "deepseek-v4-flash",
				Routes: []pmdb.ModelRoute{{
					Provider:    "deepseek",
					ModelOpenAI: "deepseek-chat",
				}},
			}},
		}},
	})

	body := `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path: got %q want /chat/completions", gotPath)
	}
	if !strings.Contains(string(gotBody), `"deepseek-chat"`) {
		t.Fatalf("model not replaced to ModelOpenAI: %s", gotBody)
	}
	if strings.Contains(string(gotBody), `"deepseek-v4-flash"`) {
		t.Fatalf("virtual model leaked upstream: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), `"messages"`) {
		t.Fatalf("translated messages body not sent: %s", gotBody)
	}
}

// TestResponsesPassthroughNoFallbackOnUpstreamError: when the configured
// responses upstream returns 5xx, the handler surfaces that exact status + body
// to the client and translation (handleCodexUnified → /chat/completions) is NOT
// invoked. This is binding constraint #2 (no fallback once in passthrough).
func TestResponsesPassthroughNoFallbackOnUpstreamError(t *testing.T) {
	pinCodexModel(t)
	prevCfg := loadCfg()
	defer storeCfg(prevCfg)

	var chatHit bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			chatHit = true
		}
		http.Error(w, `{"error":"upstream exploded"}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	storeCfg(&proxyCfg{
		upstreamURL: "http://127.0.0.1:1/v1",
		router: &ModelRouter{registry: &pmdb.ModelRegistry{
			Version: 1,
			Providers: []pmdb.Provider{{
				Name:         "deepseek",
				OpenAIURL:    "http://127.0.0.1:1/v1",
				ResponsesURL: upstream.URL,
			}},
			Models: []pmdb.VirtualModel{{
				ID: "deepseek-v4-flash",
				Routes: []pmdb.ModelRoute{{
					Provider:       "deepseek",
					ModelOpenAI:    "deepseek-chat",
					ModelResponses: "deepseek-v4-real",
				}},
			}},
		}},
	})

	body := `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d want 502 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "upstream exploded") {
		t.Fatalf("client must receive the upstream's error body verbatim, got: %s", rec.Body.String())
	}
	if chatHit {
		t.Fatal("translation invoked after passthrough upstream error — binding no-fallback violated")
	}
}
