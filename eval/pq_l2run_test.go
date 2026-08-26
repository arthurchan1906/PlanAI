package eval

// P1b L2 编排层测试（PROCESS_QUALITY_SPEC §2.2：L1 候选 → Confirm → 解析 → 回填；
// LLM 不可用降级「L2 未运行」不伪造结果；每类上限成本控制）。

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// l2StubConfirmer 按任务返回合法 JSON 的替身确认器。
type l2StubConfirmer struct{}

func (l2StubConfirmer) Confirm(p L2Prompt) (string, error) {
	switch p.Task {
	case L2ClaimClassify:
		return `{"type":"事实","confidence":0.8}`, nil
	case L2EvidenceMatch:
		return `{"match":"弱","依据":"前序命令含相关对象"}`, nil
	case L2DeadloopConfirm:
		return `{"is_deadloop":true,"repeat_pattern":"同一构建命令反复执行，中间无修改","confidence":0.9}`, nil
	case L2DirectionEval:
		return `{"direction_ok":false,"note":"确认缺少历史检索"}`, nil
	case L2FeedbackResponse:
		return `{"responded":true,"deepened":true,"sustained":false,"aligned":true,"matched_object":"历史检索","note":""}`, nil
	}
	return "", fmt.Errorf("unknown task %s", p.Task)
}

func l2Fixture() (*ProcessReport, []Turn) {
	tr := pqTurn("u1", "iOS PalV2 密友跨平台不兼容，查一下", "2026-06-24T09:10:00")
	tr.Records = append(tr.Records,
		Record{Role: "assistant", ID: "r1", Content: "问题定位在 PalService.swift，已修复。", Tool: ToolRecord{Tool: "unknown"}, CreatedAt: ts("2026-06-24T09:11:00")},
		pqRec("bash", "go build", "2026-06-24T09:12:00"),
		pqRec("bash", "go build", "2026-06-24T09:14:00"),
	)
	rep := &ProcessReport{
		Deadloops: []DeadloopCandidate{
			{Start: ts("2026-06-24T09:11:00"), End: ts("2026-06-24T09:15:00"), Builds: 12, Fails: 8, SpontRetr: 0},
			{Start: ts("2026-06-24T09:30:00"), End: ts("2026-06-24T09:40:00"), Builds: 15, SpontRetr: 1, Excluded: true, Reason: "有根因定位"},
		},
		VerifyLoops: []VerifyLoopCandidate{
			{FailTime: ts("2026-06-24T09:20:00"), RetryTime: ts("2026-06-24T09:25:00"), Command: "make -j8", FailSig: "exit 1"},
		},
		DirectionShifts: []DirectionShiftCandidate{
			{Start: ts("2026-06-24T09:16:00"), End: ts("2026-06-24T09:19:00"), Switches: 6, TotalAccess: 20, Distinct: 4, NewRatio: 0.2},
		},
		Feedback: []FeedbackCandidate{
			{UserMsgID: "u2", Ts: ts("2026-06-24T09:05:00"), Kind: KindCorrection, Snippet: "方向不对 重新审视", Referents: []string{"历史"}},
		},
	}
	return rep, []Turn{tr}
}

func TestRunL2ConfirmationsFullPipeline(t *testing.T) {
	rep, turns := l2Fixture()
	res, err := RunL2Confirmations(l2StubConfirmer{}, rep, turns, L2RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ran {
		t.Fatal("confirmer 可用时应 Ran=true")
	}
	// 任务计数：断言分类 1（事实→证据配对 1）、死循环 1（Excluded 跳过）、形态 9 → 1、
	// 方向评估：死循环 SpontRetr=0 满足触发 → 1 + 形态 6（段内自发 0）→ 1、反馈 1
	if res.Total != 7 {
		t.Fatalf("Total = %d, want 7:\n%v", res.Total, res.Items)
	}
	if res.Succeeded != 7 || res.Failed != 0 {
		t.Fatalf("Succeeded/Failed = %d/%d, want 7/0", res.Succeeded, res.Failed)
	}
	byTask := map[L2Task]int{}
	for _, it := range res.Items {
		byTask[it.Task]++
		if len(it.Result) == 0 || !json.Valid(it.Result) {
			t.Errorf("item %s 结果非 JSON: %q", it.Task, it.Result)
		}
	}
	if byTask[L2ClaimClassify] != 1 || byTask[L2EvidenceMatch] != 1 ||
		byTask[L2DeadloopConfirm] != 2 || byTask[L2DirectionEval] != 2 ||
		byTask[L2FeedbackResponse] != 1 {
		t.Errorf("任务分布不符: %v", byTask)
	}
	// Excluded 的 near-miss 死循环候选不应被确认（无 09:30 时段 target）
	for _, it := range res.Items {
		if strings.Contains(it.Target, "09:30") {
			t.Errorf("Excluded 候选不应确认: %+v", it)
		}
	}
}

func TestRunL2ConfirmationsNilDegrade(t *testing.T) {
	rep, turns := l2Fixture()
	res, err := RunL2Confirmations(nil, rep, turns, L2RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ran {
		t.Error("confirmer nil 时 Ran 应为 false（降级标注，不伪造确认）")
	}
	if res.Total != 0 || len(res.Items) != 0 {
		t.Errorf("降级时不应有确认条目: Total=%d", res.Total)
	}
	if !strings.Contains(res.Reason, "L2 未配置") {
		t.Errorf("降级原因缺失: %q", res.Reason)
	}
}

func TestRunL2ConfirmationsMaxPerTask(t *testing.T) {
	rep, turns := l2Fixture()
	res, err := RunL2Confirmations(l2StubConfirmer{}, rep, turns, L2RunOptions{MaxPerTask: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 每类上限 1：断言分类 1（含其证据配对 1）、死循环 1、形态9 1、方向评估 1、反馈 1
	if res.Total != 6 {
		t.Fatalf("Total = %d, want 6（每类上限 1 + 事实断言引出的证据配对）", res.Total)
	}
	if !strings.Contains(res.Reason, "上限") {
		t.Errorf("应有上限跳过原因，got %q", res.Reason)
	}
}

func TestRunL2ConfirmationsLLMErrorRecorded(t *testing.T) {
	rep, turns := l2Fixture()
	res, err := RunL2Confirmations(l2ErrConfirmer{}, rep, turns, L2RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total == 0 || res.Succeeded != 0 || res.Failed != res.Total {
		t.Errorf("LLM 全失败应 Failed=Total: %d/%d", res.Succeeded, res.Failed)
	}
	for _, it := range res.Items {
		if it.Error == "" {
			t.Errorf("失败条目应有 Error: %+v", it)
		}
	}
}

func TestProblemContextPrefersNearestUserMsg(t *testing.T) {
	t1 := pqTurn("u1", "首条：iOS PalV2 密友跨平台不兼容", "2026-06-24T09:00:00")
	t2 := pqTurn("u2", "候选时段内的最新指令：换用 ED 日志", "2026-06-24T09:30:00")
	t3 := pqTurn("u3", "时段后的消息不应采用", "2026-06-24T10:00:00")
	turns := []Turn{t1, t2, t3}
	// 候选时段 09:20-09:40 → 应取 09:30 的 u2
	got := problemContext(turns, ts("2026-06-24T09:20:00"), ts("2026-06-24T09:40:00"))
	if !strings.Contains(got, "换用 ED 日志") {
		t.Errorf("应取时段内最近 user 消息，got %q", got)
	}
	// 时段内无 user 消息 → 回退首条非空
	got2 := problemContext(turns, ts("2026-06-24T08:00:00"), ts("2026-06-24T08:59:00"))
	if !strings.Contains(got2, "首条") {
		t.Errorf("应回退首条非空 user 消息，got %q", got2)
	}
}

func TestL2TimeoutErrorRecordedContinues(t *testing.T) {
	rep, turns := l2Fixture()
	// 超时替身：全部调用超时报错（走 Timeout 路径由 L2Client 负责；这里验证编排层
	// 把超时当失败条目记录并继续，不中断整轮）。
	res, err := RunL2Confirmations(l2ErrConfirmer{}, rep, turns, L2RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Succeeded != 0 || res.Failed != res.Total {
		t.Errorf("失败应全部记录: %d/%d", res.Succeeded, res.Failed)
	}
}

// l2ErrConfirmer 全任务失败的替身。
type l2ErrConfirmer struct{}

func (l2ErrConfirmer) Confirm(p L2Prompt) (string, error) {
	return "", fmt.Errorf("ai down")
}
