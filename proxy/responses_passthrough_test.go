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
// 8/12 起 DeepSeek Responses 返回 input_tokens_details.cached_tokens——
// 此前按「无 cache 字段」固化断言，导致缓存命中恒 0（E3 cache_hit_rate 失真）。
func TestExtractResponsesStreamUsage(t *testing.T) {
	sse := `data: {"type":"response.output_text.delta","delta":"Hello"}

data: {"type":"response.completed","response":{"status":"completed"},"usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":10},"output_tokens":22,"total_tokens":33}}

`
	in, out, cacheHit, cacheCreate := extractResponsesStreamUsage(sse)
	if in != 11 || out != 22 {
		t.Fatalf("usage: got in=%d out=%d want in=11 out=22", in, out)
	}
	if cacheHit != 10 {
		t.Fatalf("deepseek cached_tokens: got hit=%d want 10", cacheHit)
	}
	if cacheCreate != 0 {
		t.Fatalf("deepseek has no cache_creation: got create=%d want 0", cacheCreate)
	}
}

func TestExtractResponsesStreamUsagePlainJSON(t *testing.T) {
	body := `{"usage":{"input_tokens":5,"output_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}`
	in, out, cacheHit, cacheCreate := extractResponsesStreamUsage(body)
	if in != 5 || out != 7 || cacheHit != 3 || cacheCreate != 2 {
		t.Fatalf("plain json usage: got in=%d out=%d hit=%d create=%d", in, out, cacheHit, cacheCreate)
	}
}

// TestExtractResponsesStreamUsageDeepSeekPlainJSON：DeepSeek Responses 非流式形态
// 的缓存命中在 input_tokens_details.cached_tokens（8/12 起支持）。
func TestExtractResponsesStreamUsageDeepSeekPlainJSON(t *testing.T) {
	body := `{"usage":{"input_tokens":50,"input_tokens_details":{"cached_tokens":30},"output_tokens":8,"total_tokens":58}}`
	in, out, cacheHit, cacheCreate := extractResponsesStreamUsage(body)
	if in != 50 || out != 8 {
		t.Fatalf("plain json usage: got in=%d out=%d want in=50 out=8", in, out)
	}
	if cacheHit != 30 {
		t.Fatalf("deepseek cached_tokens: got hit=%d want 30", cacheHit)
	}
	if cacheCreate != 0 {
		t.Fatalf("deepseek has no cache_creation: got create=%d want 0", cacheCreate)
	}
}

// TestExtractResponsesStreamUsageCachedTokensPriority：两个缓存字段同时存在时
// cached_tokens（DeepSeek/OpenAI 规范）优先于网关兼容字段 cache_read_input_tokens。
func TestExtractResponsesStreamUsageCachedTokensPriority(t *testing.T) {
	body := `{"usage":{"input_tokens":50,"input_tokens_details":{"cached_tokens":30},"output_tokens":8,"cache_read_input_tokens":99}}`
	_, _, cacheHit, _ := extractResponsesStreamUsage(body)
	if cacheHit != 30 {
		t.Fatalf("cached_tokens should win: got hit=%d want 30", cacheHit)
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

// TestHandlerGateUsesCodexOverrideForPassthrough: the routing gate must honor the
// codex current-model override BEFORE the body model. Regression test for the bug
// where switching to a responses-capable model via &aipmc-model was ignored by the
// gate: body model = "deepseek-v4-pro" (no responses) but override = "deepseek-v4-flash"
// (responses) → the request MUST go to native passthrough, not translation.
func TestHandlerGateUsesCodexOverrideForPassthrough(t *testing.T) {
	// Pin codex override to deepseek-v4-flash (responses-capable).
	prev := pmdb.LoadCurrentModel("codex")
	t.Cleanup(func() {
		pmdb.SaveCurrentModel("codex", prev) //nolint:errcheck
	})
	if err := pmdb.SaveCurrentModel("codex", "deepseek-v4-flash"); err != nil {
		t.Fatalf("pin codex override: %v", err)
	}
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
			Providers: []pmdb.Provider{
				{Name: "deepseek", OpenAIURL: "http://127.0.0.1:1/v1", ResponsesURL: upstream.URL},
			},
			Models: []pmdb.VirtualModel{
				// deepseek-v4-pro: NO model_responses → gate would be false if body model governs.
				{ID: "deepseek-v4-pro", Routes: []pmdb.ModelRoute{{
					Provider:    "deepseek",
					ModelOpenAI: "deepseek-chat",
				}}},
				// deepseek-v4-flash: has model_responses → true only when override governs.
				{ID: "deepseek-v4-flash", Routes: []pmdb.ModelRoute{{
					Provider:       "deepseek",
					ModelOpenAI:    "deepseek-chat",
					ModelResponses: "deepseek-v4-real",
				}}},
			},
		}},
	})

	// Body model is the NON-responses pro model — but the override is flash, so
	// the gate must still route to passthrough.
	body := `{"model":"deepseek-v4-pro","input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gotPath != "/responses" {
		t.Fatalf("upstream path: got %q want /responses (gate ignored codex override)", gotPath)
	}
	if !strings.Contains(string(gotBody), `"deepseek-v4-real"`) {
		t.Fatalf("model not replaced to ModelResponses via override: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), `"input"`) {
		t.Fatalf("native input body not preserved: %s", gotBody)
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
