package proxy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	pmdb "aipmc/db"
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

// 8/13 F2: guidelines 超长时旧实现 written 按源长度计数（len(guidelines)+20=1642），
// 后续所有段 guard 恒真被全裁。回归测试：written 按实际写入长度计数后，
// 短 fileAssoc/goals 必须送达，guidelines 被截断需埋点。
func TestBuildContextBlockGuidelinesCountBug(t *testing.T) {
	gl := strings.Repeat("g", 1622)
	files := []string{"a.go → task (in_progress, P0) task-1234567890"}
	goals := []string{"short goal"}
	block, sc := buildContextBlock(goals, nil, nil, files, gl)
	if sc.fileAssoc != 0 {
		t.Fatalf("fileAssoc suppressed: got %d want 0 (count bug would trim all)", sc.fileAssoc)
	}
	if sc.goals != 0 {
		t.Fatalf("goals suppressed: got %d want 0 (count bug would trim all)", sc.goals)
	}
	if sc.guidelines != 1 {
		t.Fatalf("guidelines trim flag: got %d want 1", sc.guidelines)
	}
	// 8/28 精确计费：guidelines 截断时追加 "…"(3B) 预扣——guidelinesDel =
	// guidelinesBudget-3（内容 597 + 省略号 3 = 600 预算内，保证 block ≤800）。
	if sc.guidelinesDel != guidelinesBudget-3 {
		t.Fatalf("guidelines delivered: got %d want %d", sc.guidelinesDel, guidelinesBudget-3)
	}
	if !strings.Contains(block, "a.go → task") {
		t.Fatal("fileAssoc line should be present")
	}
	if !strings.Contains(block, "short goal") {
		t.Fatal("goal should be present")
	}
	if len(block) > maxInjectChars {
		t.Fatalf("block %d exceeds cap %d", len(block), maxInjectChars)
	}
}

// 8/13 F2: fileAssoc 独立硬子预算——超出部分记 fileAssoc 裁减。
// 8/18 预算校准：min(200+30×len, 500)。20 文件 → 500B 注入 16/20、裁 4
// （200B 固定预算实测平均裁剪率 82%，fileAssoc 功能失效，动态缩放修复）。
func TestBuildContextBlockFileAssocSubBudget(t *testing.T) {
	files := make([]string, 20)
	for i := range files {
		files[i] = strings.Repeat("x", 30)
	}
	block, sc := buildContextBlock(nil, nil, nil, files, "")
	if sc.fileAssoc != 4 {
		t.Fatalf("fileAssoc suppressed: got %d want 4 (500B dynamic budget fits 16 of 20)", sc.fileAssoc)
	}
	if sc.total() != sc.fileAssoc {
		t.Fatalf("total %d != fileAssoc %d", sc.total(), sc.fileAssoc)
	}
	if len(block) > maxInjectChars {
		t.Fatalf("block %d exceeds cap %d", len(block), maxInjectChars)
	}
}

func TestInjectSwitchDisabled(t *testing.T) {
	// A/B 开关：AIPMC_INJECT=0 时必须原样透传，不触碰 DB。
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"看下 proxy/discussion_dedup.go"}]}],"instructions":"You are a coding agent"}`)
	t.Setenv("AIPMC_INJECT", "0")
	if out := InjectSessionContext(body, "codex"); !bytes.Equal(out, body) {
		t.Fatal("AIPMC_INJECT=0: body must pass through unchanged")
	}
	if injectSwitchState() != "off" {
		t.Fatalf("injectSwitchState with AIPMC_INJECT=0 = %q, want off", injectSwitchState())
	}
	t.Setenv("AIPMC_INJECT", "1")
	if injectSwitchState() != "on" {
		t.Fatalf("injectSwitchState with AIPMC_INJECT=1 = %q, want on", injectSwitchState())
	}
}

// Regression (8/18 cache 命中率调查): buildFileAssoc 的输出必须确定且有序。
// Go map range 顺序随机，未排序时 fullHash 每请求变化 → 每请求重新注入 →
// deepseek prefix cache 在 system prompt 末尾断裂（观测断点 4480/4608）。
// 排序同时稳定 fullHash 与 buildContextBlock 的子预算截断选择。
func TestBuildFileAssocDeterministic(t *testing.T) {
	fileTasks := map[string]map[string]string{
		"a.go": {
			"task-1": "task-1 (done, P0)",
			"task-2": "task-2 (in_progress, P1)",
			"task-3": "task-3 (todo, P2)",
		},
		"b.go": {
			"task-4": "task-4 (done, P1)",
		},
	}
	var first []string
	for i := 0; i < 100; i++ {
		got := buildFileAssoc([]string{"b.go", "a.go"}, fileTasks)
		if i == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("iter %d: length changed: %v vs %v", i, got, first)
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("iter %d: assoc order unstable: %v vs %v", i, got, first)
			}
		}
	}
	if !sort.StringsAreSorted(first) {
		t.Fatalf("assoc must be sorted, got %v", first)
	}
	if len(first) != 4 {
		t.Fatalf("want 4 associations, got %v", first)
	}
	// fullHash 必须随相同 fileAssoc 稳定（same_content 才能命中）。
	h1 := hashString(fmt.Sprintf("%s%v%s", "block", first, "guidelines"))
	h2 := hashString(fmt.Sprintf("%s%v%s", "block", first, "guidelines"))
	if h1 != h2 {
		t.Fatalf("fullHash unstable for identical fileAssoc: %s vs %s", h1, h2)
	}
}

// Regression (8/18 cache 命中率调查): same_content 跳过时不能返回未注入的
// body。每个请求对客户端都是全新 body（注入块不随会话保留），返回未注入
// body 会让 SP 在「带块/不带块」间交替（cap_1 带注入块 vs cap_2 无注入块，
// 字节实证）。正确语义：内容未变时重新注入同一 block，SP 全程一致。
func TestInjectSameContentStillInjectsBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guidelines.md"), []byte("test guideline content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", dir)
	t.Setenv("AIPMC_INJECT", "1")
	// 先 Bootstrap 建库（避免注入写库失败在测试中产生 write_err 日志噪音）
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	guidelinesCache.mu.Lock()
	guidelinesCache.updatedAt = time.Time{} // 强制从 temp 目录重新加载
	guidelinesCache.content = ""
	guidelinesCache.mu.Unlock()
	injectTracker.Delete("codex")

	body := []byte(`{"input":[{"type":"message","role":"user","content":"hello"}],"instructions":"You are a coding agent"}`)
	first := InjectSessionContext(body, "codex")
	if bytes.Equal(first, body) {
		t.Fatal("first call must inject the block")
	}
	if !bytes.Contains(first, []byte("[项目编码规范]")) {
		t.Fatal("first call block missing guidelines section")
	}
	second := InjectSessionContext(body, "codex")
	if !bytes.Equal(second, first) {
		t.Fatalf("same_content skip must still inject the identical block")
	}
	if !bytes.Contains(second, []byte("[项目编码规范]")) {
		t.Fatal("second call must also contain the block")
	}
}

// S2（HARNESS §1.3）：实际注入的请求写 inject_log 一行；same_content 跳过不写。
func TestInjectWritesInjectLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guidelines.md"), []byte("test guideline content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", dir)
	t.Setenv("AIPMC_INJECT", "1")
	// 先 Bootstrap 建库（inject_log 表依赖 schema v4）
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	guidelinesCache.mu.Lock()
	guidelinesCache.updatedAt = time.Time{} // 强制从 temp 目录重新加载
	guidelinesCache.content = ""
	guidelinesCache.mu.Unlock()
	injectTracker.Delete("codex")

	body := []byte(`{"input":[{"type":"message","role":"user","content":"hello"}],"instructions":"You are a coding agent"}`)
	first := InjectSessionContext(body, "codex")
	if bytes.Equal(first, body) {
		t.Fatal("first call must inject the block")
	}
	// 第一次注入 → 写 1 行
	count := injectLogCount(t)
	if count != 1 {
		t.Fatalf("inject_log rows after first call = %d, want 1", count)
	}
	// 第二次 same_content → 注入同一 block，但不写表
	second := InjectSessionContext(body, "codex")
	if !bytes.Equal(second, first) {
		t.Fatal("same_content skip must still inject the identical block")
	}
	count = injectLogCount(t)
	if count != 1 {
		t.Fatalf("inject_log rows after same_content = %d, want 1 (skip 不写表)", count)
	}
	// 校验写入内容
	db, err := pmdb.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var agent, hash, source, segJSON string
	var chars int
	if err := db.QueryRow(`SELECT agent, hash, source, segments_json, chars FROM inject_log`).Scan(&agent, &hash, &source, &segJSON, &chars); err != nil {
		t.Fatalf("SELECT inject_log: %v", err)
	}
	if agent != "codex" {
		t.Errorf("agent = %s, want codex", agent)
	}
	if len(hash) != 8 {
		t.Errorf("hash = %q, want 8 chars", hash)
	}
	if source != "guidelines_only" {
		t.Errorf("source = %q, want guidelines_only (无 goals/fileAssoc 仅 guidelines)", source)
	}
	if chars <= 0 {
		t.Errorf("chars = %d, want > 0", chars)
	}
	if !strings.Contains(segJSON, `"guidelines":true`) {
		t.Errorf("segments_json missing guidelines: %s", segJSON)
	}
}

func injectLogCount(t *testing.T) int {
	t.Helper()
	db, err := pmdb.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inject_log`).Scan(&n); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	return n
}

// 8/18 修订写策略：char_limit 裁剪的请求已实际注入，写表且 suppressed=1
// （原 T7「suppressed 不写表」废弃——实测 98.9% 注入带裁剪，不写表则表恒空）。
func TestWriteInjectLogSuppressed(t *testing.T) {
	t.Setenv("PMAI_HOME", t.TempDir())
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	writeInjectLog("/proj", "codex", "sess-C", "r1-3", "", "12345678", 90, true, nil, nil, nil, []string{"a.go"}, "")

	db, err := pmdb.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var suppressed int
	var segJSON string
	if err := db.QueryRow(`SELECT suppressed, segments_json FROM inject_log`).Scan(&suppressed, &segJSON); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if suppressed != 1 {
		t.Errorf("suppressed = %d, want 1 (裁剪注入如实记录)", suppressed)
	}
	if !strings.Contains(segJSON, `"fileAssoc":["a.go"]`) {
		t.Errorf("segments_json missing fileAssoc: %s", segJSON)
	}
}

// currentInjectProject 不含 .pmai 后缀（8/26 实测 Dir² 少一层错值回归锁定）。
func TestCurrentInjectProjectRoot(t *testing.T) {
	got := currentInjectProject()
	if got == "" {
		t.Skip("非项目目录运行（无 .pmai 可推导）")
	}
	if strings.Contains(got, ".pmai") {
		t.Errorf("currentInjectProject = %q, 不应含 .pmai 后缀", got)
	}
}

// C0（8/27）：按请求归因——请求体含 /.pmai/ 绝对路径时，project= 取该路径
// 的项目根（须真实存在，方法 1 Stat 验证），不再被进程级 sync.Once 缓存锁定
// （F3 根因回归锁定）。
func TestInjectProjectForRequestFromBodyPaths(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "projB")
	if err := os.MkdirAll(filepath.Join(proj, ".pmai", "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := []byte(fmt.Sprintf(`{"messages":[{"content":"请修复 %s/.pmai/data/pmai.db 的归属问题"}]}`, proj))
	got := injectProjectForRequest(body)
	if got != proj {
		t.Errorf("injectProjectForRequest = %q, want %q", got, proj)
	}
}

// C0（8/27）：方法 1 必须过 Stat 验证——请求文本提及不存在的 /.pmai/ 路径时
// 不得误判为项目根（响应 Claude 审核问题 3）。
func TestProjectRootFromBodyRejectsNonexistent(t *testing.T) {
	body := []byte(`{"messages":[{"content":"参考 /Users/dazsec/projects/NotExist/.pmai/guidelines.md 的做法"}]}`)
	if got := projectRootFromBody(body); got != "" {
		t.Errorf("projectRootFromBody = %q, want \"\" (不存在的 /.pmai/ 路径不命中)", got)
	}
}

// C0（8/27）：无路径请求回退进程级推导（env/cwd），与 M1a 对账基准同源。
func TestInjectProjectForRequestFallback(t *testing.T) {
	body := []byte(`{"messages":[{"content":"继续昨天的讨论"}]}`)
	want := currentInjectProject()
	if got := injectProjectForRequest(body); got != want {
		t.Errorf("injectProjectForRequest = %q, want fallback %q", got, want)
	}
}

// C0（8/27）：方法 2 通用绝对路径遍历——目录真实存在带 .pmai 时按 Stat 命中。
func TestProjectRootFromBodyStatPath(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "projA")
	if err := os.MkdirAll(filepath.Join(proj, ".pmai", "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := []byte(fmt.Sprintf(`{"messages":[{"content":"改一下 %s/src/main.go"}]}`, proj))
	got := projectRootFromBody(body)
	if got != proj {
		t.Errorf("projectRootFromBody = %q, want %q", got, proj)
	}
}

// C0（8/27）：env 模式（PMAI_HOME 指向无 .pmai 层目录）时按请求归因不命中，
// 必须回退进程级推导——防止把 ~/.aipmc 误判为项目根。
func TestInjectProjectForRequestEnvMode(t *testing.T) {
	envDir := t.TempDir()
	body := []byte(fmt.Sprintf(`{"messages":[{"content":"查一下 %s/data/pmai.db"}]}`, envDir))
	if got := projectRootFromBody(body); got != "" {
		t.Errorf("projectRootFromBody = %q, want \"\" (env 模式无 .pmai 层不命中)", got)
	}
	// 回退必须是进程级 currentInjectProject（本测试环境 PMAI_HOME 已由 TestMain 指向临时目录）
	if got := injectProjectForRequest(body); got != currentInjectProject() {
		t.Errorf("injectProjectForRequest = %q, want fallback %q", got, currentInjectProject())
	}
}

// E 线（8/27）：guidelines 满 600 时高优段（warnings/actionItems）不被挤掉。
// 背景数据：8/27 日志实测 1047/1047 次注入被 char_limit 裁剪（0 次完整注入），
// 段裁剪 file_cut=9404 / warn=7631 / act=4800 / goals=3113 / guide=1017——
// guidelines(600B)+fileAssoc 把 written 顶过 750 后 warn/act guard 恒真、全被挤掉。
// 预算重排：guidelines 计算 avail 时扣除 warnActReserve，warn/act 稳定获得保留空间。
// 回归断言：高优段到达条数显著提升且 block 不超 cap。
func TestBuildContextBlockWarnActReserve(t *testing.T) {
	gl := strings.Repeat("g", guidelinesBudget)
	// 5 条 warn，每条 ≈55B（贴近真实 blind_edit_loop/frustration 单条长度）
	warns := make([]string, 5)
	for i := range warns {
		warns[i] = fmt.Sprintf("w%d-%s", i, strings.Repeat("x", 52))
	}
	// 8/27 常见 file_total≈6 场景（fileAssoc 3 条占 ≈117B）
	files := []string{
		"a.go → task-1111111111 (in_progress, P0)",
		"b.go → task-2222222222 (done, P1)",
		"c.go → task-3333333333 (todo, P2)",
	}
	block, sc := buildContextBlock(nil, warns, nil, files, gl)
	// 旧实现（无 reserve）：written=93+600+20=713，warn guard 773>750 → 5 条全裁。
	// 新实现：avail=min(600,750-93-200)=457，written=570，warn 至少到 3 条（裁 ≤2）。
	if sc.warnings > 2 {
		t.Fatalf("warnings suppressed %d, want ≤2 (reserve must protect high-priority)", sc.warnings)
	}
	for _, mark := range []string{"w0-", "w1-", "w2-"} {
		if !strings.Contains(block, mark) {
			t.Fatalf("warn %q must survive guidelines: %s", mark, block)
		}
	}
	if len(block) > maxInjectChars {
		t.Fatalf("block %d exceeds cap %d", len(block), maxInjectChars)
	}
}

// E 线（8/27）：actionItems 段在 guidelines 满 600 时同样不被挤掉（v1.13 §4:
// actionItems ceil 内优先级 3，高于 fileAssoc/goals）。
func TestBuildContextBlockActionItemsSurvive(t *testing.T) {
	gl := strings.Repeat("g", guidelinesBudget)
	warns := []string{strings.Repeat("w", 55)}
	acts := []string{"⚠️ 修复: aipm_record_commit(task_id=\"?\", title=\"...\")\n  → 详情: aipm_get_commit(\"task-0000000000\")"}
	block, sc := buildContextBlock(nil, warns, acts, nil, gl)
	if sc.actionItems != 0 {
		t.Fatalf("actionItems suppressed %d, want 0", sc.actionItems)
	}
	if !strings.Contains(block, "aipm_record_commit") {
		t.Fatalf("actionItem hint must survive guidelines: %s", block)
	}
	if len(block) > maxInjectChars {
		t.Fatalf("block %d exceeds cap %d", len(block), maxInjectChars)
	}
}

// 8/28 修 chars≤800（v1.13 §4 验收）：written 精确化（含段头/Vision tip）后，
// 无论各段如何塞满，block 必须严格 ≤ maxInjectChars——旧实现 block 可达 ~890。
func TestBuildContextBlockNeverExceedsCap(t *testing.T) {
	files := make([]string, 20)
	for i := range files {
		files[i] = strings.Repeat("x", 40)
	}
	warns := make([]string, 10)
	for i := range warns {
		warns[i] = strings.Repeat("w", 60)
	}
	acts := make([]string, 10)
	for i := range acts {
		acts[i] = strings.Repeat("a", 60)
	}
	goals := make([]string, 5)
	for i := range goals {
		goals[i] = strings.Repeat("g", 60)
	}
	gl := strings.Repeat("spec", 300)
	block, sc := buildContextBlock(goals, warns, acts, files, gl)
	if len(block) > maxInjectChars {
		t.Fatalf("block %d exceeds cap %d (sc=%+v)", len(block), maxInjectChars, sc)
	}
}
