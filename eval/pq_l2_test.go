package eval

// P1b L2 五任务确认器测试（PROCESS_QUALITY_SPEC §2.2 prompt 三约束 + JSON schema）。

import (
	"errors"
	"strings"
	"testing"
)

// prompt 三约束断言（§2.2）：① 只基于证据 ② 证据必填 ③ JSON 强制
func assertL2Constraints(t *testing.T, p L2Prompt) {
	t.Helper()
	for _, want := range []string{"只基于下方「证据」字段", "输出严格 JSON", "证据中的命令/文本已截断"} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system 缺约束 %q:\n%s", want, p.System)
		}
	}
	if p.Evidence == "" {
		t.Errorf("evidence 为空（约束②证据必填）")
	}
	if p.Task == "" {
		t.Errorf("task 为空")
	}
}

// ── 任务 1：断言分类 ──

func TestBuildClaimClassifyPrompt(t *testing.T) {
	p := BuildClaimClassifyPrompt("已修复，问题在 header 对齐", []string{"10:01 bash go build", "10:02 edit PalService.swift"})
	assertL2Constraints(t, p)
	if p.Task != L2ClaimClassify {
		t.Errorf("task = %s, want claim_classify", p.Task)
	}
	for _, want := range []string{"事实", "意见", "摘要", "进度", `{"type":`} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system 缺 schema 项 %q", want)
		}
	}
	if !strings.Contains(p.Evidence, "已修复") || !strings.Contains(p.Evidence, "go build") {
		t.Errorf("evidence 未含断言/前序命令:\n%s", p.Evidence)
	}
}

func TestParseClaimClassify(t *testing.T) {
	r, err := ParseClaimClassify(`{"type":"事实","confidence":0.9}`)
	if err != nil || r.Type != "事实" {
		t.Fatalf("parse = %+v, %v", r, err)
	}
	if _, err := ParseClaimClassify(`{"type":"猜测","confidence":0.9}`); err == nil {
		t.Error("非法 type 应报错")
	}
	if _, err := ParseClaimClassify(`not json`); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

// ── 任务 2：证据细配对 ──

func TestBuildEvidenceMatchPrompt(t *testing.T) {
	p := BuildEvidenceMatchPrompt("问题在 PalService.swift 的 header 对齐", []string{"PalService.swift", "PalV2.h"})
	assertL2Constraints(t, p)
	for _, want := range []string{"只确认「真无证据」", `{"match":"强|弱|无"`} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system 缺 %q", want)
		}
	}
}

func TestParseEvidenceMatch(t *testing.T) {
	r, err := ParseEvidenceMatch(`{"match":"强","依据":"PalService.swift 被 edit 且含 sizeof 改动"}`)
	if err != nil || r.Match != "强" {
		t.Fatalf("parse = %+v, %v", r, err)
	}
	if _, err := ParseEvidenceMatch(`{"match":"中"}`); err == nil {
		t.Error("非法 match 应报错")
	}
}

// ── 任务 3：死循环确认 ──

func TestBuildDeadloopConfirmPrompt(t *testing.T) {
	c := DeadloopCandidate{Start: ts("2026-06-23T15:00:00"), End: ts("2026-06-23T16:00:00"),
		Builds: 12, Fails: 8, SpontRetr: 0, Passive: 0}
	p := BuildDeadloopConfirmPrompt(c, []string{"15:01 bash make -j8", "15:02 bash make -j8"})
	assertL2Constraints(t, p)
	for _, want := range []string{"重复盲试", "repeat_pattern", "排除：有 edit/commit"} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system 缺 %q", want)
		}
	}
	if !strings.Contains(p.Evidence, "build=12") || !strings.Contains(p.Evidence, "make -j8") {
		t.Errorf("evidence 未含候选信号/命令:\n%s", p.Evidence)
	}
}

func TestParseDeadloopConfirm(t *testing.T) {
	r, err := ParseDeadloopConfirm(`{"is_deadloop":true,"repeat_pattern":"同一构建命令反复执行，中间无修改","confidence":0.85}`)
	if err != nil || !r.IsDeadloop {
		t.Fatalf("parse = %+v, %v", r, err)
	}
	if _, err := ParseDeadloopConfirm(`{`); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

// ── 任务 4：方向评估 ──

func TestDirectionEvalTriggered(t *testing.T) {
	if !DirectionEvalTriggered(1, 10, 10) {
		t.Error("自发<2 + build 密集 → 应触发")
	}
	if DirectionEvalTriggered(3, 10, 10) {
		t.Error("自发≥2 → 不触发")
	}
	if DirectionEvalTriggered(1, 5, 10) {
		t.Error("build 不密集 → 不触发")
	}
}

func TestBuildDirectionEvalPrompt(t *testing.T) {
	p := BuildDirectionEvalPrompt("iOS PalV2 密友跨平台不兼容", []string{"15:01 bash make", "15:02 bash make"}, 1)
	assertL2Constraints(t, p)
	for _, want := range []string{"只判定一件事", "(建议)", "direction_ok"} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system 缺 %q", want)
		}
	}
	if !strings.Contains(p.Evidence, "iOS PalV2") || !strings.Contains(p.Evidence, "自发检索次数：1") {
		t.Errorf("evidence 未含问题上下文/自发检索:\n%s", p.Evidence)
	}
}

func TestParseDirectionEval(t *testing.T) {
	r, err := ParseDirectionEval(`{"direction_ok":false,"note":"确认缺少历史检索"}`)
	if err != nil || r.DirectionOK {
		t.Fatalf("parse = %+v, %v", r, err)
	}
}

// ── 任务 5：反馈响应确认 ──

func TestBuildFeedbackResponsePrompt(t *testing.T) {
	fb := FeedbackCandidate{Kind: KindCorrection, Ts: ts("2026-06-24T09:11:46"), Snippet: "方向不对 重新审视", Referents: []string{"历史"}}
	p := BuildFeedbackResponsePrompt(fb, []string{"09:12 bash git log", "09:13 read PalService.swift"})
	assertL2Constraints(t, p)
	for _, want := range []string{"responded", "deepened", "sustained", "aligned", "matched_object"} {
		if !strings.Contains(p.System, want) {
			t.Errorf("system 缺五子信号字段 %q", want)
		}
	}
	if !strings.Contains(p.Evidence, "方向不对") || !strings.Contains(p.Evidence, "git log") {
		t.Errorf("evidence 未含反馈/窗口行为:\n%s", p.Evidence)
	}
}

func TestParseFeedbackResponse(t *testing.T) {
	r, err := ParseFeedbackResponse(`{"responded":true,"deepened":true,"sustained":false,"aligned":true,"matched_object":"历史检索","note":""}`)
	if err != nil || !r.Responded || !r.Deepened || r.Sustained || r.MatchedObject != "历史检索" {
		t.Fatalf("parse = %+v, %v", r, err)
	}
}

// ── L2Client 通道 ──

type stubSummarizer struct {
	out string
	err error
	got string
}

func (s *stubSummarizer) SummarizeJSON(text, instruction string) (string, error) {
	s.got = text
	if s.err != nil {
		return "", s.err
	}
	return s.out, nil
}

func TestL2ClientConfirm(t *testing.T) {
	stub := &stubSummarizer{out: `{"type":"进度","confidence":0.8}`}
	c := &L2Client{Summarizer: stub}
	p := BuildClaimClassifyPrompt("已完成", []string{"10:01 bash go build"})
	out, err := c.Confirm(p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseClaimClassify(out)
	if err != nil || r.Type != "进度" {
		t.Fatalf("roundtrip = %+v, %v", r, err)
	}
	if stub.got != p.Evidence {
		t.Errorf("SummarizeJSON 收到的 text ≠ Evidence（System=指令应传 instruction 参数）")
	}
}

func TestL2ClientConfirmEmptyEvidence(t *testing.T) {
	c := &L2Client{Summarizer: &stubSummarizer{}}
	if _, err := c.Confirm(L2Prompt{Task: L2ClaimClassify, System: "x"}); err == nil {
		t.Error("证据为空应报错（约束②）")
	}
}

func TestL2ClientConfirmErrorPropagates(t *testing.T) {
	stub := &stubSummarizer{err: errors.New("ai down")}
	c := &L2Client{Summarizer: stub}
	if _, err := c.Confirm(BuildClaimClassifyPrompt("x", nil)); err == nil {
		t.Error("LLM 错误应向上传播")
	}
}

// ── 断言候选提取 ──

func TestCandidateClaims(t *testing.T) {
	recs := []Record{
		{Role: "assistant", Content: "我检查了 header 结构，问题定位在 PalService.swift，已修复。", Tool: ToolRecord{Tool: "unknown"}},
		{Role: "assistant", Content: "构建通过，测试全绿。", Tool: ToolRecord{Tool: "llm_message"}},
		{Role: "assistant", Content: "这只是背景说明。", Tool: ToolRecord{Tool: "llm_message"}},
		{Role: "assistant", Content: "🔧 go build", Tool: ToolRecord{Tool: "bash"}}, // 工具行不提取
	}
	claims := CandidateClaims(recs)
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2:\n%v", len(claims), claims)
	}
	if !strings.Contains(claims[0], "问题定位") || !strings.Contains(claims[1], "测试全绿") {
		t.Errorf("提取句不符:\n%v", claims)
	}
}

// ── 证据组装 ──

func TestL2CommandLinesTruncate(t *testing.T) {
	long := "cd /Users/x && " + strings.Repeat("a", 500)
	recs := []Record{
		{CreatedAt: ts("2026-06-24T09:12:00"), Tool: ToolRecord{Tool: "bash", Command: "git log --oneline"}},
		{CreatedAt: ts("2026-06-24T09:13:00"), Tool: ToolRecord{Tool: "edit", Command: ""}, Content: "修改 PalService.swift"},
		{CreatedAt: ts("2026-06-24T09:14:00"), Tool: ToolRecord{Tool: "bash", Command: long}},
	}
	lines := l2CommandLines(recs, 2, 50)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2（limit）", len(lines))
	}
	if !strings.Contains(lines[0], "09:12 bash git log") {
		t.Errorf("line0 = %q", lines[0])
	}
	if !strings.Contains(lines[1], "09:13 edit 修改 PalService.swift") {
		t.Errorf("line1 = %q（edit 无 Command 用 Content 摘要）", lines[1])
	}
	lines2 := l2CommandLines(recs, 0, 30)
	if len(lines2) != 3 || !strings.Contains(lines2[2], "…") {
		t.Errorf("截断失败: len=%d last=%q", len(lines2), lines2[2])
	}
	if len(lines2[2]) >= len(long) {
		t.Errorf("截断未生效: last_len=%d 应 < long_len=%d", len(lines2[2]), len(long))
	}
}

func TestL2RecordsBetween(t *testing.T) {
	tr := pqTurn("u1", "查", "2026-06-24T09:10:00")
	tr.Records = append(tr.Records, pqRec("bash", "a", "2026-06-24T09:12:00"))
	tr.Records = append(tr.Records, pqRec("bash", "b", "2026-06-24T09:30:00"))
	tr.Records = append(tr.Records, pqRec("bash", "c", "2026-06-24T09:31:00"))
	got := l2RecordsBetween([]Turn{tr}, ts("2026-06-24T09:11:00"), ts("2026-06-24T09:30:00"))
	if len(got) != 2 {
		t.Fatalf("records = %d, want 2（含端点 09:12/09:30）", len(got))
	}
}
